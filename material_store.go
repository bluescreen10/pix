package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
)

// materialRaster is a material instance's rasterization state (part of its pipeline
// identity). Stored per-instance so all copies of a material share it.
type materialRaster struct {
	cull  CullMode
	blend BlendMode
}

// materialStore is a generic, byte-based store for one material kind (one shader).
// The material system creates it automatically from a material's declared data size
// and shader — there is no per-type Go store to hand-write. Records live directly in
// a host-coherent, GPU-mapped BDA buffer (correctly aligned, no upload copy): a
// material's typed accessors write straight through record(id). Each instance also
// carries ref-counted texture slots and rasterization state.
type materialStore struct {
	backend  gpu.Backend
	sh       Shader
	stride   uint32 // bytes per record (the material's data size)
	count    uint32 // live + freed records
	cap      uint32
	buf      gpu.Buffer // mapped; records stored here directly
	raster   []materialRaster
	texSlots int
	texRefs  []Texture // len == count*texSlots; instance i owns [i*n:(i+1)*n]
	gens     []uint32
	free     []uint32
	label    string
}

func newMaterialStore(b gpu.Backend, sh Shader, stride uint32, texSlots int, label string) *materialStore {
	s := &materialStore{backend: b, sh: sh, stride: stride, texSlots: texSlots, label: label, cap: 1}
	s.buf = b.Alloc(uint64(stride), gpu.MemoryHost, label)
	return s
}

// alloc reserves a record slot (zeroed) and returns its id. Grows the mapped buffer
// (copying existing records) when full — its device address then changes, which the
// renderer re-reads each draw.
func (s *materialStore) alloc() uint32 {
	if n := len(s.free); n > 0 {
		id := s.free[n-1]
		s.free = s.free[:n-1]
		s.zero(id)
		s.raster[id] = materialRaster{}
		return id
	}
	if s.count == s.cap {
		s.grow()
	}
	id := s.count
	s.count++
	s.raster = append(s.raster, materialRaster{})
	s.gens = append(s.gens, 1)
	s.texRefs = append(s.texRefs, make([]Texture, s.texSlots)...)
	s.zero(id)
	return id
}

func (s *materialStore) grow() {
	s.cap *= 2
	nb := s.backend.Alloc(uint64(s.cap)*uint64(s.stride), gpu.MemoryHost, s.label)
	copy(unsafe.Slice((*byte)(nb.Ptr), uint64(s.count)*uint64(s.stride)),
		unsafe.Slice((*byte)(s.buf.Ptr), uint64(s.count)*uint64(s.stride)))
	s.backend.Free(s.buf)
	s.buf = nb
}

func (s *materialStore) zero(id uint32) {
	b := unsafe.Slice((*byte)(s.record(id)), s.stride)
	for i := range b {
		b[i] = 0
	}
}

// record returns a pointer to instance id's record bytes in the mapped buffer. Used
// transiently (compute, read/write, discard) — never held across an alloc/grow.
func (s *materialStore) record(id uint32) unsafe.Pointer {
	return unsafe.Pointer(uintptr(s.buf.Ptr) + uintptr(id)*uintptr(s.stride))
}

// dataOf returns instance id's record as a byte slice (Material.Data()).
func (s *materialStore) dataOf(id uint32) []byte {
	return unsafe.Slice((*byte)(s.record(id)), s.stride)
}

func (s *materialStore) bufAddr() uint64 { return s.buf.Addr }
func (s *materialStore) shader() Shader   { return s.sh }

func (s *materialStore) rasterOf(id uint32) materialRaster       { return s.raster[id] }
func (s *materialStore) setRasterOf(id uint32, r materialRaster) { s.raster[id] = r }

func (s *materialStore) setTexture(id uint32, slot int, t Texture) {
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
}

func (s *materialStore) texture(id uint32, slot int) Texture {
	return s.texRefs[int(id)*s.texSlots+slot]
}

// --- Disposer ---

func (s *materialStore) generation(id uint32) uint32 {
	if id >= uint32(len(s.gens)) {
		return 0
	}
	return s.gens[id]
}

func (s *materialStore) dispose(id uint32) {
	base := int(id) * s.texSlots
	for i := 0; i < s.texSlots; i++ {
		if t := s.texRefs[base+i]; t.Valid() {
			t.Release()
			s.texRefs[base+i] = Texture{}
		}
	}
	s.zero(id)
	s.gens[id]++
	s.free = append(s.free, id)
}

func (s *materialStore) destroy() {
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
