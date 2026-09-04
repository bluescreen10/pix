// Package render is the bindless, GPU-driven renderer built on the gpu. It is the
// port of pix's WebGPU renderer onto the "no graphics API" model: no bind groups,
// no descriptor sets — every buffer is reached through a 64-bit device address
// (BDA) carried in a per-draw root struct, and textures through the bindless heap.
package pix

import (
	"fmt"
	"math"
	"sync"
	"unsafe"

	"github.com/bluescreen10/pix/colors"
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

// Attribute-presence flags stored in GeometryDesc.Flags (mirrors the GLSL).
const (
	FlagNormal uint32 = 1 << iota
	FlagUV
	FlagColor
	FlagSkinned
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
	normal glm.Unorm10x3 // unit normal, [-1,1] remapped to the unsigned [0,1]
	color  colors.RGBA8
	uv     glm.Vec2f
}

// vertexSkin is the interleaved per-vertex skin record (16 bytes): 4 skeleton-
// relative joint indices + 4 unorm16 weights (normalized to sum 1 at pack time).
// Same 4-word shape as vertexAttributes, so it addresses identically in the GLSL.
type vertexSkin struct {
	joints  [4]uint16
	weights [4]uint16 // unorm16
}

// GeometryDesc links a geometry id to its data in the shared streams. Bases are
// element offsets (byteOffset / streamElemSize). 24 bytes; matches the GLSL GeoDesc.
type GeometryDesc struct {
	PositionBase  uint32
	AttributeBase uint32
	IndexBase     uint32
	IndexCount    uint32
	Flags         uint32
	SkinBase      uint32
}

func (d *GeometryDesc) setBase(stream int, base uint32) {
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

var descSize = uint64(unsafe.Sizeof(GeometryDesc{}))

// AttributeType is a vertex attribute's semantic role.
type AttributeType uint8

const (
	AttributePosition AttributeType = iota
	AttributeNormal
	AttributeColor
	AttributeUV
	AttributeSkinIndex  // skeleton-relative joint indices, per vertex (up to 4)
	AttributeSkinWeight // per-joint blend weights, per vertex (normalized at pack time)
	attributeCount
)

// DataType is an attribute element's CPU format. It records how NewAttribute's
// typed slice was laid out so GetAttributeData can hand it back in the same shape.
type DataType uint8

const (
	Float32 DataType = iota
	Float32x2
	Float32x3
	Float32x4
	Float64
	Int32
	Uint32
	Uint16x4
)

// canonicalDataType is the element format the fixed GPU packer expects for each
// known attribute; Create rejects mismatches.
var canonicalDataType = [attributeCount]DataType{
	AttributePosition:   Float32x3,
	AttributeNormal:     Float32x3,
	AttributeColor:      Float32x4,
	AttributeUV:         Float32x2,
	AttributeSkinIndex:  Uint16x4,
	AttributeSkinWeight: Float32x4,
}

// Attribute is one named vertex stream, built with NewAttribute.
type Attribute struct {
	attrType AttributeType
	dataType DataType
	data     []byte // the typed slice reinterpreted as raw bytes (no copy)
	count    int    // element (vertex) count
}

// NewAttribute wraps a typed slice as an Attribute. dataType records the element
// layout (so GetAttributeData returns the same shape); data is reinterpreted as
// raw bytes without copying, so the backing array must outlive the geometry.
func NewAttribute[T any](attrType AttributeType, dataType DataType, data []T) Attribute {
	return Attribute{attrType: attrType, dataType: dataType, data: toBytes(data), count: len(data)}
}

// GeometryConfig is a geometry's source data: a set of vertex attributes (Position
// required, others optional and matching the position count), an optional index
// list (nil → generated 0..n-1), and a Static hint. Static has no effect today; it
// is reserved for a future acceleration structure (BVH for collision / lighting).
type GeometryConfig struct {
	Attributes []Attribute
	Indices    []uint32
	Static     bool
}

// entry is the source of truth for one geometry. Attributes hold the original CPU
// bytes (retained for grow-repacking and GetAttributeData); the packed GPU stream
// bytes are reconstructed on demand. It holds each present stream's suballocation
// and the local bounds.
//
// derived marks a geometry with no CPU-side attribute bytes at all: a compute-
// skinning output range (see createSkinOutput), sized to mirror a source geometry's
// vertex count but filled by the GPU every frame instead of uploaded once. Its
// position/attribute presence is tracked directly (derivedHasAttr) rather than
// through has()/hasVertexAttrs(), which read attrs[].count — a derived entry only
// sets AttributePosition's count (for sizing) and carries no other attribute data.
type entry struct {
	attrs          [attributeCount]Attribute
	indices        []uint32
	static         bool
	derived        bool
	derivedHasAttr bool
	allocs         [streamCount]mem.Allocation
	bounds         Sphere
}

// has reports whether an attribute is present (non-empty).
func (e *entry) has(t AttributeType) bool {
	return e.attrs[t].count > 0
}

// vec2/vec3/vec4/vec4u16 return typed read-only views over an attribute's stored bytes.
func (e *entry) vec2(t AttributeType) []glm.Vec2f {
	return fromBytes[glm.Vec2f](e.attrs[t].data, e.attrs[t].count)
}

func (e *entry) vec3(t AttributeType) []glm.Vec3f {
	return fromBytes[glm.Vec3f](e.attrs[t].data, e.attrs[t].count)
}

func (e *entry) vec4(t AttributeType) []glm.Vec4f {
	return fromBytes[glm.Vec4f](e.attrs[t].data, e.attrs[t].count)
}

func (e *entry) rgba(t AttributeType) []colors.RGBA32F {
	return fromBytes[colors.RGBA32F](e.attrs[t].data, e.attrs[t].count)
}

func (e *entry) vec4u16(t AttributeType) []glm.Vec4[uint16] {
	return fromBytes[glm.Vec4[uint16]](e.attrs[t].data, e.attrs[t].count)
}

func (e *entry) hasVertexAttrs() bool {
	return e.has(AttributeNormal) || e.has(AttributeColor) || e.has(AttributeUV)
}

// hasSkin reports whether both skin attributes are present. Create() rejects one
// without the other, so this is also the single source of truth for FlagSkinned.
func (e *entry) hasSkin() bool {
	return e.has(AttributeSkinIndex) && e.has(AttributeSkinWeight)
}

func (e *entry) streamPresent(stream int) bool {
	switch stream {
	case streamPos:
		return true
	case streamAttr:
		if e.derived {
			return e.derivedHasAttr
		}
		return e.hasVertexAttrs()
	case streamIndex:
		return !e.derived // a derived (skin output) entry reuses its source's index range
	case streamSkin:
		return !e.derived && e.hasSkin()
	}
	return false
}

func (e *entry) flags() uint32 {
	var f uint32
	if e.has(AttributeNormal) {
		f |= FlagNormal
	}
	if e.has(AttributeUV) {
		f |= FlagUV
	}
	if e.has(AttributeColor) {
		f |= FlagColor
	}
	if e.hasSkin() {
		f |= FlagSkinned
	}
	return f
}

// bytes reconstructs the packed byte payload for a stream from the attributes. Never
// called for a derived entry (it has none — see growStream's derived special case).
func (e *entry) bytes(stream int) []byte {
	switch stream {
	case streamPos:
		return e.attrs[AttributePosition].data // raw f32x3, same layout as the stream
	case streamIndex:
		return toBytes(e.indices)
	case streamAttr:
		return e.packAttributes()
	case streamSkin:
		return e.packSkin()
	}
	return nil
}

func (e *entry) packAttributes() []byte {
	if !e.hasVertexAttrs() {
		return nil
	}
	n := e.attrs[AttributePosition].count
	normals, vertColors, uvs := e.vec3(AttributeNormal), e.rgba(AttributeColor), e.vec2(AttributeUV)
	attrs := make([]vertexAttributes, n)
	for i := 0; i < n; i++ {
		va := vertexAttributes{color: colors.RGBA8{255, 255, 255, 255}} // default white
		if i < len(normals) {
			// Unorm10x3 stores unsigned [0,1], so remap the signed normal here. The
			// other half of this codec is in scene_draw.vert.glsl ("* 2.0 - 1.0");
			// the two have to stay in step.
			nn := normals[i]
			va.normal = glm.Vec3f{nn[0]*0.5 + 0.5, nn[1]*0.5 + 0.5, nn[2]*0.5 + 0.5}.Unorm10x3()
		}
		if i < len(vertColors) {
			va.color = vertColors[i].RGBA8()
		}
		if i < len(uvs) {
			va.uv = uvs[i]
		}
		attrs[i] = va
	}
	return toBytes(attrs)
}

// packSkin builds the packed skin-record stream from the skin index/weight
// attributes: weights are normalized to sum 1 (an all-zero record falls back to
// full weight on joint 0) and quantized to unorm16.
func (e *entry) packSkin() []byte {
	if !e.hasSkin() {
		return nil
	}
	n := e.attrs[AttributePosition].count
	joints, weights := e.vec4u16(AttributeSkinIndex), e.vec4(AttributeSkinWeight)
	out := make([]vertexSkin, n)
	for i := 0; i < n; i++ {
		var j glm.Vec4[uint16]
		if i < len(joints) {
			j = joints[i]
		}
		w := glm.Vec4f{1, 0, 0, 0}
		if i < len(weights) {
			if sum := weights[i][0] + weights[i][1] + weights[i][2] + weights[i][3]; sum > 0 {
				inv := 1 / sum
				w = glm.Vec4f{weights[i][0] * inv, weights[i][1] * inv, weights[i][2] * inv, weights[i][3] * inv}
			}
		}
		out[i] = vertexSkin{
			joints:  [4]uint16{j[0], j[1], j[2], j[3]},
			weights: [4]uint16{quantizeUnorm16(w[0]), quantizeUnorm16(w[1]), quantizeUnorm16(w[2]), quantizeUnorm16(w[3])},
		}
	}
	return toBytes(out)
}

// quantizeUnorm16 clamps v to [0,1] and quantizes it to a 16-bit unsigned-normalized value.
func quantizeUnorm16(v float32) uint16 {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return uint16(v*65535 + 0.5)
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
	descDirty bool

	// Stream buffers live in MemoryDevice, so Create/grow cannot write them
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

// newGeometrySystem creates the system, its three streams, and the descriptor buffer.
func newGeometrySystem(backend gpu.Backend) *geometrySystem {
	g := &geometrySystem{backend: backend, entries: mem.NewSlab[entry]()}
	g.streams[streamPos] = g.newStream("Vertex Positions", initialStreamBytes)
	g.streams[streamAttr] = g.newStream("Vertex Attributes", initialStreamBytes)
	g.streams[streamIndex] = g.newStream("Vertex Indices", initialStreamBytes)
	g.streams[streamSkin] = g.newStream("Vertex Skin", initialStreamBytes)
	g.descBuf = backend.Alloc(descSize, gpu.MemoryDevice, "Geometry Descriptors")
	return g
}

func (g *geometrySystem) newStream(label string, bytes uint32) *stream {
	return &stream{
		label: label,
		buf:   g.backend.Alloc(uint64(bytes), gpu.MemoryDevice, label),
		tlsf:  mem.NewTLSF(bytes),
	}
}

// PositionsAddr / AttributesAddr / DescriptorsAddr return the current device
// addresses of the shared buffers (they change when a stream grows, so read them
// each frame when building root structs).
func (g *geometrySystem) PositionsAddr() uint64 {
	return g.streams[streamPos].buf.Addr
}

func (g *geometrySystem) AttributesAddr() uint64 {
	return g.streams[streamAttr].buf.Addr
}

func (g *geometrySystem) DescriptorsAddr() uint64 {
	return g.descBuf.Addr
}

// SkinAddr returns the current device address of the shared skin-record stream
// (joint indices + weights). Changes when the stream grows, like the others.
func (g *geometrySystem) SkinAddr() uint64 {
	return g.streams[streamSkin].buf.Addr
}

// IndexBuffer returns the shared index stream as a hardware index buffer, for
// DrawIndexed / DrawIndexedIndirect. A geometry's firstIndex is its Desc().IndexBase.
func (g *geometrySystem) IndexBuffer() gpu.Buffer {
	return g.streams[streamIndex].buf
}

// Desc returns a copy of a geometry's GPU descriptor (bases + counts + flags).
func (g *geometrySystem) Desc(id uint32) GeometryDesc {
	return g.descs[id]
}

// Bounds returns a geometry's local bounding sphere.
func (g *geometrySystem) Bounds(id uint32) Sphere {
	return g.entries.Get(id).bounds
}

// Generation returns the current generation of a geometry id (for stale checks).
func (g *geometrySystem) Generation(id uint32) uint32 {
	return g.entries.Generation(id)
}

// --- ref-counted handle (renderer-owned resource) ---

// Geometry is a ref-counted handle to a renderer-owned geometry. Clone with Copy();
// surrender ownership with Release() (the geometry is freed at refcount 0). It caches
// the local bounding sphere so scenes/loaders can frame without the geometry system.
type Geometry struct {
	ref    Ref
	bounds Sphere
}

// Copy returns another handle to the same geometry, bumping the refcount.
func (g Geometry) Copy() Geometry {
	return Geometry{ref: g.ref.Copy(), bounds: g.bounds}
}

// Release drops this handle's reference; the geometry is freed when the last is released.
func (g Geometry) Release() {
	g.ref.Release()
}

// Valid reports whether the underlying geometry is still alive.
func (g Geometry) Valid() bool {
	return g.ref.Valid()
}

// GetAttributeData returns a copy of a stored attribute reinterpreted as
// []T (e.g. GetAttributeData[glm.Vec3f](AttributePosition)), or nil if the attribute
// is absent. T must match the element layout the attribute was created with. Do not
// mutate the returned slice — it aliases the geometry's internal bytes.
func (g Geometry) GetAttributeData[T any](t AttributeType) []T {
	sys, ok := g.ref.owner.(*geometrySystem)
	if !ok {
		return nil
	}
	a := sys.attribute(g.ref.id, t)
	if a == nil {
		return nil
	}
	return fromBytes[T](a.data, a.count)
}

// SetAttributeData replaces a present attribute's data and re-uploads it. T and the
// element count must match what the attribute was created with (same byte length);
// this does not add attributes or resize — use NewGeometry for that. Changing
// AttributePosition also recomputes bounds (note: a Geometry handle caches bounds
// from creation, so existing handles keep their old BoundingSphere).
func (g Geometry) SetAttributeData[T any](t AttributeType, data []T) {
	sys, ok := g.ref.owner.(*geometrySystem)
	if !ok {
		return
	}
	sys.setAttribute(g.ref.id, t, toBytes(data), len(data))
}

// BoundingSphere returns the geometry's local-space bounding sphere.
func (g Geometry) BoundingSphere() Sphere {
	return g.bounds
}

// id is the geometry's slot in the system (used by drawables).
func (g Geometry) id() uint32 {
	return g.ref.id
}

// skinOutput allocates a derived output geometry that receives this geometry's
// compute-skinned vertex data (see geometrySystem.createSkinOutput), and returns a
// fresh single-ref handle to it. g must carry skin index/weight attributes.
// Package-internal — used by Scene.NewSkinnedMesh.
func (g Geometry) skinOutput() Geometry {
	sys, ok := g.ref.owner.(*geometrySystem)
	if !ok {
		panic("render: skinOutput on a geometry with no owning system")
	}
	id, gen := sys.createSkinOutput(g.ref.id)
	rc := int32(1)
	return Geometry{ref: Ref{id: id, gen: gen, refCount: &rc, owner: sys}, bounds: sys.Bounds(id)}
}

// attribute returns a pointer to a geometry's stored attribute (nil if the id is
// dead or the attribute is absent). The generic GetAttributeData/SetAttributeData
// methods reinterpret its bytes.
func (g *geometrySystem) attribute(id uint32, t AttributeType) *Attribute {
	if !g.entries.Alive(id) || t >= attributeCount {
		return nil
	}
	e := g.entries.Get(id)
	if !e.has(t) {
		return nil
	}
	return &e.attrs[t]
}

// setAttribute replaces a present attribute's bytes in place and re-uploads the
// affected stream. The element size and count must match the existing attribute
// (so the suballocation still fits); adding a new attribute or resizing is not
// supported — use NewGeometry for that.
func (g *geometrySystem) setAttribute(id uint32, t AttributeType, data []byte, count int) {
	if !g.entries.Alive(id) || t >= attributeCount {
		return
	}
	e := g.entries.Get(id)
	if !e.has(t) {
		panic(fmt.Sprintf("render: SetAttributeData on absent attribute %d (use NewGeometry to add it)", t))
	}
	old := &e.attrs[t]
	if count != old.count || len(data) != len(old.data) {
		panic(fmt.Sprintf("render: SetAttributeData size mismatch for attribute %d (want %d bytes / %d elems)", t, len(old.data), old.count))
	}
	old.data = data
	// Re-upload the affected stream into its existing suballocation.
	if t == AttributePosition {
		g.writeStream(streamPos, e.allocs[streamPos].Offset(), e.bytes(streamPos))
		e.bounds = boundingSphereOf(e.vec3(AttributePosition))
	} else {
		g.writeStream(streamAttr, e.allocs[streamAttr].Offset(), e.bytes(streamAttr))
	}
}

// Disposer: dispose/generation let a Ref own a slot in this system.
func (g *geometrySystem) dispose(id uint32) {
	g.Free(id)
}

func (g *geometrySystem) generation(id uint32) uint32 {
	return g.entries.Generation(id)
}

// newHandle allocates a geometry from cfg and returns a fresh single-ref handle.
func (g *geometrySystem) newHandle(cfg GeometryConfig) Geometry {
	id, gen := g.Create(cfg)
	rc := int32(1)
	return Geometry{ref: Ref{id: id, gen: gen, refCount: &rc, owner: g}, bounds: g.Bounds(id)}
}

// Create allocates a generation-stamped geometry id from cfg, uploads its streams,
// and records its descriptor. A Position attribute is required; other attributes
// must match the position count and carry their canonical dataType; a nil index
// list is generated 0..n-1.
func (g *geometrySystem) Create(cfg GeometryConfig) (id, gen uint32) {
	var e entry
	e.static = cfg.Static
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
	e.bounds = boundingSphereOf(e.vec3(AttributePosition))

	var d GeometryDesc
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
func (g *geometrySystem) createSkinOutput(srcID uint32) (id, gen uint32) {
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

	var d GeometryDesc
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
		g.descs = append(g.descs, GeometryDesc{})
	}
	g.descs[id] = d
	g.descDirty = true
	return id, gen
}

// Sync drains queued stream writes and the descriptor table into device memory
// through the uploader. Stream buffers and the descriptor buffer are MemoryDevice,
// so Create/grow only enqueue; the actual staging + CopyBuffer happens here. Call
// once per frame before recording draws that read the *Addr getters. The caller
// flushes the uploader (submit + wait) after all subsystems have synced.
func (g *geometrySystem) Sync(u *uploader) {
	g.pendingWriteMu.Lock()
	for i := range g.pending {
		w := &g.pending[i]
		u.copy(g.streams[w.stream].buf, w.offset, w.data)
	}
	g.pending = g.pending[:0]
	g.pendingWriteMu.Unlock()

	if !g.descDirty {
		return
	}
	g.ensureDescCap(uint64(len(g.descs)))
	if len(g.descs) > 0 {
		u.copy(g.descBuf, 0, toBytes(g.descs))
	}
	g.descDirty = false
}

func (g *geometrySystem) ensureDescCap(need uint64) {
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
func (g *geometrySystem) writeStream(stream int, byteOffset uint32, data []byte) {
	if len(data) == 0 {
		return
	}
	g.pendingWriteMu.Lock()
	g.pending = append(g.pending, pendingWrite{stream: stream, offset: byteOffset, data: data})
	g.pendingWriteMu.Unlock()
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

// fromBytes reinterprets raw bytes as a slice of n elements of T (no copy).
func fromBytes[T any](b []byte, n int) []T {
	if n == 0 || len(b) == 0 {
		return nil
	}
	return unsafe.Slice((*T)(unsafe.Pointer(&b[0])), n)
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
