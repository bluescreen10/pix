package pix

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/internal/mem"
)

// Stream indices. Position and index are their own SSBOs (so position-only
// passes — shadow/depth/cull — touch minimal memory); normal/color/uv are
// interleaved into one packed attributes SSBO. All three are TLSF-suballocated.
const (
	streamPos = iota
	streamAttr
	streamIndex
	streamCount
)

// streamElemSize is the byte size of one element of each stream, used to convert
// a TLSF byte offset into the element base stored in the descriptor.
var streamElemSize = [streamCount]uint32{
	streamPos:   4,  // f32 (position base is in f32 units)
	streamAttr:  16, // vertexAttributes
	streamIndex: 4,  // u32
}

// Geometry Set bindings (read-only storage, vertex stage).
const (
	// Geometry set bindings. Indices are NOT bound here — the index stream is a
	// real hardware index buffer (SetIndexBuffer), so the GPU's post-transform
	// vertex cache can de-duplicate shared vertices.
	GeoPositionsBinding   = 0
	GeoAttributesBinding  = 1
	GeoDescriptorsBinding = 2
)

// vertexAttributes is the interleaved per-vertex attribute record. Its fields
// pack with no std430 padding (16 bytes, vec2 aligned). Mirrors the WESL
// VertexAttributes struct.
type vertexAttributes struct {
	normal glm.RGB10A2 // unit normal, [-1,1] -> [0,1] per component
	color  glm.RGBA8
	uv     glm.Vec2f
}

// geometryDesc links a geometry id to its data in the streams. Bases are element
// offsets (byteOffset/streamElemSize). Mirrors the WESL GeometryDesc; 24 bytes.
type geometryDesc struct {
	positionBase  uint32
	attributeBase uint32
	indexBase     uint32
	indexCount    uint32
	flags         uint32
	_             uint32
}

func (d *geometryDesc) setBase(stream int, base uint32) {
	switch stream {
	case streamPos:
		d.positionBase = base
	case streamAttr:
		d.attributeBase = base
	case streamIndex:
		d.indexBase = base
	}
}

// Geometry attribute-presence flags, stored in geometryDesc.flags.
const (
	useNormal uint32 = 1 << iota
	useUVs
	useColor
)

// descSize is the byte size of one geometry descriptor row.
var descSize = uint32(unsafe.Sizeof(geometryDesc{}))

// initialGeoStreamBytes is the starting capacity of each stream.
const initialGeoStreamBytes uint32 = 1 << 16

type geometryStream struct {
	label string
	buf   gpu.Buffer
	usage gpu.BufferUsage
	tlsf  *mem.TLSF
}

// geometryEntry is the source of truth for one geometry: its original (unpacked)
// attributes and index list. The packed stream bytes are never stored — they are
// reconstructed from attrs/indices on demand (create, grow, SetAttribute) via
// streamBytes, so nothing is duplicated. Which streams exist is likewise derived
// (streamPresent), not stored. It holds each present stream's suballocation, the
// pipeline-facing flags and its local bounding sphere.
type geometryEntry struct {
	attrs   []*Attribute
	indices []uint32
	allocs  [streamCount]mem.Allocation
	flags   GeometryFlags
	bounds  Sphere
}

// attribute returns the entry's named attribute, or nil if absent.
func (e *geometryEntry) attribute(name string) *Attribute {
	for _, a := range e.attrs {
		if a.name == name {
			return a
		}
	}
	return nil
}

// streamPresent reports whether stream a exists for this geometry. Position and
// index streams are always present; the interleaved attribute stream exists iff
// the geometry has any normal/color/uv. (Presence is fixed at Create — SetAttribute
// only rewrites existing attributes.)
func (e *geometryEntry) streamPresent(a int) bool {
	if a == streamAttr {
		return e.hasVertexAttrs()
	}
	return true
}

// GeometrySystem owns geometry resources for GPU-driven, vertex-pulling
// rendering. It is the geometry resource table: it allocates generation-stamped
// ids (also drawable.geometryID), packs attributes into shared streams, and
// resolves an id back to both its CPU attributes and its GPU pull data.
type GeometrySystem struct {
	gpu *gpu.Instance

	streams [streamCount]*geometryStream

	// Slot-stable, generation-counted id table (also drawable.geometryID). descs
	// is the GPU descriptor mirror, kept parallel to the slab's indices.
	entries mem.Slab[geometryEntry]
	descs   []geometryDesc

	descBuf   gpu.Buffer
	descCap   uint32
	descDirty bool

	bindGroupLayout *wgpu.BindGroupLayout
	bindGroup       *wgpu.BindGroup
	bindGroupDirty  bool
}

