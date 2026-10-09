package materials

import (
	"fmt"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/mem"
	"github.com/bluescreen10/pix/ref"
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
	// isMasked is whether the instance cuts its surface out by alpha (see Masked), as
	// last marked dirty.
	isMasked bool
	// isTransmissive is whether the instance lets light through (see Transmissive), as
	// last marked dirty.
	isTransmissive bool
	// isUVTransformed is whether the instance moves the mesh's texture coordinates (see
	// isUVTransformed), as last marked dirty.
	isUVTransformed bool
	// features is the optional parts of its shader the instance uses (see Featured), as
	// last marked dirty.
	features Features
}

// Pool holds every material of one kind (one shader). The material system
// creates it automatically — there is no per-type Go store to hand-write.
//
// Records live in device-local memory, which the CPU cannot write. The store keeps no
// copy of them: the materials are the source of truth, and Sync re-serializes just the
// ones marked dirty. So every setter must call markDirty, or its change is never
// uploaded and the GPU renders the previous value indefinitely.
type Pool struct {
	backend gpu.Backend
	sh      Shader
	// index is this pool's position in its Store's pool list, assigned at creation.
	// Pools are never removed from a store (Destroy tears down all of them at once),
	// so it is stable for the store's lifetime and dense — which is what lets the
	// renderer key per-pool state, such as its pipeline table, by array index rather
	// than by lookup, and what lets a material be named by value as (pool, slot, gen).
	index uint32
	hash  uint64 // hash of the whole Shader — this pool's identity, and its dedup key

	entries mem.Slab[entry]

	stride uint32     // bytes per record, learned from the first material registered
	cap    uint32     // records the device buffer holds
	buf    gpu.Buffer // MemoryDevice; written only by Sync
	// masks holds, for each record slot, how its material cuts its surface out (see
	// maskEntry): what the depth-only passes discard by, whatever the material's type.
	masks gpu.Buffer

	// rasterRevision counts changes to any instance's cull or blend mode. Those two
	// are the whole of what a pipeline's identity adds to the pool's shader, so a
	// consumer that resolved pipelines from this pool can tell they are stale by
	// comparing one number — without keeping a copy of every material it looked at.
	rasterRevision uint64

	dirty    map[uint32]struct{} // ids changed since the last Sync
	allDirty bool                // every live record needs re-upload (set by grow)
	scratch  []byte              // serialization buffer, reused across Syncs
	parts    []scatterPart       // scatterCopy parts, reused across Syncs

	label string
}

