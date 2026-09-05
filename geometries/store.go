// Package geometries owns geometry resources for pix's bindless, GPU-driven
// renderer: no bind groups, no descriptor sets — every buffer is reached through a
// 64-bit device address (BDA) carried in a per-draw root struct.
package geometries

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/internal/mem"
	"github.com/bluescreen10/pix/internal/ref"
)

// Stream indices. Position and index are their own buffers (so position-only
// passes — shadow/depth/cull — touch minimal memory); normal/color/uv are
// interleaved into one packed attributes buffer. All three are TLSF-suballocated
// within a single growable BDA buffer.
const (
	streamPos = iota
	streamAttr
	streamIndex
	streamSkin
	streamCount
)

// streamElemSize is the byte size of one addressable element of each stream, used
// to turn a TLSF byte offset into the element base stored in the descriptor.
// Positions are addressed as floats (base in f32 units) so the vertex shader can
// pull an x/y/z triple; attributes and skin records as 16-byte records; indices as u32.
var streamElemSize = [streamCount]uint32{
	streamPos:   4,
	streamAttr:  16,
	streamIndex: 4,
	streamSkin:  16,
}

// Attribute-presence flags stored in geometryDesc.Flags (mirrors the GLSL).
const (
	FlagNormal uint32 = 1 << iota
	FlagUV
	FlagColor
	FlagSkinned
)

const initialStreamBytes uint32 = 1 << 16

// geometryDesc links a geometry id to its data in the shared streams. Bases are
// element offsets (byteOffset / streamElemSize). 24 bytes; matches the GLSL GeoDesc.
type geometryDesc struct {
	PositionBase  uint32
	AttributeBase uint32
	IndexBase     uint32
	IndexCount    uint32
	Flags         uint32
	SkinBase      uint32
}

func (d *geometryDesc) setBase(stream int, base uint32) {
	switch stream {
	case streamPos:
		d.PositionBase = base
	case streamAttr:
		d.AttributeBase = base
	case streamIndex:
		d.IndexBase = base
	case streamSkin:
		d.SkinBase = base
	}
}

var descSize = uint64(unsafe.Sizeof(geometryDesc{}))

type stream struct {
	label string
	buf   gpu.Buffer
	tlsf  *mem.TLSF
}

// Store owns geometry resources for GPU-driven, vertex-pulling rendering: it
// allocates generation-stamped ids, packs attributes into shared BDA streams, and
// resolves an id to both its CPU data and its GPU pull descriptor. There are no
// bind groups — callers pass PositionsAddr/AttributesAddr/DescriptorsAddr into
// their root structs and IndexBuffer as the hardware index buffer.
type Store struct {
	backend gpu.Backend
	streams [streamCount]*stream

	entries mem.Slab[entry]
	descs   []geometryDesc

	descBuf   gpu.Buffer
	descDirty bool

	// Stream buffers live in MemoryDevice, so alloc/grow cannot write them
	// directly; they enqueue pendingWrite entries that Sync drains through the
	// uploader (stage + CopyBuffer). pendingWriteMu guards the queue because
	// geometry may be created/freed from multiple goroutines.
	pending        []pendingWrite
	pendingWriteMu sync.Mutex
}

// pendingWrite is a deferred upload of packed bytes into a stream at a byte
// offset. The data slice is retained until Sync stages it. Because it targets a
// stream index (not a buffer handle), a grow that replaces the stream buffer
// before Sync still resolves to the current buffer.
type pendingWrite struct {
	stream int
	offset uint32
	data   []byte
}

// NewStore creates the store, its three streams, and the descriptor buffer.
func NewStore(backend gpu.Backend) *Store {
	g := &Store{backend: backend, entries: mem.NewSlab[entry]()}
	g.streams[streamPos] = g.newStream("Vertex Positions", initialStreamBytes)
	g.streams[streamAttr] = g.newStream("Vertex Attributes", initialStreamBytes)
	g.streams[streamIndex] = g.newStream("Vertex Indices", initialStreamBytes)
	g.streams[streamSkin] = g.newStream("Vertex Skin", initialStreamBytes)
	g.descBuf = backend.Alloc(descSize, gpu.MemoryDevice, "Geometry Descriptors")
	return g
}

func (g *Store) newStream(label string, bytes uint32) *stream {
	return &stream{
		label: label,
		buf:   g.backend.Alloc(uint64(bytes), gpu.MemoryDevice, label),
		tlsf:  mem.NewTLSF(bytes),
	}
}

