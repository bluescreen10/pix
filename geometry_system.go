package pix

import (
	"fmt"
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
	GeoPositionsBinding   = streamPos
	GeoAttributesBinding  = streamAttr
	GeoIndicesBinding     = streamIndex
	GeoDescriptorsBinding = streamCount
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
	geoHasNormal uint32 = 1 << 0
	geoHasUV     uint32 = 1 << 1
	geoHasColor  uint32 = 1 << 2
)

// descSize is the byte size of one geometry descriptor row.
var descSize = uint32(unsafe.Sizeof(geometryDesc{}))

// initialGeoStreamBytes is the starting capacity of each stream.
const initialGeoStreamBytes uint32 = 1 << 16

// GeometryConfig describes one geometry's attribute data. Positions are
// required; the rest are optional but, when present, must have the same length
// as Positions. If Indices is nil an index buffer is generated (0..N-1), since
// the system only supports indexed drawing.
type GeometryConfig struct {
	Positions []glm.Vec3f
	Normals   []glm.Vec3f
	UVs       []glm.Vec2f
	Colors    []glm.Vec4f
	Indices   []uint32
}

type geoStream struct {
	buf      gpu.Buffer
	tlsf     *mem.TLSF
	capacity uint32
	label    string
}

// geometryEntry retains a geometry's raw stream bytes (so a stream can be
// repacked when it grows) and its suballocations (so they can be freed).
type geometryEntry struct {
	data   [streamCount][]byte
	allocs [streamCount]mem.Allocation
	has    [streamCount]bool
	flags  uint32
	alive  bool
}

// GeometrySystem owns geometry attribute storage for GPU-driven, vertex-pulling
// rendering. It is keyed by the geometry's slab id (also drawable.geometryID),
// so one id resolves both the CPU GeometryData and the GPU pull data.
type GeometrySystem struct {
	gpu *gpu.Instance

	streams [streamCount]*geoStream
	entries []geometryEntry

	descs     []geometryDesc
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
	labels := [streamCount]string{"Geometry Positions", "Geometry Attributes", "Geometry Indices"}
	for a := 0; a < streamCount; a++ {
		gs.streams[a] = gs.newStream(labels[a], initialGeoStreamBytes)
	}
	gs.descBuf = gs.gpu.CreateBuffer(gpu.BufferDescriptor{
		Label: "Geometry Descriptors",
		Size:  uint64(descSize),
		Usage: gpu.BufferUsageStorage | gpu.BufferUsageCopyDst,
	})
	gs.descCap = 1
	gs.bindGroupLayout = g.CreateBindGroupLayout(wgpu.BindGroupLayoutDescriptor{
		Label:   "Geometry Set Layout",
		Entries: geometryBindGroupLayoutEntries(),
	})
	gs.rebuildBindGroup()
	return gs
}

func (gs *GeometrySystem) newStream(label string, bytes uint32) *geoStream {
	return &geoStream{
		buf: gs.gpu.CreateBuffer(gpu.BufferDescriptor{
			Label: label,
			Size:  uint64(bytes),
			Usage: gpu.BufferUsageStorage | gpu.BufferUsageCopyDst,
		}),
		tlsf:     mem.NewTLSF(bytes),
		capacity: bytes,
		label:    label,
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
		storage(GeoIndicesBinding, 4),
		storage(GeoDescriptorsBinding, uint64(descSize)),
	}
}

// BindGroupLayout returns the layout for the geometry set (for pipeline layouts).
func (gs *GeometrySystem) BindGroupLayout() *wgpu.BindGroupLayout { return gs.bindGroupLayout }

// BindGroup returns the current geometry set bind group. Call sync() first.
func (gs *GeometrySystem) BindGroup() *wgpu.BindGroup { return gs.bindGroup }

