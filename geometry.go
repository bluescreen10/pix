// Package render is the bindless, GPU-driven renderer built on the gpu. It is the
// port of pix's WebGPU renderer onto the "no graphics API" model: no bind groups,
// no descriptor sets — every buffer is reached through a 64-bit device address
// (BDA) carried in a per-draw root struct, and textures through the bindless heap.
package pix

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/internal/mem"
)

// Stream indices. Position and index are their own buffers (so position-only
// passes — shadow/depth/cull — touch minimal memory); normal/color/uv are
// interleaved into one packed attributes buffer. All three are TLSF-suballocated
// within a single growable BDA buffer.
const (
	streamPos = iota
	streamAttr
	streamIndex
	streamCount
)

// streamElemSize is the byte size of one addressable element of each stream, used
// to turn a TLSF byte offset into the element base stored in the descriptor.
// Positions are addressed as floats (base in f32 units) so the vertex shader can
// pull an x/y/z triple; attributes as 16-byte records; indices as u32.
var streamElemSize = [streamCount]uint32{
	streamPos:   4,
	streamAttr:  16,
	streamIndex: 4,
}

// Attribute-presence flags stored in GeometryDesc.Flags (mirrors the GLSL).
const (
	FlagNormal uint32 = 1 << iota
	FlagUV
	FlagColor
)

const initialStreamBytes uint32 = 1 << 16

// Sphere is a local-space bounding sphere (renderer-local; the top-level pix.Sphere
// bridges to this when the full renderer is ported).
type Sphere struct {
	Center glm.Vec3f
	Radius float32
}

// vertexAttributes is the interleaved per-vertex attribute record (16 bytes,
// scalar-packed). Mirrors the GLSL attribute stream layout.
type vertexAttributes struct {
	normal glm.RGB10A2 // unit normal, [-1,1] -> [0,1]
	color  glm.RGBA8
	uv     glm.Vec2f
}

// GeometryDesc links a geometry id to its data in the shared streams. Bases are
// element offsets (byteOffset / streamElemSize). 24 bytes; matches the GLSL GeoDesc.
type GeometryDesc struct {
	PositionBase  uint32
	AttributeBase uint32
	IndexBase     uint32
	IndexCount    uint32
	Flags         uint32
	_             uint32
}

func (d *GeometryDesc) setBase(stream int, base uint32) {
	switch stream {
	case streamPos:
		d.PositionBase = base
	case streamAttr:
		d.AttributeBase = base
	case streamIndex:
		d.IndexBase = base
	}
}

var descSize = uint32(unsafe.Sizeof(GeometryDesc{}))

// Data is a geometry's source CPU data. Position is required; the optional
// attributes, when present, must match the position count. A nil Indices list is
// generated as 0..n-1.
type Data struct {
	Positions []glm.Vec3f
	Normals   []glm.Vec3f
	Colors    []glm.Vec4f
	UVs       []glm.Vec2f
	Indices   []uint32
}

// entry is the source of truth for one geometry. The packed stream bytes are never
// stored — they're reconstructed from Data on demand (create, grow), so nothing is
// duplicated. It holds each present stream's suballocation and the local bounds.
type entry struct {
	data   Data
	allocs [streamCount]mem.Allocation
	bounds Sphere
}

func (e *entry) hasVertexAttrs() bool {
	return len(e.data.Normals) > 0 || len(e.data.Colors) > 0 || len(e.data.UVs) > 0
}

func (e *entry) streamPresent(stream int) bool {
	if stream == streamAttr {
		return e.hasVertexAttrs()
	}
	return true
}

func (e *entry) flags() uint32 {
	var f uint32
	if len(e.data.Normals) > 0 {
		f |= FlagNormal
	}
	if len(e.data.UVs) > 0 {
		f |= FlagUV
	}
	if len(e.data.Colors) > 0 {
		f |= FlagColor
	}
	return f
}

// bytes reconstructs the packed byte payload for a stream from the source Data.
func (e *entry) bytes(stream int) []byte {
	switch stream {
	case streamPos:
		return toBytes(e.data.Positions)
	case streamIndex:
		return toBytes(e.data.Indices)
	case streamAttr:
		return e.packAttributes()
	}
	return nil
}