func newPool(b gpu.Backend, sh Shader, label string, index uint32) *Pool {
	s := &Pool{
		backend: b, sh: sh, label: label, index: index, entries: mem.NewSlab[entry](),
		hash: hashShader(sh),
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
	s.MarkDirty(id)

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
	if s.buf.IsValid() {
		s.backend.Free(s.buf)
		s.backend.Free(s.masks)
	}
	s.cap = newCap
	s.buf = s.backend.Alloc(uint64(newCap)*uint64(s.stride), gpu.MemoryDevice, s.label)
	s.masks = s.backend.Alloc(uint64(newCap)*maskEntrySize, gpu.MemoryDevice, s.label+" masks")
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

// scatterPart is one changed record, or mask table entry: data lands at dstOffset in
// buffer, wherever the uploader's arena happens to stage it.
type scatterPart struct {
	buffer    gpu.Buffer
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
		for id := range s.entries.Entries() {
			s.dirty[id] = struct{}{}
		}
	}
	if len(s.dirty) == 0 {
		return
	}
	need := len(s.dirty) * (int(s.stride) + maskEntrySize)
	if cap(s.scratch) < need {
		s.scratch = make([]byte, need)
	}
	scratch := s.scratch[:0]
	parts := s.parts[:0]
	for id := range s.dirty {
		lo := len(scratch)
		var mask maskEntry
		if s.entries.IsAlive(id) {
			inst := s.entries.Value(id).inst
			scratch = append(scratch, inst.Bytes()...)
			mask = maskEntryOf(inst)
		} else {
			// A freed slot still gets one last write, of zeros: a draw issued in the
			// same frame the material was released must not read its old record.
			scratch = scratch[:lo+int(s.stride)]
			clear(scratch[lo:])
		}
		parts = append(parts, scatterPart{buffer: s.buf, dstOffset: id * s.stride, data: scratch[lo:]})
		// A slot no longer alive gets a zero entry, which no draw reads.
		lo = len(scratch)
		scratch = mask.appendBytes(scratch)
		parts = append(parts, scatterPart{buffer: s.masks, dstOffset: id * maskEntrySize, data: scratch[lo:]})
	}
	for _, p := range parts {
		u.Copy(p.buffer, p.dstOffset, p.data)
	}
	s.scratch, s.parts = scratch, parts[:0]
	clear(s.dirty)
}

// RecordsAddr is the device address of the pool's record buffer, resolved at draw
// time because it moves when the pool grows.
func (s *Pool) RecordsAddr() uint64 {
	return s.buf.Addr
}

// MasksAddr is the device address of the pool's mask table, an entry per record slot —
// Mask in shaders/src/alpha_mask.glsl — resolved at draw time because it moves when the
// pool grows.
func (s *Pool) MasksAddr() uint64 {
	return s.masks.Addr
}

// Hash is this pool's shader identity, wide enough to stand alone as its dedup key
// (see Store.Pool).
func (s *Pool) Hash() uint64 {
	return s.hash
}

// Shader is the SPIR-V set the pool is keyed by, and the source of truth for how its
// materials are drawn.
func (s *Pool) Shader() Shader {
	return s.sh
}

// Index is this pool's dense, stable position in its owning Store — see the field.
func (s *Pool) Index() uint32 {
	return s.index
}

// IDOf names one of this pool's instances by value. Material implementations return
// it from ID(), so none of them has to know how an identity is put together.
func (s *Pool) IDOf(r ref.Ref) ID {
	return ID{PoolID: s.index, Slot: r.ID(), Gen: r.Gen()}
}

// Live reports whether slot id still holds the same instance generation gen. A
// reference carried as plain data cannot ref-count what it names, so whoever resolves
// one checks it here before trusting the slot.
func (s *Pool) Live(id, gen uint32) bool {
	return s.validate(id, gen)
}

// MarkDirty flags an instance's record for re-upload on the next Sync. A material
// implementation must call it from every setter, or the GPU keeps rendering the
// previous value indefinitely.
//
// It also notes whether the instance is masked, whether it is transmissive, and which
// optional parts of its shader it uses now (see IsMaskedAt, IsTransmissiveAt and
// FeaturesAt): those decide how the passes draw it, so a change of any counts as a
// change of rasterization state (see RasterRevision), noticed before the next frame is
// laid out rather than once its records are uploaded.
func (s *Pool) MarkDirty(id uint32) {
	e := s.entries.Value(id)
	mask := maskEntryOf(e.inst)
	isMasked, isTransmissive := mask.alpha.IsMasked(), mask.transmission.IsTransmissive()
	isUVTransformed := isUVTransformed(e.inst)
	var features Features
	if featured, ok := e.inst.(Featured); ok {
		features = featured.Features()
	}
	if isMasked != e.isMasked || isTransmissive != e.isTransmissive || isUVTransformed != e.isUVTransformed || features != e.features {
		e.isMasked, e.isTransmissive, e.isUVTransformed, e.features = isMasked, isTransmissive, isUVTransformed, features
		s.rasterRevision++
	}
	s.markDirty(id)
}

// maskEntry is what the depth-only passes, which cannot read a material's record, know of
// how it cuts its surface out: by alpha, by the light it lets through, and the
// transforms of the coordinates both masks' maps are read at. A record's layout is its
// type's own; the mask table's is one for every type, so that one depth pass reads any
// material's.
type maskEntry struct {
	alpha        AlphaMask
	transmission TransmissionMask
	uvTransforms [2]UVTransform
}

// maskEntryOf returns inst's entry of the mask table: what it reports through Masked,
// Transmissive and UVTransformer, and for what it does not implement, a mask that does
// nothing, or NoUVTransform.
func maskEntryOf(inst Instance) maskEntry {
	mask := maskEntry{uvTransforms: [2]UVTransform{NoUVTransform, NoUVTransform}}
	if masked, ok := inst.(Masked); ok {
		mask.alpha = masked.AlphaMask()
	}
	if transmissive, ok := inst.(Transmissive); ok {
		mask.transmission = transmissive.TransmissionMask()
	}
	if transformer, ok := inst.(UVTransformer); ok {
		mask.uvTransforms = [2]UVTransform{transformer.UVTransform(UV1), transformer.UVTransform(UV2)}
	}
	return mask
}

// appendBytes appends the entry as the shaders read it, maskEntrySize bytes, to b.
func (m maskEntry) appendBytes(b []byte) []byte {
	b = append(b, m.alpha.ToBytes()...)
	b = append(b, m.transmission.ToBytes()...)
	for _, t := range m.uvTransforms {
		b = append(b, t.ToBytes()...)
	}
	var padding [12]byte
	return append(b, padding[:]...)
}

// IsMaskedAt reports whether the instance in slot id cuts its surface out by alpha (see
// Masked).
func (s *Pool) IsMaskedAt(id uint32) bool {
	return s.entries.Value(id).isMasked
}

// IsUVTransformedAt reports whether the instance in slot id moves either set of the
// mesh's texture coordinates: whether it implements UVTransformer, with a transform other
// than NoUVTransform. Like IsMaskedAt, a change of it counts as a change of
// rasterization state (see MarkDirty).
func (s *Pool) IsUVTransformedAt(id uint32) bool {
	return s.entries.Value(id).isUVTransformed
}

// FeaturesAt reports which optional parts of its shader the instance in slot id uses
// (see Featured). Like IsMaskedAt, a change of it counts as a change of rasterization
// state (see MarkDirty).
func (s *Pool) FeaturesAt(id uint32) Features {
	return s.entries.Value(id).features
}

// IsTransmissiveAt reports whether the instance in slot id lets light through (see
// Transmissive): whether drawing it reads the scene behind it. Like IsMaskedAt, a
// change of it counts as a change of rasterization state (see MarkDirty).
func (s *Pool) IsTransmissiveAt(id uint32) bool {
	return s.entries.Value(id).isTransmissive
}

// Cull/Blend rasterization state, stored per instance beside the record so the
// renderer can read it without deserializing anything.

func (s *Pool) CullAt(id uint32) CullMode {
	return s.entries.Value(id).cull
}

func (s *Pool) SetCullAt(id uint32, c CullMode) {
	e := s.entries.Value(id)
	if e.cull == c {
		return
	}
	e.cull = c
	s.rasterRevision++
}

func (s *Pool) BlendAt(id uint32) BlendMode {
	return s.entries.Value(id).blend
}

func (s *Pool) SetBlendAt(id uint32, b BlendMode) {
	e := s.entries.Value(id)
	if e.blend == b {
		return
	}
	e.blend = b
	s.rasterRevision++
}

// RasterRevision changes whenever any instance's cull or blend mode, or whether it is
// masked, does, and so whenever a pipeline resolved from this pool might have become
// the wrong one.
//
// It is deliberately per pool rather than per instance: every material in a pool shares
// its shader, so cull, blend and masking are all that can move a material to a
// different pipeline, and one counter covers every instance at once. A consumer holding pipelines
// for a whole scene can check a handful of these instead of remembering which materials
// it resolved and re-resolving each of them every frame.
func (s *Pool) RasterRevision() uint64 {
	return s.rasterRevision
}

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
	s.entries.Value(id).inst.Dispose()
	s.entries.Free(id)
	s.markDirty(id)
}

func (s *Pool) destroy() {
	if s.buf.IsValid() {
		s.backend.Free(s.buf)
		s.backend.Free(s.masks)
		s.buf = gpu.Buffer{}
		s.masks = gpu.Buffer{}
	}
}
