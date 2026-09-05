package materials

import (
	"fmt"

	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/internal/mem"
	"github.com/bluescreen10/pix/internal/ref"
)

// Instance is one live material. The typed materials implement it: a material
// holds its own fields, and Bytes serializes them into the exact layout its shader
// reads. There is no separate record type and no host shadow of the GPU buffer — the
// material is the record, and Sync asks it for bytes only when it has changed.
type Instance interface {
	// Bytes returns the material's GPU record. It must be free of side effects (Sync
	// calls it) and the same length on every call for a given material type.
	Bytes() []byte
	// Dispose drops whatever the material holds (texture references). The Pool calls
	// it exactly once, when the last handle to the instance is released. It is
	// deliberately not named Release: that is the *handle* operation on Material, and
	// this is the one-shot teardown underneath it.
	Dispose()
}

// entry is one slot's bookkeeping: the material itself plus the rasterization
// state the renderer reads without touching the record.
type entry struct {
	inst  Instance
	cull  CullMode
	blend BlendMode
}

// Pool holds every material of one kind (one shader). The material system
// creates it automatically — there is no per-type Go store to hand-write.
//
// Records live in device-local memory, which the CPU cannot write. The store keeps no
// copy of them: the materials are the source of truth, and Sync re-serializes just the
// ones marked dirty. So every setter must call markDirty, or its change is never
// uploaded and the GPU renders the previous value indefinitely.
type Pool struct {
	backend     gpu.Backend
	sh          Shader
	forwardHash uint32 // hash of sh.Forward alone (pool dedup key)
	hash        uint32 // hash of the whole Shader — every instance's Material.Hash

	entries mem.Slab[entry]

	stride uint32     // bytes per record, learned from the first material registered
	cap    uint32     // records the device buffer holds
	buf    gpu.Buffer // MemoryDevice; written only by Sync

	dirty    map[uint32]struct{} // ids changed since the last Sync
	allDirty bool                // every live record needs re-upload (set by grow)
	scratch  []byte              // serialization buffer, reused across Syncs
	parts    []scatterPart       // scatterCopy parts, reused across Syncs

	label string
}

func newPool(b gpu.Backend, sh Shader, label string) *Pool {
	s := &Pool{
		backend: b, sh: sh, label: label, entries: mem.NewSlab[entry](),
		forwardHash: HashSPIRV(sh.Forward), hash: hashShader(sh),
	}
	// The buffer is allocated by the first register, which is what reveals the stride.
	return s
}

// Create takes ownership of a material, assigns it a slot, and returns the
// ref-counted handle it stores. The pool's stride comes from the first material
// registered; every later one must serialize to the same length, since a pool is
// keyed by shader and one shader reads one record layout.
func (s *Pool) Create(inst Instance) ref.Ref {
	n := uint32(len(inst.Bytes()))
	if s.stride == 0 {
		s.stride = n
	} else if n != s.stride {
		panic(fmt.Sprintf("pix: material record is %d bytes, store %q holds %d", n, s.label, s.stride))
	}

	if s.dirty == nil {
		s.dirty = make(map[uint32]struct{})
	}
	id, gen := s.entries.Alloc(entry{inst: inst, cull: CullNone, blend: BlendOpaque})
	s.ensureCap(id + 1)
	s.markDirty(id)

	return ref.New(id, gen, s.dispose, s.validate)
}

// ensureCap grows the device buffer to hold at least n records. The new buffer's
// contents are undefined and its device address changes, so every live record is
// re-uploaded on the next Sync and the renderer re-reads the address each draw.
func (s *Pool) ensureCap(n uint32) {
	if n <= s.cap {
		return
	}
	newCap := max(s.cap, 1)
	for newCap < n {
		newCap *= 2
	}
	if s.buf.Valid() {
		s.backend.Free(s.buf)
	}
	s.cap = newCap
	s.buf = s.backend.Alloc(uint64(newCap)*uint64(s.stride), gpu.MemoryDevice, s.label)
	s.markAllDirty()
}