func (e *entry) packAttributes() []byte {
	if !e.hasVertexAttrs() {
		return nil
	}
	n := len(e.data.Positions)
	attrs := make([]vertexAttributes, n)
	for i := 0; i < n; i++ {
		va := vertexAttributes{color: glm.RGBA8{255, 255, 255, 255}} // default white
		if i < len(e.data.Normals) {
			// RGB10A2 stores unsigned [0,1]; remap the [-1,1] normal so the shader
			// can decode it back with *2-1.
			n := e.data.Normals[i]
			va.normal = glm.Vec3f{n[0]*0.5 + 0.5, n[1]*0.5 + 0.5, n[2]*0.5 + 0.5}.RGB10A2()
		}
		if i < len(e.data.Colors) {
			va.color = e.data.Colors[i].RGBA8()
		}
		if i < len(e.data.UVs) {
			va.uv = e.data.UVs[i]
		}
		attrs[i] = va
	}
	return toBytes(attrs)
}

type stream struct {
	label string
	buf   gpu.Buffer
	tlsf  *mem.TLSF
}

// Geometry owns geometry resources for GPU-driven, vertex-pulling rendering: it
// allocates generation-stamped ids, packs attributes into shared BDA streams, and
// resolves an id to both its CPU data and its GPU pull descriptor. There are no
// bind groups — callers pass PositionsAddr/AttributesAddr/DescriptorsAddr into
// their root structs and IndexBuffer as the hardware index buffer.
type geometrySystem struct {
	backend gpu.Backend
	streams [streamCount]*stream

	entries mem.Slab[entry]
	descs   []GeometryDesc

	descBuf   gpu.Buffer
	descCap   uint32
	descDirty bool
}

// newGeometrySystem creates the system, its three streams, and the descriptor buffer.
func newGeometrySystem(b gpu.Backend) *geometrySystem {
	g := &geometrySystem{backend: b, entries: mem.NewSlab[entry]()}
	g.streams[streamPos] = g.newStream("Vertex Positions", initialStreamBytes)
	g.streams[streamAttr] = g.newStream("Vertex Attributes", initialStreamBytes)
	g.streams[streamIndex] = g.newStream("Vertex Indices", initialStreamBytes)
	g.descBuf = b.Alloc(uint64(descSize), gpu.MemoryHost, "Geometry Descriptors")
	g.descCap = 1
	return g
}

func (g *geometrySystem) newStream(label string, bytes uint32) *stream {
	return &stream{
		label: label,
		buf:   g.backend.Alloc(uint64(bytes), gpu.MemoryHost, label),
		tlsf:  mem.NewTLSF(bytes),
	}
}

// PositionsAddr / AttributesAddr / DescriptorsAddr return the current device
// addresses of the shared buffers (they change when a stream grows, so read them
// each frame when building root structs).
func (g *geometrySystem) PositionsAddr() uint64   { return g.streams[streamPos].buf.Addr }
func (g *geometrySystem) AttributesAddr() uint64  { return g.streams[streamAttr].buf.Addr }
func (g *geometrySystem) DescriptorsAddr() uint64 { return g.descBuf.Addr }

// IndexBuffer returns the shared index stream as a hardware index buffer, for
// DrawIndexed / DrawIndexedIndirect. A geometry's firstIndex is its Desc().IndexBase.
func (g *geometrySystem) IndexBuffer() gpu.Buffer { return g.streams[streamIndex].buf }

// Desc returns a copy of a geometry's GPU descriptor (bases + counts + flags).
func (g *geometrySystem) Desc(id uint32) GeometryDesc { return g.descs[id] }

// Bounds returns a geometry's local bounding sphere.
func (g *geometrySystem) Bounds(id uint32) Sphere { return g.entries.Get(id).bounds }

// Generation returns the current generation of a geometry id (for stale checks).
func (g *geometrySystem) Generation(id uint32) uint32 { return g.entries.Generation(id) }

// --- ref-counted handle (renderer-owned resource) ---

