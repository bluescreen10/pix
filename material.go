package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
)

// NoTexture is the colorMap value for a material with no bound texture.
const NoTexture uint32 = 0xFFFFFFFF

// Material flag bits (mirror scene_material.frag).
const (
	MatColorMap uint32 = 1 << 0
)

// MaterialDesc describes a material. Textures are bindless heap indices
// (Texture.Index / Sampler.Index), not bindings.
type MaterialDesc struct {
	BaseColor glm.Vec4f
	ColorMap  uint32 // heap sampled-image index, or NoTexture
	Sampler   uint32 // heap sampler index
}

// gpuMaterial is one row of the material table (scalar, 32 bytes); matches the
// GLSL Material struct.
type gpuMaterial struct {
	baseColor [4]float32
	colorMap  uint32
	sampler   uint32
	flags     uint32
	_         uint32
}

var materialSize = uint32(unsafe.Sizeof(gpuMaterial{}))

// materialSystem is the renderer-owned material table: a growable BDA buffer of
// material records indexed by materialID. No bind groups — the draw shader reads
// materials[materialID] through the address in its root struct and samples heap
// textures by the stored index. Freed slots are recycled (generation-stamped).
type materialSystem struct {
	backend gpu.Backend
	records []gpuMaterial
	gens    []uint32
	free    []uint32
	buf     gpu.Buffer
	cap     uint32
	dirty   bool
}

func newMaterialSystem(b gpu.Backend) *materialSystem {
	m := &materialSystem{backend: b, cap: 1}
	m.buf = b.Alloc(uint64(materialSize), gpu.MemoryHost, "Materials")
	return m
}

// create records a material and returns its slot id.
func (m *materialSystem) create(desc MaterialDesc) uint32 {
	var flags uint32
	if desc.ColorMap != NoTexture {
		flags |= MatColorMap
	}
	rec := gpuMaterial{
		baseColor: [4]float32{desc.BaseColor[0], desc.BaseColor[1], desc.BaseColor[2], desc.BaseColor[3]},
		colorMap:  desc.ColorMap,
		sampler:   desc.Sampler,
		flags:     flags,
	}
	m.dirty = true
	if n := len(m.free); n > 0 {
		id := m.free[n-1]
		m.free = m.free[:n-1]
		m.records[id] = rec
		return id
	}
	id := uint32(len(m.records))
	m.records = append(m.records, rec)
	m.gens = append(m.gens, 1)
	return id
}

// Addr returns the material table's device address (re-read after Sync).
func (m *materialSystem) Addr() uint64 { return m.buf.Addr }

// Sync uploads the table when it changed, growing the buffer as needed.
func (m *materialSystem) Sync() {
	if !m.dirty {
		return
	}
	need := uint32(len(m.records))
	if need > m.cap {
		for m.cap < need {
			m.cap *= 2
		}
		m.backend.Free(m.buf)
		m.buf = m.backend.Alloc(uint64(m.cap)*uint64(materialSize), gpu.MemoryHost, "Materials")
	}
	if len(m.records) > 0 {
		writeAt(m.buf, 0, toBytes(m.records))
	}
	m.dirty = false
}

// Destroy releases the table buffer.
func (m *materialSystem) Destroy() {
	if m.buf.Valid() {
		m.backend.Free(m.buf)
		m.buf = gpu.Buffer{}
	}
}

// Disposer: dispose/generation let a Ref own a slot.
func (m *materialSystem) dispose(id uint32) {
	m.gens[id]++
	m.free = append(m.free, id)
}
func (m *materialSystem) generation(id uint32) uint32 {
	if id >= uint32(len(m.gens)) {
		return 0
	}
	return m.gens[id]
}

func (m *materialSystem) newHandle(desc MaterialDesc) Material {
	id := m.create(desc)
	rc := int32(1)
	return Material{ref: Ref{id: id, gen: m.gens[id], refCount: &rc, owner: m}}
}

// Material is a ref-counted handle to a renderer-owned material.
type Material struct{ ref Ref }

func (m Material) Copy() Material { return Material{m.ref.Copy()} }
func (m Material) Release()       { m.ref.Release() }
func (m Material) Valid() bool    { return m.ref.Valid() }
func (m Material) id() uint32     { return m.ref.id }