// NewGeometrySystem creates the system, its streams, the descriptor buffer, and
// the bind group.
func NewGeometrySystem(g *gpu.Instance) *GeometrySystem {
	gs := &GeometrySystem{gpu: g}
	gs.init()
	return gs
}

func (gs *GeometrySystem) init() {
	gs.entries = mem.NewSlab[geometryEntry]()
	gs.streams[streamPos] = gs.newStream("Vertex Positions", initialGeoStreamBytes, gpu.BufferUsageStorage|gpu.BufferUsageCopyDst)
	gs.streams[streamAttr] = gs.newStream("Vertex Attributes", initialGeoStreamBytes, gpu.BufferUsageStorage|gpu.BufferUsageCopyDst)
	gs.streams[streamIndex] = gs.newStream("Vertex Indices", initialGeoStreamBytes, gpu.BufferUsageIndex|gpu.BufferUsageCopyDst)

	gs.descBuf = gs.gpu.CreateBuffer(gpu.BufferDescriptor{
		Label: "Geometry Descriptors",
		Size:  uint64(descSize),
		Usage: gpu.BufferUsageStorage | gpu.BufferUsageCopyDst,
	})
	gs.descCap = 1
	gs.bindGroupLayout = gs.gpu.CreateBindGroupLayout(wgpu.BindGroupLayoutDescriptor{
		Label:   "Geometry Set Layout",
		Entries: geometryBindGroupLayoutEntries(),
	})
	gs.rebuildBindGroup()
}

func (gs *GeometrySystem) newStream(label string, bytes uint32, usage gpu.BufferUsage) *geometryStream {
	return &geometryStream{
		buf: gs.gpu.CreateBuffer(gpu.BufferDescriptor{
			Label: label,
			Size:  uint64(bytes),
			Usage: usage,
		}),
		tlsf:  mem.NewTLSF(bytes),
		label: label,
		usage: usage,
	}
}

func geometryBindGroupLayoutEntries() []wgpu.BindGroupLayoutEntry {
	storage := func(binding uint32, minSize uint64) wgpu.BindGroupLayoutEntry {
		return wgpu.BindGroupLayoutEntry{
			Binding:    binding,
			Visibility: wgpu.ShaderStageVertex,
			Buffer: wgpu.BufferBindingLayout{
				Type:           wgpu.BufferBindingTypeReadOnlyStorage,
				MinBindingSize: minSize,
			},
		}
	}
	return []wgpu.BindGroupLayoutEntry{
		storage(GeoPositionsBinding, 4),
		storage(GeoAttributesBinding, uint64(streamElemSize[streamAttr])),
		storage(GeoDescriptorsBinding, uint64(descSize)),
	}
}

// BindGroupLayout returns the layout for the geometry set (for pipeline layouts).
func (gs *GeometrySystem) BindGroupLayout() *wgpu.BindGroupLayout { return gs.bindGroupLayout }

// BindGroup returns the current geometry set bind group. Call sync() first.
func (gs *GeometrySystem) BindGroup() *wgpu.BindGroup { return gs.bindGroup }

// indexCount returns a geometry's index count.
func (gs *GeometrySystem) indexCount(id uint32) uint32 { return gs.descs[id].indexCount }

// indexBase returns a geometry's first index (element offset) into the shared
// index buffer — the firstIndex for its indirect draw.
func (gs *GeometrySystem) indexBase(id uint32) uint32 { return gs.descs[id].indexBase }

// IndexBuffer returns the shared index stream, bound as a hardware index buffer.
func (gs *GeometrySystem) IndexBuffer() *wgpu.Buffer {
	return gs.gpu.RawBuffer(gs.streams[streamIndex].buf)
}

