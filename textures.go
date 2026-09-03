package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/internal/mem"
)

// defaultAnisotropy is the sampler anisotropy the built-in sampler requests. 8 is
// the usual quality/cost knee; the backend clamps it to maxSamplerAnisotropy.
// TODO: make this a renderer setting. It can't simply be a setter — materials hold
// sampler heap indices, so changing it later means re-registering samplers.
const defaultAnisotropy = 8

// textureEntry is one uploaded texture in the system. Generation is owned by the
// slab, not stored here.
type textureEntry struct {
	tex   gpu.Texture
	index uint32 // bindless sampled-image heap index
}

// textureSystem uploads CPU images into the backend's bindless heap and owns them
// (renderer-owned resource). It also owns samplers, including a default linear/
// repeat one. Uploads are immediate (a one-shot submit) since they happen at load.
type textureSystem struct {
	backend gpu.Backend

	samplers       []gpu.Sampler
	defaultSampler uint32

	entries mem.Slab[textureEntry]
}

func newTextureSystem(backend gpu.Backend) *textureSystem {
	t := &textureSystem{backend: backend, entries: mem.NewSlab[textureEntry]()}
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
func (t *textureSystem) DefaultSampler() uint32 { return t.defaultSampler }

// CreateSampler creates (and retains) a sampler, returning its heap index.
func (t *textureSystem) CreateSampler(d gpu.SamplerDescriptor) uint32 {
	s := t.backend.CreateSampler(d)
	t.samplers = append(t.samplers, s)
	return s.Index
}

// TextureFormat is what a texture is *for*, which is what decides both its GPU
// format and how its mips must be filtered. It is not a raw format enum: callers
// know they have a normal map, not that they want two unorm channels.
//
// Source pixels are always handed in as RGBA8; upload repacks to the narrower
// formats, so callers never have to pre-swizzle.
type TextureFormat uint8

const (
	// TextureSRGB is 8-bit sRGB color (base color, emissive) — the GPU decodes to
	// linear on sample, and mips are filtered in linear space.
	TextureSRGB TextureFormat = iota
	// TextureLinear is 8-bit linear RGBA data sampled as-is (packed
	// metallic-roughness-occlusion, lookup tables).
	TextureLinear
	// TextureNormal is a tangent-space normal map, stored as two channels: Z is a
	// unit vector's implied third component, so storing it wastes a quarter of the
	// texture. Shaders reconstruct it (see scene_pbr.frag.glsl), and mips are
	// renormalized rather than plainly averaged.
	TextureNormal
	// TextureGrayscale is a single-channel 8-bit map (occlusion, a standalone
	// roughness mask) — the red channel of the source is kept.
	TextureGrayscale
)

func (f TextureFormat) gpuFormat() gpu.Format {
	switch f {
	case TextureLinear:
		return gpu.FormatRGBA8Unorm
	case TextureNormal:
		return gpu.FormatRG8Unorm
	case TextureGrayscale:
		return gpu.FormatR8Unorm
	default:
		return gpu.FormatRGBA8Srgb
	}
}

// channels is how many bytes per texel this format stores on the GPU.
func (f TextureFormat) channels() int {
	switch f {
	case TextureNormal:
		return 2
	case TextureGrayscale:
		return 1
	default:
		return 4
	}
}

// repack narrows RGBA8 source pixels to this format's channel count, keeping the
// leading channels (RG for a normal map, R for grayscale). RGBA formats pass the
// source through untouched.
func (f TextureFormat) repack(rgba []byte, w, h int) []byte {
	c := f.channels()
	if c == 4 {
		return rgba
	}
	out := make([]byte, w*h*c)
	for i := range w * h {
		copy(out[i*c:(i+1)*c], rgba[i*4:i*4+c])
	}
	return out
}

// upload creates a mipmapped heap texture from RGBA8 pixels (w*h*4, row-major),
// repacked and filtered according to format, and returns a ref-counted handle.
// Submitted + waited immediately.
func (t *textureSystem) upload(rgba []byte, w, h int, format TextureFormat) Texture {
	channels := format.channels()
	base := format.repack(rgba, w, h)
	levels, sizes := mipChain(base, w, h, channels, format == TextureSRGB, format == TextureNormal)

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

	id, gen := t.entries.Alloc(textureEntry{tex: tex, index: tex.Index})
	rc := int32(1)
	return Texture{ref: Ref{id: id, gen: gen, refCount: &rc, owner: t}, index: tex.Index}
}

// createDepthTarget allocates a w×h depth texture usable as both a depth attachment
// (rendered into) and a bindless sampled image (sampled in the lit shaders) — i.e. a
// shadow map. Index is its heap slot.
func (t *textureSystem) createDepthTarget(w, h uint32) Texture {
	tex := t.backend.CreateTexture(gpu.TextureDescriptor{
		Kind: gpu.Texture2D, Width: w, Height: h,
		Format: gpu.FormatDepth32F, Usage: gpu.TextureDepth | gpu.TextureSampled,
		Label: "shadow-map",
	})
	id, gen := t.entries.Alloc(textureEntry{tex: tex, index: tex.Index})
	rc := int32(1)
	return Texture{ref: Ref{id: id, gen: gen, refCount: &rc, owner: t}, index: tex.Index}
}

// gpu resolves a handle to its backing backend texture (e.g. to bind a shadow map
// as a depth render attachment, or to transition it for sampling).
func (t *textureSystem) gpu(tex Texture) gpu.Texture {
	return t.entries.Get(tex.ref.id).tex
}

// Destroy releases all uploaded textures and samplers.
func (t *textureSystem) Destroy() {
	for e := range t.entries.Items() {
		if e.tex.Valid() {
			t.backend.DestroyTexture(e.tex)
		}
	}
	for _, s := range t.samplers {
		t.backend.DestroySampler(s)
	}
	t.entries, t.samplers = mem.NewSlab[textureEntry](), nil
}

// Disposer.
func (t *textureSystem) dispose(id uint32) {
	if e := t.entries.Get(id); e.tex.Valid() {
		t.backend.DestroyTexture(e.tex)
		e.tex = gpu.Texture{}
	}
	t.entries.Free(id) // bumps the slot's generation
}
func (t *textureSystem) generation(id uint32) uint32 {
	return t.entries.Generation(id)
}

// Texture is a ref-counted handle to a renderer-owned texture. Index is its bindless
// heap slot (store it in a MaterialDesc.ColorMap).
type Texture struct {
	ref   Ref
	index uint32
}

func (t Texture) Copy() Texture {
	return Texture{ref: t.ref.Copy(), index: t.index}
}

func (t Texture) Release() {
	t.ref.Release()
}

func (t Texture) Valid() bool {
	return t.ref.Valid()
}

// Index returns the bindless heap index used by materials.
func (t Texture) Index() uint32 {
	return t.index
}
