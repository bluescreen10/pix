// Package textures owns texture resources for pix's bindless renderer: it uploads
// CPU images into the backend's sampled-image heap and hands back ref-counted
// handles carrying their heap index. There are no bind groups — a material stores a
// Texture's Index in its record and the shader indexes the heap with it.
package textures

import (
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/internal/mem"
	"github.com/bluescreen10/pix/internal/ref"
)

// defaultAnisotropy is the sampler anisotropy the built-in sampler requests. 8 is
// the usual quality/cost knee; the backend clamps it to maxSamplerAnisotropy.
// TODO: make this a renderer setting. It can't simply be a setter — materials hold
// sampler heap indices, so changing it later means re-registering samplers.
const defaultAnisotropy = 8

// entry is one uploaded texture in the store. Generation is owned by the slab, not
// stored here.
type entry struct {
	tex   gpu.Texture
	index uint32 // bindless sampled-image heap index
}

// Store uploads CPU images into the backend's bindless heap and owns them
// (renderer-owned resource). It also owns samplers, including a default linear/
// repeat one. Uploads are immediate (a one-shot submit) since they happen at load.
type Store struct {
	backend gpu.Backend

	samplers       []gpu.Sampler
	defaultSampler uint32

	entries mem.Slab[entry]
}

// NewStore creates the store and its default linear/repeat sampler.
func NewStore(backend gpu.Backend) *Store {
	t := &Store{backend: backend, entries: mem.NewSlab[entry]()}
	s := backend.CreateSampler(gpu.SamplerDescriptor{
		MinLinear: true, MagLinear: true, MipLinear: true,
		AddressU: gpu.AddressRepeat, AddressV: gpu.AddressRepeat, AddressW: gpu.AddressRepeat,
		// Trilinear alone still over-blurs surfaces seen at a grazing angle (any
		// ground plane), which is exactly what anisotropy fixes. The backend clamps
		// this to the device limit, so asking for more than the hardware has is safe.
		MaxAnisotropy: defaultAnisotropy,
		Label:         "default",
	})
	t.samplers = append(t.samplers, s)
	t.defaultSampler = s.Index
	return t
}

// DefaultSampler returns the heap index of the linear/repeat sampler.
func (t *Store) DefaultSampler() uint32 { return t.defaultSampler }

// CreateSampler creates (and retains) a sampler, returning its heap index.
func (t *Store) CreateSampler(d gpu.SamplerDescriptor) uint32 {
	s := t.backend.CreateSampler(d)
	t.samplers = append(t.samplers, s)
	return s.Index
}

// Create builds a mipmapped heap texture from RGBA8 pixels (w*h*4, row-major),
// repacked and filtered according to format, and returns a fresh single-ref handle.
// Submitted + waited immediately.
func (t *Store) Create(rgba []byte, w, h int, format Format) Texture {
	channels := format.channels()
	base := format.repack(rgba, w, h)
	levels, sizes := mipChain(base, w, h, channels, format == SRGB, format == Normal)

	tex := t.backend.CreateTexture(gpu.TextureDescriptor{
		Kind: gpu.Texture2D, Width: uint32(w), Height: uint32(h),
		Mips:   uint32(len(levels) + 1),
		Format: format.gpuFormat(), Usage: gpu.TextureSampled | gpu.TextureTransfer,
	})

	// Textures upload at load time and must be usable the moment this returns, so
	// they don't ride the frame's uploader (whose copies only execute when that
	// frame is submitted). One staging buffer holding the whole chain, one submit,
	// one wait — which also keeps the stricter buffer-to-image copy alignment out
	// of the frame arena's business.
	total := len(base)
	for _, l := range levels {
		total += len(l)
	}
	staging := t.backend.Alloc(uint64(total), gpu.MemoryHost, "texture-staging")
	dst := unsafe.Slice((*byte)(staging.Ptr), total)

	cmd := t.backend.Begin()
	var off uint64
	put := func(level uint32, data []byte) {
		copy(dst[off:], data)
		cmd.CopyBufferToTexture(tex, level, 0, staging, off)
		off += uint64(len(data))
	}
	put(0, base)
	for i, l := range levels {
		_ = sizes[i] // the backend derives each level's extent from the mip index
		put(uint32(i+1), l)
	}
	cmd.PrepareSampled(tex, gpu.StageFragment)
	t.backend.Wait(t.backend.Submit(cmd))
	t.backend.Free(staging)

	return t.handle(tex)
}

// CreateDepthTarget allocates a w×h depth texture usable as both a depth attachment
// (rendered into) and a bindless sampled image (sampled in the lit shaders) — i.e. a
// shadow map. Index is its heap slot.
func (t *Store) CreateDepthTarget(w, h uint32) Texture {
	tex := t.backend.CreateTexture(gpu.TextureDescriptor{
		Kind: gpu.Texture2D, Width: w, Height: h,
		Format: gpu.FormatDepth32F, Usage: gpu.TextureDepth | gpu.TextureSampled,
		Label: "shadow-map",
	})
	return t.handle(tex)
}

// handle records a backend texture in the slab and returns a fresh single-ref handle.
func (t *Store) handle(tex gpu.Texture) Texture {
	id, gen := t.entries.Alloc(entry{tex: tex, index: tex.Index})
	return Texture{ref: ref.New(id, gen, t.dispose, t.validate), index: tex.Index}
}

// GPU resolves a handle to its backing backend texture (e.g. to bind a shadow map
// as a depth render attachment, or to transition it for sampling).
func (t *Store) GPU(tex Texture) gpu.Texture {
	return t.entries.Get(tex.ref.ID()).tex
}

// Destroy releases all uploaded textures and samplers.
func (t *Store) Destroy() {
	for e := range t.entries.Items() {
		if e.tex.Valid() {
			t.backend.DestroyTexture(e.tex)
		}
	}
	for _, s := range t.samplers {
		t.backend.DestroySampler(s)
	}
	t.entries, t.samplers = mem.NewSlab[entry](), nil
}

// dispose/validate let a ref own a slot in this store.
func (t *Store) dispose(id uint32) {
	if e := t.entries.Get(id); e.tex.Valid() {
		t.backend.DestroyTexture(e.tex)
		e.tex = gpu.Texture{}
	}
	t.entries.Free(id) // bumps the slot's generation
}

func (t *Store) validate(id, gen uint32) bool {
	return t.entries.Generation(id) == gen
}