// PositionsAddr / AttributesAddr / DescriptorsAddr return the current device
// addresses of the shared buffers (they change when a stream grows, so read them
// each frame when building root structs).
func (g *Store) PositionsAddr() uint64 {
	return g.streams[streamPos].buf.Addr
}

func (g *Store) AttributesAddr() uint64 {
	return g.streams[streamAttr].buf.Addr
}

func (g *Store) DescriptorsAddr() uint64 {
	return g.descBuf.Addr
}

// SkinAddr returns the current device address of the shared skin-record stream
// (joint indices + weights). Changes when the stream grows, like the others.
func (g *Store) SkinAddr() uint64 {
	return g.streams[streamSkin].buf.Addr
}

// IndexBuffer returns the shared index stream as a hardware index buffer, for
// DrawIndexed / DrawIndexedIndirect. A geometry's firstIndex is its IndexBase.
func (g *Store) IndexBuffer() gpu.Buffer {
	return g.streams[streamIndex].buf
}

// IndexCount returns a geometry's index count, for building an indirect draw command.
func (g *Store) IndexCount(id uint32) uint32 {
	return g.descs[id].IndexCount
}

// IndexBase returns a geometry's first-index offset into the shared index buffer
// (see IndexBuffer).
func (g *Store) IndexBase(id uint32) uint32 {
	return g.descs[id].IndexBase
}

// BoundingSphere returns a geometry's local bounding sphere.
func (g *Store) BoundingSphere(id uint32) glm.Sphere {
	return g.entries.Get(id).boundingSphere
}

// Generation returns the current generation of a geometry id (for stale checks).
func (g *Store) Generation(id uint32) uint32 {
	return g.entries.Generation(id)
}

// dispose/validate let a ref own a slot in this store.
func (g *Store) dispose(id uint32) {
	g.Free(id)
}

func (g *Store) validate(id, gen uint32) bool {
	return g.entries.Generation(id) == gen
}

// Create allocates a geometry from cfg and returns a fresh single-ref handle.
func (g *Store) Create(cfg GeometryConfig) Geometry {
	id, gen := g.alloc(cfg)
	return Geometry{ref: ref.New(id, gen, g.dispose, g.validate), store: g, boundingSphere: g.BoundingSphere(id)}
}

// alloc allocates a generation-stamped geometry id from cfg, uploads its streams,
// and records its descriptor. A Position attribute is required; other attributes
// must match the position count and carry their canonical dataType; a nil index
// list is generated 0..n-1.
func (g *Store) alloc(cfg GeometryConfig) (id, gen uint32) {
	var e entry
	for _, a := range cfg.Attributes {
		if a.attrType >= attributeCount {
			panic(fmt.Sprintf("render: unknown attribute type %d", a.attrType))
		}
		if want := canonicalDataType[a.attrType]; a.dataType != want {
			panic(fmt.Sprintf("render: attribute %d expects dataType %d, got %d", a.attrType, want, a.dataType))
		}
		e.attrs[a.attrType] = a
	}

	n := e.attrs[AttributePosition].count
	if n == 0 {
		panic("render: geometry has no positions")
	}
	for t := AttributeType(0); t < attributeCount; t++ {
		if e.has(t) && e.attrs[t].count != n {
			panic(fmt.Sprintf("render: attribute %d length %d does not match %d positions", t, e.attrs[t].count, n))
		}
	}
	if e.has(AttributeSkinIndex) != e.has(AttributeSkinWeight) {
		panic("render: geometry must have both skin index and skin weight attributes, or neither")
	}

	e.indices = cfg.Indices
	if len(e.indices) == 0 {
		e.indices = make([]uint32, n)
		for i := range e.indices {
			e.indices[i] = uint32(i)
		}
	}
	e.boundingSphere = glm.BoundingSphereOf(e.vec3(AttributePosition))

	var d geometryDesc
	d.Flags = e.flags()
	d.IndexCount = uint32(len(e.indices))

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
		g.descs = append(g.descs, geometryDesc{})
	}
	g.descs[id] = d
	g.descDirty = true
	return id, gen
}

// Free releases a geometry id; its suballocations return to the TLSF pools and the
// slot's generation is bumped so existing handles become detectably stale.
func (g *Store) Free(id uint32) {
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
	g.descs[id] = geometryDesc{}
	g.descDirty = true
}