// Geometry is a ref-counted handle to a renderer-owned geometry. Clone with Copy();
// surrender ownership with Release() (the geometry is freed at refcount 0). It caches
// the local bounding sphere so scenes/loaders can frame without the geometry system.
type Geometry struct {
	ref    Ref
	bounds Sphere
}

// Copy returns another handle to the same geometry, bumping the refcount.
func (g Geometry) Copy() Geometry { return Geometry{ref: g.ref.Copy(), bounds: g.bounds} }

// Release drops this handle's reference; the geometry is freed when the last is released.
func (g Geometry) Release() { g.ref.Release() }

// Valid reports whether the underlying geometry is still alive.
func (g Geometry) Valid() bool { return g.ref.Valid() }

// BoundingSphere returns the geometry's local-space bounding sphere.
func (g Geometry) BoundingSphere() Sphere { return g.bounds }

// id is the geometry's slot in the system (used by drawables).
func (g Geometry) id() uint32 { return g.ref.id }

// Disposer: dispose/generation let a Ref own a slot in this system.
func (g *geometrySystem) dispose(id uint32)           { g.Free(id) }
func (g *geometrySystem) generation(id uint32) uint32 { return g.entries.Generation(id) }

// newHandle allocates a geometry from data and returns a fresh single-ref handle.
func (g *geometrySystem) newHandle(data Data) Geometry {
	id, gen := g.Create(data)
	rc := int32(1)
	return Geometry{ref: Ref{id: id, gen: gen, refCount: &rc, owner: g}, bounds: g.Bounds(id)}
}

// Create allocates a generation-stamped geometry id from data, uploads its streams,
// and records its descriptor. Positions are required; optional attributes must match
// the position count; a nil index list is generated 0..n-1.
func (g *geometrySystem) Create(data Data) (id, gen uint32) {
	n := len(data.Positions)
	if n == 0 {
		panic("render: geometry has no positions")
	}
	for name, l := range map[string]int{"normals": len(data.Normals), "colors": len(data.Colors), "uvs": len(data.UVs)} {
		if l != 0 && l != n {
			panic(fmt.Sprintf("render: %s length %d does not match %d positions", name, l, n))
		}
	}
	if len(data.Indices) == 0 {
		data.Indices = make([]uint32, n)
		for i := range data.Indices {
			data.Indices[i] = uint32(i)
		}
	}

	e := entry{data: data, bounds: boundingSphereOf(data.Positions)}

	var d GeometryDesc
	d.Flags = e.flags()
	d.IndexCount = uint32(len(data.Indices))

	// Suballocate + upload each present stream. Growth repacks existing geometries
	// only (this entry is not yet in the slab), so it's safe here.
	for s := range streamCount {
		if !e.streamPresent(s) {
			continue
		}
		b := e.bytes(s)
		alloc := g.allocIn(s, uint32(len(b)))
		e.allocs[s] = alloc
		g.writeStream(s, alloc.Offset(), b)
		d.setBase(s, alloc.Offset()/streamElemSize[s])
	}

	id, gen = g.entries.Alloc(e)
	if g.entries.Len() >= len(g.descs) {
		g.descs = append(g.descs, GeometryDesc{})
	}
	g.descs[id] = d
	g.descDirty = true
	return id, gen
}

// Free releases a geometry id; its suballocations return to the TLSF pools and the
// slot's generation is bumped so existing handles become detectably stale.
func (g *geometrySystem) Free(id uint32) {
	if !g.entries.Alive(id) {
		return
	}
	e := g.entries.Get(id)
	for s := 0; s < streamCount; s++ {
		if e.streamPresent(s) {
			g.streams[s].tlsf.Free(e.allocs[s])
		}
	}
	*e = entry{}
	g.entries.Free(id)
	g.descs[id] = GeometryDesc{}
	g.descDirty = true
}

// allocIn suballocates bytes in a stream, growing (and repacking) if it's full.
func (g *geometrySystem) allocIn(stream int, bytes uint32) mem.Allocation {
	s := g.streams[stream]
	if alloc, err := s.tlsf.Alloc(bytes); err == nil {
		return alloc
	}
	free, _ := s.tlsf.StorageReport()
	used := s.tlsf.Capacity() - free
	g.growStream(stream, used+bytes)
	alloc, err := g.streams[stream].tlsf.Alloc(bytes)
	if err != nil {
		panic(fmt.Sprintf("render: %s alloc of %d bytes failed after grow", s.label, bytes))
	}
	return alloc
}

