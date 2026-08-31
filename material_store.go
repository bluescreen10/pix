package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
)

// materialStore is a generic, byte-based store for one material kind (one shader).
// The material system creates it automatically from a material's declared data size
// and shader — there is no per-type Go store to hand-write. Records live directly in
// a host-coherent, GPU-mapped BDA buffer (correctly aligned, no upload copy): a
// material's typed accessors write straight through record(id). Each instance also
// carries ref-counted texture slots and per-instance cull/blend rasterization state.
type materialStore struct {
	backend      gpu.Backend
	sh           Shader
	fragHash     uint32 // hash of sh.Fragment (store dedup key)
	pipeHash     uint32 // hash of sh.Vertex+sh.Fragment (draw-pipeline identity)
	stride       uint32 // bytes per record (the material's data size)
	count        uint32 // live + freed records
	cap          uint32
	buf          gpu.Buffer  // mapped; records stored here directly
	cull         []CullMode  // per-instance, indexed by id
	blend        []BlendMode // per-instance, indexed by id
	textureSlots int
	textures     []Texture // len == count*textureSlots; instance i owns [i*n:(i+1)*n]
	gens         []uint32
	free         []uint32
	label        string
}

func newMaterialStore(b gpu.Backend, sh Shader, stride uint32, textureSlots int, label string) *materialStore {
	s := &materialStore{
		backend: b, sh: sh, stride: stride, textureSlots: textureSlots, label: label, cap: 1,
		fragHash: hashSPIRV(sh.Fragment), pipeHash: shaderHash(sh),
	}
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
		s.cull[id], s.blend[id] = CullNone, BlendOpaque
		return id
	}
	if s.count == s.cap {
		s.grow()
	}
	id := s.count
	s.count++
	s.cull = append(s.cull, CullNone)
	s.blend = append(s.blend, BlendOpaque)
	s.gens = append(s.gens, 1)
	s.textures = append(s.textures, make([]Texture, s.textureSlots)...)
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
func (s *materialStore) shader() Shader  { return s.sh }

func (s *materialStore) cullOf(id uint32) CullMode {
	return s.cull[id]
}

func (s *materialStore) setCullOf(id uint32, c CullMode) {
	s.cull[id] = c
}

func (s *materialStore) blendOf(id uint32) BlendMode {
	return s.blend[id]
}

func (s *materialStore) setBlendOf(id uint32, b BlendMode) {
	s.blend[id] = b
}

func (s *materialStore) setTexture(id uint32, slot int, t Texture) {
	i := int(id)*s.textureSlots + slot
	old := s.textures[i]
	if t.Valid() {
		s.textures[i] = t.Copy()
	} else {
		s.textures[i] = Texture{}
	}
	if old.Valid() {
		old.Release()
	}
}

func (s *materialStore) texture(id uint32, slot int) Texture {
	return s.textures[int(id)*s.textureSlots+slot]
}

// --- Disposer ---

func (s *materialStore) generation(id uint32) uint32 {
	if id >= uint32(len(s.gens)) {
		return 0
	}
	return s.gens[id]
}

func (s *materialStore) dispose(id uint32) {
	base := int(id) * s.textureSlots
	for i := 0; i < s.textureSlots; i++ {
		if t := s.textures[base+i]; t.Valid() {
			t.Release()
			s.textures[base+i] = Texture{}
		}
	}
	s.zero(id)
	s.gens[id]++
	s.free = append(s.free, id)
}

func (s *materialStore) destroy() {
	for _, t := range s.textures {
		if t.Valid() {
			t.Release()
		}
	}
	if s.buf.Valid() {
		s.backend.Free(s.buf)
		s.buf = gpu.Buffer{}
	}
}