func (s *Pool) markDirty(id uint32) {
	if s.allDirty {
		return
	}
	s.dirty[id] = struct{}{}
}

func (s *Pool) markAllDirty() {
	s.allDirty = true
	clear(s.dirty)
}

// scatterPart is one changed record: data lands at dstOffset in the store's record
// buffer, wherever the uploader's arena happens to stage it.
type scatterPart struct {
	dstOffset uint32
	data      []byte
}

// Sync re-serializes every material changed since the last call and uploads it through
// the uploader. Call once per frame before recording draws that read the record buffer;
// the caller flushes the uploader (submit + wait) after all subsystems have synced.
//
// At most a few dozen materials change in a frame, so this does not bother coalescing
// adjacent ids into contiguous device-side runs: the uploader stages every changed
// record into its shared arena regardless of how scattered their ids are, so per-record
// copies cost a bump-pointer advance each and there is nothing left to gain by sorting
// them first. That also means the dirty set does not need to be ordered — a plain map
// works.
func (s *Pool) Sync(u Uploader) {
	if s.allDirty {
		s.allDirty = false
		for id := range s.entries.All() {
			s.dirty[id] = struct{}{}
		}
	}
	if len(s.dirty) == 0 {
		return
	}
	need := len(s.dirty) * int(s.stride)
	if cap(s.scratch) < need {
		s.scratch = make([]byte, need)
	}
	scratch := s.scratch[:0]
	parts := s.parts[:0]
	for id := range s.dirty {
		lo := len(scratch)
		if s.entries.Alive(id) {
			scratch = append(scratch, s.entries.Get(id).inst.Bytes()...)
		} else {
			// A freed slot still gets one last write, of zeros: a draw issued in the
			// same frame the material was released must not read its old record.
			scratch = scratch[:lo+int(s.stride)]
			clear(scratch[lo:])
		}
		parts = append(parts, scatterPart{dstOffset: id * s.stride, data: scratch[lo:]})
	}
	for _, p := range parts {
		u.Copy(s.buf, p.dstOffset, p.data)
	}
	s.scratch, s.parts = scratch, parts[:0]
	clear(s.dirty)
}

// RecordsAddr is the device address of the pool's record buffer, resolved at draw
// time because it moves when the pool grows. Hash is the pipeline identity every
// instance of this pool reports. Shader is the SPIR-V set the pool is keyed by.
func (s *Pool) RecordsAddr() uint64 { return s.buf.Addr }
func (s *Pool) Hash() uint32        { return s.hash }
func (s *Pool) Shader() Shader      { return s.sh }

// MarkDirty flags an instance's record for re-upload on the next Sync. A material
// implementation must call it from every setter, or the GPU keeps rendering the
// previous value indefinitely.
func (s *Pool) MarkDirty(id uint32) { s.markDirty(id) }

// Cull/Blend rasterization state, stored per instance beside the record so the
// renderer can read it without deserializing anything.
func (s *Pool) Cull(id uint32) CullMode         { return s.entries.Get(id).cull }
func (s *Pool) SetCull(id uint32, c CullMode)   { s.entries.Get(id).cull = c }
func (s *Pool) Blend(id uint32) BlendMode       { return s.entries.Get(id).blend }
func (s *Pool) SetBlend(id uint32, b BlendMode) { s.entries.Get(id).blend = b }

// dispose/validate let a ref own a slot in this pool.

func (s *Pool) validate(id, gen uint32) bool {
	if id >= uint32(s.entries.Len()) {
		return false
	}
	return s.entries.Generation(id) == gen
}

// dispose frees the slot behind the last released handle: the material drops the
// textures it held, and the slot uploads zeros on the next Sync.
func (s *Pool) dispose(id uint32) {
	s.entries.Get(id).inst.Dispose()
	s.entries.Free(id)
	s.markDirty(id)
}

func (s *Pool) destroy() {
	if s.buf.Valid() {
		s.backend.Free(s.buf)
		s.buf = gpu.Buffer{}
	}
}