// allocIn suballocates bytes in a stream, growing (and repacking) if it's full.
func (g *Store) allocIn(stream int, bytes uint32) mem.Allocation {
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
func (g *Store) growStream(stream int, minCap uint32) {
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
		// A derived (compute-skinning output) entry has no CPU bytes to repack from
		// — its content is rewritten by the GPU every frame regardless — so just
		// re-suballocate at the same size and move on.
		if e.derived {
			old := e.allocs[stream]
			alloc, err := ns.tlsf.Alloc(old.Size())
			if err != nil {
				panic(fmt.Sprintf("render: repack of %s failed", s.label))
			}
			e.allocs[stream] = alloc
			g.descs[id].setBase(stream, alloc.Offset()/streamElemSize[stream])
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

// createSkinOutput allocates a derived geometry that receives compute-skinned
// vertex output: its own position range, and (if the source carries one) its own
// attribute range, both sized to the source's vertex count — but no CPU bytes and
// no index stream of its own, since it reuses the source's index range verbatim
// (skinning never changes topology). FlagSkinned is cleared on the output
// descriptor: once skinned, the data is plain triangles again.
func (g *Store) createSkinOutput(srcID uint32) (id, gen uint32) {
	if !g.entries.Alive(srcID) {
		panic("render: createSkinOutput on a dead geometry")
	}
	src := g.entries.Get(srcID)
	srcDesc := g.descs[srcID]
	n := src.attrs[AttributePosition].count
	if n == 0 {
		panic("render: createSkinOutput on an empty geometry")
	}

	var e entry
	e.derived = true
	e.attrs[AttributePosition].count = n // sizing only; no data
	e.derivedHasAttr = src.hasVertexAttrs()

	var d geometryDesc
	d.Flags = srcDesc.Flags &^ FlagSkinned
	d.IndexBase = srcDesc.IndexBase
	d.IndexCount = srcDesc.IndexCount

	posAlloc := g.allocIn(streamPos, uint32(n)*3*4)
	e.allocs[streamPos] = posAlloc
	d.setBase(streamPos, posAlloc.Offset()/streamElemSize[streamPos])

	if e.derivedHasAttr {
		attrAlloc := g.allocIn(streamAttr, uint32(n)*streamElemSize[streamAttr])
		e.allocs[streamAttr] = attrAlloc
		d.setBase(streamAttr, attrAlloc.Offset()/streamElemSize[streamAttr])
	}

	id, gen = g.entries.Alloc(e)
	if g.entries.Len() >= len(g.descs) {
		g.descs = append(g.descs, geometryDesc{})
	}
	g.descs[id] = d
	g.descDirty = true
	return id, gen
}

// Uploader is the minimal capability Sync needs to stage a copy into device memory.
// Declared here, by the consumer, rather than imported from wherever an
// implementation lives — this package takes no dependency on that package at all;
// anything with a matching Copy method (e.g. *upload.Uploader) satisfies it for free.
type Uploader interface {
	Copy(dst gpu.Buffer, dstOffset uint32, data []byte)
}

// Sync drains queued stream writes and the descriptor table into device memory
// through the uploader. Stream buffers and the descriptor buffer are MemoryDevice,
// so alloc/grow only enqueue; the actual staging + CopyBuffer happens here. Call
// once per frame before recording draws that read the *Addr getters. The caller
// flushes the uploader (submit + wait) after all subsystems have synced.
func (g *Store) Sync(u Uploader) {
	g.pendingWriteMu.Lock()
	for i := range g.pending {
		w := &g.pending[i]
		u.Copy(g.streams[w.stream].buf, w.offset, w.data)
	}
	g.pending = g.pending[:0]
	g.pendingWriteMu.Unlock()

	if !g.descDirty {
		return
	}
	g.ensureDescCap(uint64(len(g.descs)))
	if len(g.descs) > 0 {
		u.Copy(g.descBuf, 0, toBytes(g.descs))
	}
	g.descDirty = false
}

func (g *Store) ensureDescCap(need uint64) {
	needSize := need * descSize
	if g.descBuf.Size >= needSize {
		return
	}

	newSize := g.descBuf.Size
	for newSize < needSize {
		newSize *= 2
	}
	g.backend.Free(g.descBuf)
	g.descBuf = g.backend.Alloc(newSize, gpu.MemoryDevice, "Geometry Descriptors")
}

// writeStream enqueues packed bytes to be uploaded into a stream at a byte offset.
// The stream buffers are MemoryDevice, so the actual copy is deferred to Sync (via
// the uploader). Safe to call from multiple goroutines creating geometry.
func (g *Store) writeStream(stream int, byteOffset uint32, data []byte) {
	if len(data) == 0 {
		return
	}
	g.pendingWriteMu.Lock()
	g.pending = append(g.pending, pendingWrite{stream: stream, offset: byteOffset, data: data})
	g.pendingWriteMu.Unlock()
}

// Destroy releases all GPU buffers held by the store.
func (g *Store) Destroy() {
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