// Create allocates a generation-stamped geometry id from cfg and returns it.
// A position attribute is required; optional attributes must match the vertex
// count; a missing index buffer is generated (0..n-1).
func (gs *GeometrySystem) Create(cfg *GeometryConfig) (id, gen uint32) {
	posAttr := cfg.attribute(PositionAttrName)
	if posAttr == nil || posAttr.len == 0 {
		panic("geometry: config has no positions")
	}
	n := posAttr.len
	for _, a := range cfg.attrs {
		if a.len != n {
			panic(fmt.Sprintf("geometry: attribute %q length %d does not match %d positions", a.name, a.len, n))
		}
	}

	entry := geometryEntry{attrs: cfg.attrs, indices: cfg.indices, flags: cfg.flags}
	if len(entry.indices) == 0 {
		entry.indices = make([]uint32, n)
		for i := range entry.indices {
			entry.indices[i] = uint32(i)
		}
	}
	entry.bounds = boundingSphereOf(CastTo[glm.Vec3f](posAttr.data))

	var d geometryDesc
	d.flags = entry.vertexAttrFlags()
	d.indexCount = uint32(len(entry.indices))

	// Suballocate and upload each present stream. Growth repacks existing
	// geometries only (this entry is not yet in the slab), so it's safe here.
	for stream := range streamCount {
		if !entry.streamPresent(stream) {
			continue
		}
		data := entry.Bytes(stream)
		alloc := gs.allocIn(stream, uint32(len(data)))
		entry.allocs[stream] = alloc
		gs.gpu.WriteBuffer(gs.streams[stream].buf, uint64(alloc.Offset()), data)
		d.setBase(stream, alloc.Offset()/streamElemSize[stream])
	}

	id, gen = gs.entries.Alloc(entry)
	if gs.entries.Len() >= len(gs.descs) {
		gs.descs = append(gs.descs, geometryDesc{})
	}
	gs.descs[id] = d
	gs.descDirty = true
	return id, gen
}

// hasVertexAttrs reports whether the entry has any interleaved attribute
// (normal/color/uv), i.e. whether the attribute stream is present.
func (e *geometryEntry) hasVertexAttrs() bool {
	return e.attribute(NormalAttrName) != nil ||
		e.attribute(ColorAttrName) != nil ||
		e.attribute(UVAttrName) != nil
}

// vertexAttrFlags returns the descriptor presence flags (geoHas*) for the
// entry's optional interleaved attributes.
func (e *geometryEntry) vertexAttrFlags() uint32 {
	var f uint32
	if e.attribute(NormalAttrName) != nil {
		f |= useNormal
	}
	if e.attribute(ColorAttrName) != nil {
		f |= useColor
	}
	if e.attribute(UVAttrName) != nil {
		f |= useUVs
	}
	return f
}

// streamBytes reconstructs the packed byte payload for stream a from the entry's
// source attributes/indices. Position and index streams pass their attribute
// bytes through; the attribute stream interleaves normal/color/uv into
// vertexAttributes records.
func (e *geometryEntry) Bytes(stream int) []byte {
	switch stream {
	case streamPos:
		return e.attribute(PositionAttrName).data
	case streamIndex:
		return wgpu.ToBytes(e.indices)
	case streamAttr:
		return e.packVertexAttributes()
	}
	return nil
}

// packVertexAttributes interleaves the entry's optional normal/color/uv
// attributes into a vertexAttributes byte slice, or nil if it has none.
func (e *geometryEntry) packVertexAttributes() []byte {
	if !e.hasVertexAttrs() {
		return nil
	}
	n := e.attribute(PositionAttrName).len

	var normals []glm.Vec3f
	var colors []glm.Vec4f
	var uvs []glm.Vec2f
	if a := e.attribute(NormalAttrName); a != nil {
		normals = CastTo[glm.Vec3f](a.data)
	}
	if a := e.attribute(ColorAttrName); a != nil {
		colors = CastTo[glm.Vec4f](a.data)
	}
	if a := e.attribute(UVAttrName); a != nil {
		uvs = CastTo[glm.Vec2f](a.data)
	}

	attrs := make([]vertexAttributes, n)
	for i := 0; i < n; i++ {
		va := vertexAttributes{color: 0xFFFFFFFF} // default white
		if normals != nil {
			va.normal = normals[i].RGB10A2()
		}
		if colors != nil {
			va.color = colors[i].RGBA8()
		}
		if uvs != nil {
			va.uv = uvs[i]
		}
		attrs[i] = va
	}
	return wgpu.ToBytes(attrs)
}

// Get returns a pointer to a geometry's entry. Only valid while the id is alive.
func (gs *GeometrySystem) Get(id uint32) *geometryEntry { return gs.entries.Get(id) }

// Generation returns the current generation of a geometry id (for Ref.Valid).
func (gs *GeometrySystem) Generation(id uint32) uint32 { return gs.entries.Generation(id) }

