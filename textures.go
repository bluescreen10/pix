package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
)

// texEntry is one uploaded texture in the system.
type texEntry struct {
	tex   gpu.Texture
	index uint32 // bindless sampled-image heap index
	gen   uint32
}

// textureSystem uploads CPU images into the backend's bindless heap and owns them
// (renderer-owned resource). It also owns samplers, including a default linear/
// repeat one. Uploads are immediate (a one-shot submit) since they happen at load.
type textureSystem struct {
	backend    gpu.Backend
	defSampler uint32
	entries    []texEntry
	free       []uint32
	samplers   []gpu.Sampler
}

func newTextureSystem(b gpu.Backend) *textureSystem {
	t := &textureSystem{backend: b}
	s := b.CreateSampler(gpu.SamplerDescriptor{
		MinLinear: true, MagLinear: true, MipLinear: true,
		AddressU: gpu.AddressRepeat, AddressV: gpu.AddressRepeat, AddressW: gpu.AddressRepeat,
		Label: "default",
	})
	t.samplers = append(t.samplers, s)
	t.defSampler = s.Index
	return t
}

// DefaultSampler returns the heap index of the linear/repeat sampler.
func (t *textureSystem) DefaultSampler() uint32 { return t.defSampler }

// CreateSampler creates (and retains) a sampler, returning its heap index.
func (t *textureSystem) CreateSampler(d gpu.SamplerDescriptor) uint32 {
	s := t.backend.CreateSampler(d)
	t.samplers = append(t.samplers, s)
	return s.Index
}

// upload creates an RGBA8 heap texture from pixels (w*h*4, row-major) and returns a
// ref-counted handle. The upload is submitted and waited on immediately.
func (t *textureSystem) upload(pixels []byte, w, h int) Texture {
	tex := t.backend.CreateTexture(gpu.TextureDescriptor{
		Kind: gpu.Texture2D, Width: uint32(w), Height: uint32(h),
		Format: gpu.FormatRGBA8Unorm, Usage: gpu.TextureSampled | gpu.TextureTransfer,
	})
	staging := t.backend.Alloc(uint64(len(pixels)), gpu.MemoryHost, "tex-staging")
	copy(unsafe.Slice((*byte)(staging.Ptr), len(pixels)), pixels)
	cl := t.backend.Begin()
	cl.CopyBufferToTexture(tex, 0, 0, staging, 0)
	cl.PrepareSampled(tex, gpu.StageFragment)
	f := t.backend.Submit(cl)
	t.backend.Wait(f)
	t.backend.Free(staging)

	var id uint32
	if n := len(t.free); n > 0 {
		id = t.free[n-1]
		t.free = t.free[:n-1]
		t.entries[id] = texEntry{tex: tex, index: tex.Index, gen: t.entries[id].gen}
	} else {
		id = uint32(len(t.entries))
		t.entries = append(t.entries, texEntry{tex: tex, index: tex.Index, gen: 1})
	}
	rc := int32(1)
	return Texture{ref: Ref{id: id, gen: t.entries[id].gen, refCount: &rc, owner: t}, index: tex.Index}
}

// Destroy releases all uploaded textures and samplers.
func (t *textureSystem) Destroy() {
	for _, e := range t.entries {
		if e.tex.Valid() {
			t.backend.DestroyTexture(e.tex)
		}
	}
	for _, s := range t.samplers {
		t.backend.DestroySampler(s)
	}
	t.entries, t.samplers = nil, nil
}

// Disposer.
func (t *textureSystem) dispose(id uint32) {
	e := &t.entries[id]
	if e.tex.Valid() {
		t.backend.DestroyTexture(e.tex)
		e.tex = gpu.Texture{}
	}
	e.gen++
	t.free = append(t.free, id)
}
func (t *textureSystem) generation(id uint32) uint32 {
	if id >= uint32(len(t.entries)) {
		return 0
	}
	return t.entries[id].gen
}

// Texture is a ref-counted handle to a renderer-owned texture. Index is its bindless
// heap slot (store it in a MaterialDesc.ColorMap).
type Texture struct {
	ref   Ref
	index uint32
}

func (t Texture) Copy() Texture { return Texture{ref: t.ref.Copy(), index: t.index} }
func (t Texture) Release()      { t.ref.Release() }
func (t Texture) Valid() bool   { return t.ref.Valid() }

// Index returns the bindless heap index used by materials.
func (t Texture) Index() uint32 { return t.index }
