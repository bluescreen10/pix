package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
)

// materialRaster is a material instance's rasterization state (part of its pipeline
// identity). Stored per-instance so all copies of a material share it (mutating one
// material updates every mesh using it).
type materialRaster struct {
	cull  CullMode
	blend BlendMode
}

// materialStore is the non-generic view the materialSystem + renderer use to sync/
// destroy any per-type store, read its shaders and a record's raster state, and get
// its buffer address at draw time. It does NOT know its pipeline — the renderer
// resolves that from each material's (shaders, raster).
type materialStore interface {
	sync()
	destroy()
	bufAddr() uint64
	shader() Shader
	rasterOf(id uint32) materialRaster
	setRasterOf(id uint32, r materialRaster)
}

// materialSlab is a generic per-type material store: a growable BDA buffer of the
// type's POD record T, a generation-stamped free list, per-instance texture refs and
// raster state, and the type's shader. Instances are handles into it, and the slab is
// their ref-count Disposer.
type materialSlab[T any] struct {
	backend  gpu.Backend
	sh       Shader
	records  []T // POD; uploaded verbatim
	gens     []uint32
	free     []uint32
	raster   []materialRaster
	texSlots int
	texRefs  []Texture // len == len(records)*texSlots; instance i owns [i*n:(i+1)*n]
	buf      gpu.Buffer
	cap      uint32
	dirty    bool
	label    string
}

func newMaterialSlab[T any](b gpu.Backend, sh Shader, texSlots int, label string) *materialSlab[T] {
	var z T
	s := &materialSlab[T]{backend: b, sh: sh, cap: 1, texSlots: texSlots, label: label}
	s.buf = b.Alloc(uint64(unsafe.Sizeof(z)), gpu.MemoryHost, label)
	return s
}

// alloc records a new instance (opaque by default) and returns its id.
func (s *materialSlab[T]) alloc(rec T) uint32 {
	s.dirty = true
	if n := len(s.free); n > 0 {
		id := s.free[n-1]
		s.free = s.free[:n-1]
		s.records[id] = rec
		s.raster[id] = materialRaster{}
		return id
	}
	id := uint32(len(s.records))
	s.records = append(s.records, rec)
	s.gens = append(s.gens, 1)
	s.raster = append(s.raster, materialRaster{})
	s.texRefs = append(s.texRefs, make([]Texture, s.texSlots)...)
	return id
}

func (s *materialSlab[T]) get(id uint32) *T { return &s.records[id] }
func (s *materialSlab[T]) touch()           { s.dirty = true }

func (s *materialSlab[T]) setTexture(id uint32, slot int, t Texture) {
	i := int(id)*s.texSlots + slot
	old := s.texRefs[i]
	if t.Valid() {
		s.texRefs[i] = t.Copy()
	} else {
		s.texRefs[i] = Texture{}
	}
	if old.Valid() {
		old.Release()
	}
	s.dirty = true
}

func (s *materialSlab[T]) texture(id uint32, slot int) Texture {
	return s.texRefs[int(id)*s.texSlots+slot]
}

// --- Disposer + materialStore ---

func (s *materialSlab[T]) generation(id uint32) uint32 {
	if id >= uint32(len(s.gens)) {
		return 0
	}
	return s.gens[id]
}

func (s *materialSlab[T]) dispose(id uint32) {
	base := int(id) * s.texSlots
	for i := 0; i < s.texSlots; i++ {
		if t := s.texRefs[base+i]; t.Valid() {
			t.Release()
			s.texRefs[base+i] = Texture{}
		}
	}
	var z T
	s.records[id] = z
	s.gens[id]++
	s.free = append(s.free, id)
	s.dirty = true
}

func (s *materialSlab[T]) bufAddr() uint64                         { return s.buf.Addr }
func (s *materialSlab[T]) shader() Shader                          { return s.sh }
func (s *materialSlab[T]) rasterOf(id uint32) materialRaster       { return s.raster[id] }
func (s *materialSlab[T]) setRasterOf(id uint32, r materialRaster) { s.raster[id] = r }

func (s *materialSlab[T]) sync() {
	if !s.dirty {
		return
	}
	var z T
	stride := uint32(unsafe.Sizeof(z))
	need := uint32(len(s.records))
	if need > s.cap {
		for s.cap < need {
			s.cap *= 2
		}
		s.backend.Free(s.buf)
		s.buf = s.backend.Alloc(uint64(s.cap)*uint64(stride), gpu.MemoryHost, s.label)
	}
	if need > 0 {
		copy(unsafe.Slice((*T)(s.buf.Ptr), need), s.records)
	}
	s.dirty = false
}

func (s *materialSlab[T]) destroy() {
	for _, t := range s.texRefs {
		if t.Valid() {
			t.Release()
		}
	}
	if s.buf.Valid() {
		s.backend.Free(s.buf)
		s.buf = gpu.Buffer{}
	}
}