// Free releases a geometry id; its suballocations return to the TLSF pools and
// the slot's generation is bumped so existing handles become detectably stale.
func (gs *GeometrySystem) Free(id uint32) {
	if !gs.entries.Alive(id) {
		return
	}
	e := gs.entries.Get(id)
	for a := 0; a < streamCount; a++ {
		if e.streamPresent(a) {
			gs.streams[a].tlsf.Free(e.allocs[a])
		}
	}
	*e = geometryEntry{} // release attr/index slices; slot is recycled on next Alloc
	gs.entries.Free(id)
	gs.descs[id] = geometryDesc{}
	gs.descDirty = true
}

// GetAttribute returns the raw bytes of a named vertex attribute.
func (gs *GeometrySystem) GetAttribute(id uint32, name string) []byte {
	a := gs.entries.Get(id).attribute(name)
	if a == nil {
		panic(fmt.Sprintf("geometry: attribute %q not found", name))
	}
	return a.data
}

// SetAttribute replaces a named vertex attribute's data and re-uploads the
// affected stream (repacking the interleaved stream for normal/color/uv, or the
// position stream for position, recomputing bounds). Panics if the geometry is
// Static.
func (gs *GeometrySystem) SetAttribute(id uint32, name string, data []byte) {
	entry := gs.entries.Get(id)
	if entry.flags&StaticFlag != 0 {
		panic("geometry: SetAttribute on a static geometry")
	}
	a := entry.attribute(name)
	if a == nil {
		panic(fmt.Sprintf("geometry: attribute %q not found", name))
	}
	a.SetBytes(data)

	// Position lives in its own stream; normal/color/uv share the interleaved
	// attribute stream. Attribute.SetBytes enforces the same length, so the
	// reconstructed bytes always fit the existing suballocation.
	if name == PositionAttrName {
		gs.reupload(id, streamPos)
		entry.bounds = boundingSphereOf(CastTo[glm.Vec3f](data))
		return
	}
	gs.reupload(id, streamAttr)
}

// reupload rebuilds stream a's packed bytes for geometry id and writes them in
// place when they still fit its existing suballocation, or reallocates (updating
// the descriptor base) when the size changed.
func (gs *GeometrySystem) reupload(id uint32, stream int) {
	entry := gs.entries.Get(id)
	data := entry.Bytes(stream)
	bytes := uint32(len(data))
	// SetBytes enforces identical length, so a present stream's bytes always fit
	// its existing suballocation — the in-place write is the normal path.
	if entry.allocs[stream].Size() >= bytes {
		gs.gpu.WriteBuffer(gs.streams[stream].buf, uint64(entry.allocs[stream].Offset()), data)
		return
	}
	gs.streams[stream].tlsf.Free(entry.allocs[stream])
	alloc := gs.allocIn(stream, bytes)
	entry.allocs[stream] = alloc
	gs.gpu.WriteBuffer(gs.streams[stream].buf, uint64(alloc.Offset()), data)
	gs.descs[id].setBase(stream, alloc.Offset()/streamElemSize[stream])
	gs.descDirty = true
}

// allocIn suballocates bytes in stream a, growing (and repacking the stream's
// live ranges) if it's full.
func (gs *GeometrySystem) allocIn(stream int, bytes uint32) mem.Allocation {
	s := gs.streams[stream]
	if alloc, err := s.tlsf.Alloc(bytes); err == nil {
		return alloc
	}
	free, _ := s.tlsf.StorageReport()
	used := s.tlsf.Capacity() - free
	gs.growStream(stream, used+bytes)
	alloc, err := gs.streams[stream].tlsf.Alloc(bytes)
	if err != nil {
		panic(fmt.Sprintf("geometry: %s alloc of %d bytes failed after grow", s.label, bytes))
	}
	return alloc
}

