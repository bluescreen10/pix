// Package textures owns texture resources for pix's bindless renderer: it uploads
// CPU images into the backend's sampled-image heap and hands back ref-counted
// handles carrying their heap index. There are no bind groups — a material stores a
// Texture's Index in its record and the shader indexes the heap with it.
package textures

import (
	"image"
	"unsafe"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/mem"
	"github.com/bluescreen10/pix/ref"
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
	// mipViews is, for a writable texture of more than one mip, a storage view of each
	// mip, which the store frees with the texture (see WritableTexture.Mips).
	mipViews []gpu.Texture
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
func (t *Store) DefaultSampler() uint32 {
	return t.defaultSampler
}

// CreateSampler creates (and retains) a sampler, returning its heap index.
func (t *Store) CreateSampler(d gpu.SamplerDescriptor) uint32 {
	s := t.backend.CreateSampler(d)
	t.samplers = append(t.samplers, s)
	return s.Index
}

// Create makes a mipmapped heap texture of format from img (see Prepare for how each
// format reads it) and returns a fresh single-ref handle. Submitted + waited
// immediately. It is Prepare and Upload; a loader with many textures to make prepares
// them in parallel and uploads them as they are ready.
func (t *Store) Create(img image.Image, format Format) Texture {
	return t.Upload(Prepare(img, format, FullMipChain))
}

// Upload makes a heap texture of img and returns a fresh single-ref handle, its levels
// uploaded and ready to sample when it returns.
func (t *Store) Upload(img Image) Texture {
	tex := t.backend.CreateTexture(gpu.TextureDescriptor{
		Kind: gpu.Texture2D, Width: uint32(img.width), Height: uint32(img.height),
		Mips:   uint32(len(img.levels)),
		Format: img.format.gpuFormat(), Usage: gpu.TextureSampled | gpu.TextureTransfer,
	})

	// Textures upload at load time and must be usable the moment this returns, so
	// they don't ride the frame's uploader (whose copies only execute when that
	// frame is submitted). One staging buffer holding the whole chain, one submit,
	// one wait — which also keeps the stricter buffer-to-image copy alignment out
	// of the frame arena's business.
	total := 0
	for _, l := range img.levels {
		total += len(l)
	}
	staging := t.backend.Alloc(uint64(total), gpu.MemoryHost, "texture-staging")
	dst := unsafe.Slice((*byte)(staging.Ptr), total)

	cmd := t.backend.Begin()
	var off uint64
	for level, data := range img.levels {
		copy(dst[off:], data)
		cmd.CopyBufferToTexture(tex, uint32(level), 0, staging, off)
		off += uint64(len(data))
	}
	cmd.Barrier(gpu.StageTransfer, gpu.StageVertex|gpu.StageFragment|gpu.StageCompute, 0)
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

// WritableConfig describes a texture that compute shaders write: a simulation's state,
// a baked lookup table, a noise volume — or, with Mips, a chain of mips each written from
// the one before: a blur pyramid, a depth chain, an environment's reflections by
// roughness.
type WritableConfig struct {
	// Kind is the texture's shape. Depth is a 3D texture's depth and Layers an array's
	// layer count — a cube's is 6; each is 1 when zero.
	Kind          gpu.TextureKind
	Width, Height uint32
	Depth         uint32
	Layers        uint32
	// Mips is how many mips it has, each half the size of the one before; 1 when zero.
	Mips uint32
	// Format is a gpu.Format rather than a Format: what the texture holds is the writing
	// shader's to define, not an image's to describe.
	Format gpu.Format
	Label  string
}

// WritableTexture is a texture that compute shaders write (see Store.CreateWritable): a
// Texture, which any shader samples whole through its Index, and the storage views its
// mips are written through.
type WritableTexture struct {
	Texture
	// Mips holds a storage view of each mip, for a shader writing that mip alone — each
	// Index is the heap slot to write through. For a texture of one mip, it is the
	// texture itself. The views belong to the texture, and go when it is released.
	Mips []gpu.Texture
}

// CreateWritable allocates a texture that compute shaders write, through the heap's
// storage arrays (gImages and gImages3D in bindless.glsl), and any shader samples,
// through its sampled ones (gTextures and gTextures3D). Nothing is uploaded: its contents
// are undefined until written, and whatever samples it has to come after whatever writes
// it.
//
// A texture of more than one mip is written one mip at a time, each through a storage
// view of its own (see WritableTexture.Mips); its Index samples them all.
func (t *Store) CreateWritable(config WritableConfig) WritableTexture {
	mips := max(config.Mips, 1)
	tex := t.backend.CreateTexture(gpu.TextureDescriptor{
		Kind: config.Kind, Width: config.Width, Height: config.Height,
		Depth: config.Depth, Layers: config.Layers, Mips: mips,
		Format: config.Format, Usage: gpu.TextureSampled | gpu.TextureStorage,
		Label: config.Label,
	})
	handle := t.handle(tex)
	if mips == 1 {
		return WritableTexture{Texture: handle, Mips: []gpu.Texture{tex}}
	}
	views := make([]gpu.Texture, mips)
	for mip := range views {
		views[mip] = t.backend.TextureView(tex, config.Kind, uint32(mip), 1, 0, max(config.Layers, 1))
	}
	t.entries.Value(handle.ref.ID()).mipViews = views
	return WritableTexture{Texture: handle, Mips: views}
}

// handle records a backend texture in the slab and returns a fresh single-ref handle.
func (t *Store) handle(tex gpu.Texture) Texture {
	id, gen := t.entries.Alloc(entry{tex: tex, index: tex.Index})
	return Texture{ref: ref.New(id, gen, t.dispose, t.validate), index: tex.Index}
}

// GPU resolves a handle to its backing backend texture (e.g. to bind a shadow map
// as a depth render attachment, or to transition it for sampling).
func (t *Store) GPU(tex Texture) gpu.Texture {
	return t.entries.Value(tex.ref.ID()).tex
}

// Destroy releases all uploaded textures and samplers.
//
// TODO: with two renderers that each loaded a glTF in one test, the second's teardown
// crashes here: vkDestroySampler is handed a null device. Not investigated; the loader's
// tests use one renderer each.
func (t *Store) Destroy() {
	for e := range t.entries.Values() {
		t.destroyEntry(&e)
	}
	for _, s := range t.samplers {
		t.backend.DestroySampler(s)
	}
	t.entries, t.samplers = mem.NewSlab[entry](), nil
}

// dispose/validate let a ref own a slot in this store.
func (t *Store) dispose(id uint32) {
	t.destroyEntry(t.entries.Value(id))
	t.entries.Free(id) // bumps the slot's generation
}

// destroyEntry frees an entry's texture and its mip views, leaving it empty.
func (t *Store) destroyEntry(e *entry) {
	for _, view := range e.mipViews {
		t.backend.DestroyTexture(view)
	}
	if e.tex.IsValid() {
		t.backend.DestroyTexture(e.tex)
	}
	*e = entry{}
}

func (t *Store) validate(id, gen uint32) bool {
	return t.entries.Generation(id) == gen
}