// growStream replaces a stream with a larger BDA buffer (and fresh TLSF) sized to
// at least minCap, repacking every live geometry's range and updating descriptor
// bases. The buffer's device address changes, so callers must re-read the *Addr
// getters afterwards (they do, per frame).
func (g *geometrySystem) growStream(stream int, minCap uint32) {
	s := g.streams[stream]
	newCap := s.tlsf.Capacity()
	if newCap == 0 {
		newCap = initialStreamBytes
	}
	for newCap < minCap {
		newCap *= 2
	}

	ns := g.newStream(s.label, newCap)
	g.streams[stream] = ns
	for id, e := range g.entries.All() {
		if !e.streamPresent(stream) {
			continue
		}
		b := e.bytes(stream)
		alloc, err := ns.tlsf.Alloc(uint32(len(b)))
		if err != nil {
			panic(fmt.Sprintf("render: repack of %s failed", s.label))
		}
		e.allocs[stream] = alloc
		g.writeStream(stream, alloc.Offset(), b)
		g.descs[id].setBase(stream, alloc.Offset()/streamElemSize[stream])
	}
	g.backend.Free(s.buf)
	g.descDirty = true
}

// Sync uploads the descriptor table when it changed. Stream bytes are written
// directly into mapped memory on Create/grow, so they need nothing here. Call once
// per frame before recording draws that read DescriptorsAddr.
func (g *geometrySystem) Sync() {
	if !g.descDirty {
		return
	}
	g.ensureDescCap(uint32(len(g.descs)))
	if len(g.descs) > 0 {
		writeAt(g.descBuf, 0, toBytes(g.descs))
	}
	g.descDirty = false
}

func (g *geometrySystem) ensureDescCap(need uint32) {
	if g.descCap >= need {
		return
	}
	for g.descCap < need {
		g.descCap *= 2
	}
	g.backend.Free(g.descBuf)
	g.descBuf = g.backend.Alloc(uint64(g.descCap)*uint64(descSize), gpu.MemoryHost, "Geometry Descriptors")
}

// writeStream copies packed bytes into a stream's mapped buffer at a byte offset.
func (g *geometrySystem) writeStream(stream int, byteOffset uint32, data []byte) {
	writeAt(g.streams[stream].buf, byteOffset, data)
}

// Destroy releases all GPU buffers held by the system.
func (g *geometrySystem) Destroy() {
	for s := 0; s < streamCount; s++ {
		if g.streams[s] != nil && g.streams[s].buf.Valid() {
			g.backend.Free(g.streams[s].buf)
			g.streams[s] = nil
		}
	}
	if g.descBuf.Valid() {
		g.backend.Free(g.descBuf)
		g.descBuf = gpu.Buffer{}
	}
}

// writeAt copies data into a host-mapped buffer at a byte offset.
func writeAt(buf gpu.Buffer, byteOffset uint32, data []byte) {
	if len(data) == 0 {
		return
	}
	dst := unsafe.Slice((*byte)(buf.Ptr), buf.Size)
	copy(dst[byteOffset:], data)
}

// toBytes reinterprets a slice as its raw bytes (no copy).
func toBytes[T any](s []T) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*int(unsafe.Sizeof(s[0])))
}

// boundingSphereOf returns the centroid and max-radius bounding sphere of points.
func boundingSphereOf(points []glm.Vec3f) Sphere {
	var s Sphere
	if len(points) == 0 {
		return s
	}
	for _, p := range points {
		s.Center = s.Center.Add(p)
	}
	s.Center = s.Center.Scale(1.0 / float32(len(points)))
	var maxSq float32
	for _, p := range points {
		d := p.Sub(s.Center)
		if sq := d[0]*d[0] + d[1]*d[1] + d[2]*d[2]; sq > maxSq {
			maxSq = sq
		}
	}
	s.Radius = float32(math.Sqrt(float64(maxSq)))
	return s
}