// growStream replaces stream a with a larger buffer (and fresh TLSF) sized to at
// least minCap, repacking every live geometry's range in that stream and updating
// their descriptor bases.
func (gs *GeometrySystem) growStream(stream int, minCap uint32) {
	s := gs.streams[stream]
	newCap := s.tlsf.Capacity()
	if newCap == 0 {
		newCap = initialGeoStreamBytes
	}
	for newCap < minCap {
		newCap *= 2
	}

	if s.buf.Valid() {
		gs.gpu.ReleaseBuffer(s.buf)
	}
	ns := gs.newStream(s.label, newCap, s.usage)
	gs.streams[stream] = ns

	// TODO: add a method to TLSF to Grow size and append
	// a new entry with the extra space at the end
	for id, e := range gs.entries.All() {
		if !e.streamPresent(stream) {
			continue
		}
		b := e.Bytes(stream)
		alloc, err := ns.tlsf.Alloc(uint32(len(b)))
		if err != nil {
			panic(fmt.Sprintf("geometry: repack of %s failed", s.label))
		}
		e.allocs[stream] = alloc
		gs.gpu.WriteBuffer(ns.buf, uint64(alloc.Offset()), b)
		gs.descs[id].setBase(stream, alloc.Offset()/streamElemSize[stream])
	}

	gs.descDirty = true
	gs.bindGroupDirty = true
}

// sync uploads the descriptor table when it changed and rebuilds the bind group
// when a stream buffer was replaced. Stream data is written incrementally on
// Create, so it needs nothing here.
func (gs *GeometrySystem) sync() {
	if gs.descDirty {
		gs.ensureDescCap(uint32(len(gs.descs)))
		if len(gs.descs) > 0 {
			gs.gpu.WriteBuffer(gs.descBuf, 0, wgpu.ToBytes(gs.descs))
		}
		gs.descDirty = false
	}
	if gs.bindGroupDirty {
		gs.rebuildBindGroup()
		gs.bindGroupDirty = false
	}
}

func (gs *GeometrySystem) ensureDescCap(need uint32) {
	if gs.descCap >= need {
		return
	}
	for gs.descCap < need {
		gs.descCap *= 2
	}
	gs.gpu.ReleaseBuffer(gs.descBuf)
	gs.descBuf = gs.gpu.CreateBuffer(gpu.BufferDescriptor{
		Label: "Geometry Descriptors",
		Size:  uint64(gs.descCap) * uint64(descSize),
		Usage: gpu.BufferUsageStorage | gpu.BufferUsageCopyDst,
	})
	gs.bindGroupDirty = true
}

func (gs *GeometrySystem) rebuildBindGroup() {
	if gs.bindGroup != nil {
		gs.bindGroup.Release()
	}
	entry := func(binding uint32, buf gpu.Buffer) wgpu.BindGroupEntry {
		return wgpu.BindGroupEntry{Binding: binding, Buffer: gs.gpu.RawBuffer(buf), Offset: 0, Size: wgpu.WholeSize}
	}
	gs.bindGroup = gs.gpu.CreateBindGroup(wgpu.BindGroupDescriptor{
		Label:  "Geometry Set",
		Layout: gs.bindGroupLayout,
		Entries: []wgpu.BindGroupEntry{
			entry(GeoPositionsBinding, gs.streams[streamPos].buf),
			entry(GeoAttributesBinding, gs.streams[streamAttr].buf),
			entry(GeoDescriptorsBinding, gs.descBuf),
		},
	})
}

// Destroy releases all GPU resources held by the system.
func (gs *GeometrySystem) Destroy() {
	if gs.bindGroup != nil {
		gs.bindGroup.Release()
		gs.bindGroup = nil
	}
	if gs.bindGroupLayout != nil {
		gs.bindGroupLayout.Release()
		gs.bindGroupLayout = nil
	}
	for a := 0; a < streamCount; a++ {
		if gs.streams[a] != nil && gs.streams[a].buf.Valid() {
			gs.gpu.ReleaseBuffer(gs.streams[a].buf)
		}
	}
	if gs.descBuf.Valid() {
		gs.gpu.ReleaseBuffer(gs.descBuf)
	}
}

// boundingSphereOf returns the local-space bounding sphere of a point set: the
// centroid and the max distance from it to any point.
func boundingSphereOf(points []glm.Vec3f) Sphere {
	var s Sphere
	if len(points) == 0 {
		return s
	}
	for _, p := range points {
		s.Center[0] += p[0]
		s.Center[1] += p[1]
		s.Center[2] += p[2]
	}
	inv := 1.0 / float32(len(points))
	s.Center[0] *= inv
	s.Center[1] *= inv
	s.Center[2] *= inv

	var maxDistSq float32
	for _, p := range points {
		d := p.Sub(s.Center)
		if dSq := d[0]*d[0] + d[1]*d[1] + d[2]*d[2]; dSq > maxDistSq {
			maxDistSq = dSq
		}
	}
	s.Radius = float32(math.Sqrt(float64(maxDistSq)))
	return s
}