// Create registers a geometry under the given id (its slab id). Positions are
// required; optional attributes must match the vertex count; a missing index
// buffer is generated.
func (gs *GeometrySystem) Create(id uint32, cfg GeometryConfig) {
	n := len(cfg.Positions)
	if n == 0 {
		panic("geometry: config has no positions")
	}
	check := func(name string, got int) {
		if got != 0 && got != n {
			panic(fmt.Sprintf("geometry: %s length %d does not match %d positions", name, got, n))
		}
	}
	check("normals", len(cfg.Normals))
	check("uvs", len(cfg.UVs))
	check("colors", len(cfg.Colors))

	var e geometryEntry
	e.alive = true

	e.data[streamPos] = copyBytes(wgpu.ToBytes(CastTo[float32](cfg.Positions)))
	e.has[streamPos] = true

	indices := cfg.Indices
	if len(indices) == 0 {
		indices = make([]uint32, n)
		for i := range indices {
			indices[i] = uint32(i)
		}
	}
	e.data[streamIndex] = copyBytes(wgpu.ToBytes(indices))
	e.has[streamIndex] = true

	hasNormal := len(cfg.Normals) > 0
	hasUV := len(cfg.UVs) > 0
	hasColor := len(cfg.Colors) > 0
	if hasNormal {
		e.flags |= geoHasNormal
	}
	if hasUV {
		e.flags |= geoHasUV
	}
	if hasColor {
		e.flags |= geoHasColor
	}
	if hasNormal || hasUV || hasColor {
		attrs := make([]vertexAttributes, n)
		for i := 0; i < n; i++ {
			va := vertexAttributes{color: 0xFFFFFFFF} // default white
			if hasNormal {
				va.normal = cfg.Normals[i].RGB10A2()
			}
			if hasColor {
				va.color = cfg.Colors[i].RGBA8()
			}
			if hasUV {
				va.uv = cfg.UVs[i]
			}
			attrs[i] = va
		}
		e.data[streamAttr] = copyBytes(wgpu.ToBytes(attrs))
		e.has[streamAttr] = true
	}

	// Suballocate and upload each present stream. Growth repacks existing
	// geometries only (this entry is not in gs.entries yet), so it's safe here.
	var d geometryDesc
	d.flags = e.flags
	d.indexCount = uint32(len(indices))
	for a := 0; a < streamCount; a++ {
		if !e.has[a] {
			continue
		}
		alloc := gs.allocIn(a, uint32(len(e.data[a])))
		e.allocs[a] = alloc
		gs.gpu.WriteBuffer(gs.streams[a].buf, uint64(alloc.Offset()), e.data[a])
		d.setBase(a, alloc.Offset()/streamElemSize[a])
	}

	gs.ensureID(id)
	gs.entries[id] = e
	gs.descs[id] = d
	gs.descDirty = true
}

// ensureID grows the entry/descriptor arrays so index id is addressable.
func (gs *GeometrySystem) ensureID(id uint32) {
	for uint32(len(gs.entries)) <= id {
		gs.entries = append(gs.entries, geometryEntry{})
		gs.descs = append(gs.descs, geometryDesc{})
	}
}

// Free releases a geometry id; its suballocations return to the TLSF pools.
func (gs *GeometrySystem) Free(id uint32) {
	if id >= uint32(len(gs.entries)) || !gs.entries[id].alive {
		return
	}
	e := &gs.entries[id]
	for a := 0; a < streamCount; a++ {
		if e.has[a] {
			gs.streams[a].tlsf.Free(e.allocs[a])
		}
	}
	*e = geometryEntry{}
	gs.descs[id] = geometryDesc{}
	gs.descDirty = true
}

// allocIn suballocates bytes in stream a, growing (and repacking the stream's
// live ranges) if it's full.
func (gs *GeometrySystem) allocIn(a int, bytes uint32) mem.Allocation {
	s := gs.streams[a]
	if alloc, err := s.tlsf.Alloc(bytes); err == nil {
		return alloc
	}
	free, _ := s.tlsf.StorageReport()
	used := s.capacity - free
	gs.growStream(a, used+bytes)
	alloc, err := gs.streams[a].tlsf.Alloc(bytes)
	if err != nil {
		panic(fmt.Sprintf("geometry: %s alloc of %d bytes failed after grow", s.label, bytes))
	}
	return alloc
}

// growStream replaces stream a with a larger buffer (and fresh TLSF) sized to at
// least minCap, repacking every live geometry's range in that stream and updating
// their descriptor bases.
func (gs *GeometrySystem) growStream(a int, minCap uint32) {
	s := gs.streams[a]
	newCap := s.capacity
	if newCap == 0 {
		newCap = initialGeoStreamBytes
	}
	for newCap < minCap {
		newCap *= 2
	}

	if s.buf.Valid() {
		gs.gpu.ReleaseBuffer(s.buf)
	}
	ns := gs.newStream(s.label, newCap)
	gs.streams[a] = ns

	for id := range gs.entries {
		e := &gs.entries[id]
		if !e.alive || !e.has[a] {
			continue
		}
		alloc, err := ns.tlsf.Alloc(uint32(len(e.data[a])))
		if err != nil {
			panic(fmt.Sprintf("geometry: repack of %s failed", s.label))
		}
		e.allocs[a] = alloc
		gs.gpu.WriteBuffer(ns.buf, uint64(alloc.Offset()), e.data[a])
		gs.descs[id].setBase(a, alloc.Offset()/streamElemSize[a])
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
			entry(GeoIndicesBinding, gs.streams[streamIndex].buf),
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

func copyBytes(b []byte) []byte {
	return append([]byte(nil), b...)
}

// geometryConfigFromData extracts a GeometryConfig from a GeometryData's CPU
// attribute bytes so it can be registered with the GeometrySystem.
func geometryConfigFromData(g *GeometryData) GeometryConfig {
	cfg := GeometryConfig{Indices: g.indices}
	for _, a := range g.attrs {
		switch a.name {
		case PositionAttrName:
			cfg.Positions = CastTo[glm.Vec3f](a.data)
		case NormalAttrName:
			cfg.Normals = CastTo[glm.Vec3f](a.data)
		case UVAttrName:
			cfg.UVs = CastTo[glm.Vec2f](a.data)
		}
	}
	return cfg
}
