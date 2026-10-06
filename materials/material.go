package materials

import (
	"unsafe"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/textures"
)

// NoTextureIndex is the shader sentinel heap index for "no bound texture". A custom
// material writes it into its record for every map it has not bound.
const NoTextureIndex uint32 = 0xFFFFFFFF

// Material flag bits (mirror the per-type material structs in the shaders).
const (
	MatColorMap  uint32 = 1 << 0 // base-color map bound
	MatNormalMap uint32 = 1 << 1 // tangent-space normal map bound (PBR)
	MatMetalMap  uint32 = 1 << 2 // metallic map bound (PBR; sampled .b)
	MatRoughMap  uint32 = 1 << 3 // roughness map bound (PBR; sampled .g)
	MatTransMap  uint32 = 1 << 4 // transmission map bound (PBR; sampled .r)
	// MatOcclusionMap: occlusion map bound (PBR; sampled .r).
	MatOcclusionMap uint32 = 1 << 5
)

// CullMode selects which triangle faces are discarded. CullNone is double-sided.
type CullMode uint8

const (
	CullNone  CullMode = iota // double-sided
	CullBack                  // discard back faces
	CullFront                 // discard front faces
)

// BlendMode selects how a material's fragments blend with the target.
type BlendMode uint8

const (
	BlendOpaque   BlendMode = iota // no blending (writes replace)
	BlendAlpha                     // src-alpha over
	BlendAdditive                  // add
)

// Shader is a material type's GPU programs: a vertex stage and a fragment stage, and
// nothing else. The vertex-pull stage is shared, so Vertex is usually nil and defaults
// to the scene vertex shader; Fragment is required and does surface and lighting
// together, in one pass.
//
// There used to be four stages here — a G-buffer fill and a fullscreen lighting pass
// alongside these two — so that a material could opt into deferred shading. The
// renderer is forward-only now, which removes not just the two blobs but the question
// a material type had to answer about which combination it supplied.
//
// The fragment shader outputs linear, unclamped light — never display-encoded. Without
// HDR it lands in the sRGB target, which encodes it as it stores; with HDR it lands in
// the scene image, which the renderer tone-maps into that target at the end of the
// frame. A shader that encodes its own output is encoded twice and comes out washed out.
type Shader struct {
	Vertex   []byte // Backend-native bytes; nil => the default scene vertex-pull shader
	Fragment []byte // Backend-native bytes; required — surface + lighting in one pass, outputs linear light
}

// Built-ins identify shaders from the shaders package. Store.Pool selects the
// embedded backend-native variant before computing pipeline identity. Custom
// shaders must already be supplied in the selected backend's format.

// AlphaMask is how a material cuts its surface out: where Map's alpha, times Alpha, falls
// below Cutoff there is no surface — not when shaded, nor in the shadow maps or the
// depth prepass. Map is the material's colour map, or a zero Texture for none.
type AlphaMask struct {
	Map     textures.Texture
	Sampler uint32
	Alpha   float32
	Cutoff  float32
}

// Masked is implemented by materials that can cut their surface out by alpha — leaves,
// fences, grilles. A material that cannot need not implement it. One that does reports
// whether it is masked now, and calls its pool's MarkDirty whenever that or its mask
// changes, as for any change to its record.
//
// A masked material's fragment shader cuts its surface out by calling discardCutOut
// (material_common.glsl); the depth-only passes do it for every masked material alike,
// from the mask this reports, so it needs no shader of its own for them.
type Masked interface {
	AlphaMask() (AlphaMask, bool)
}

// ToBytes is the mask as an entry of a pool's mask table: AlphaMask in
// shaders/src/alpha_mask.glsl.
func (m AlphaMask) ToBytes() []byte {
	entry := struct {
		mapIndex uint32
		sampler  uint32
		alpha    float32
		cutoff   float32
	}{
		mapIndex: MapIndex(m.Map),
		sampler:  m.Sampler,
		alpha:    m.Alpha,
		cutoff:   m.Cutoff,
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&entry)), unsafe.Sizeof(entry))
}

// alphaMaskSize is the size of one entry of a pool's mask table (see AlphaMask.ToBytes).
const alphaMaskSize = 16

// ID names one material by value: which pool holds its record, its slot in that pool,
// and the slot's generation. It is the whole of what a renderer needs to find a
// material, which is what lets a material reference travel as plain data — through a
// frame packet, a GPU record index, an ECS component — instead of as a handle.
//
// Slot alone is not an identity. It is unique only within its pool, and only until the
// slot is reused: after the last handle to a material is released the pool hands that
// slot to the next one, and a stale Slot would then name a live material that is not
// the one it was taken from. Gen is what distinguishes them — see Pool.Live.
type ID struct {
	PoolID uint32 // Pool index within the owning Store (see Pool.Index).
	Slot   uint32 // Record index inside that pool.
	Gen    uint32 // Slot generation, to survive reuse.
}

// Material is the handle a mesh holds. Each material type owns its own storage (a
// per-type record buffer), and a Material value is a ref-counted instance in one of
// those stores. The renderer issues all draws.
//
// A Material answers only two questions about itself: which pool holds its record, and
// which slot it occupies there. It reports nothing about how it is drawn — the shaders,
// the rasterization state, the pipeline identity, the record buffer address all come
// from the pool, which is keyed by those very shaders and so cannot be contradicted.
//
// The interface is NOT sealed — implement it from any package to add a material type
// of your own. The built-ins each spell out every method themselves, against their own
// pool+ref fields, so a material type is entirely readable in its own file (basic.go,
// blinn_phong.go, pbr.go, raw.go) and doubles as a worked example. RawMaterial is the
// shortcut when you only need custom shaders + record bytes, not custom Go accessors.
type Material interface {
	// Copy returns another handle to the same instance (refcount++). Since a material
	// now *is* its own record, every handle is the same object: an edit through one is
	// seen by all of them.
	//
	//TODO: Copy meant something weaker before the record types were folded in (a
	// distinct handle over shared storage). Add Clone(), which allocates a SEPARATE
	// instance with the same field values — the operation callers reach for when they
	// want "another material like this one" and are currently served, wrongly, by Copy.
	Copy() Material
	// Release drops this handle's reference (freed at refcount 0).
	Release()
	// Valid reports whether the underlying instance is still alive.
	IsValid() bool

	// ID names this instance: its pool, its slot, and the slot's generation.
	ID() ID

	// Pool is where this material's record lives, and with it everything the renderer
	// needs to draw the material: the shaders (a pool is keyed by them, and they never
	// change for its lifetime), the per-slot cull and blend modes, the record buffer
	// address, and a dense index to key per-pool state by.
	//
	// This pair replaces the methods the interface used to require — Vertex, Forward,
	// Cull, Blend, Hash, RecordsAddr. Every one of them was a pure forward to this pool
	// in every implementation that ever existed, which is the evidence that none was a
	// question a material should have been answering.
	//
	// Cull and Blend are gone from this interface but remain on the concrete types,
	// where they are ordinary accessors for application code rather than something the
	// renderer consumes. See docs/frame-packet.md.
	Pool() *Pool
}

// Every material type implements Material by hand against its own store+ref, so a
// method that is missing, misspelled, or on the wrong receiver would otherwise only
// surface at the call site that passes the type to NewMesh. These pin it at build time.
var (
	_ Material = (*BasicMaterial)(nil)
	_ Material = (*BlinnPhongMaterial)(nil)
	_ Material = (*PBRMaterial)(nil)
	_ Material = (*RawMaterial)(nil)
	_ Material = (*BasicParticleMaterial)(nil)
)

// MapIndex is the bindless heap index a material writes into its record for a bound
// texture, or the "unbound" sentinel. MapFlag is the matching presence bit. Derive
// both from the Texture inside Bytes, as the built-ins do, and a record can never
// disagree with what the material actually holds.
func MapIndex(t textures.Texture) uint32 {
	if t.IsValid() {
		return t.Index()
	}
	return NoTextureIndex
}

func MapFlag(t textures.Texture, flag uint32) uint32 {
	if t.IsValid() {
		return flag
	}
	return 0
}

// sameShaderData reports whether two Shaders point at the very same bytes in every
// stage (same backing array and length), which makes them the same shader without
// reading any of it. Distinct arrays holding equal bytes return false — that is a
// miss, not an error, and falls through to the hash + compare path.
func sameShaderData(a, b Shader) bool {
	return SameSPIRV(a.Vertex, b.Vertex) && SameSPIRV(a.Fragment, b.Fragment)
}

// SameSPIRV reports whether two SPIR-V slices are the very same bytes (same backing
// array and length), which settles identity without reading any of it. Distinct
// arrays holding equal bytes return false — a miss, not an error.
func SameSPIRV(a, b []byte) bool {
	return len(a) == len(b) && unsafe.SliceData(a) == unsafe.SliceData(b)
}

// Uploader is the minimal capability Sync needs to stage a copy into device memory.
// Declared here, by the consumer, rather than imported from wherever an
// implementation lives — this package takes no dependency on that package at all;
// anything with a matching Copy method satisfies it for free.
type Uploader interface {
	Copy(dst gpu.Buffer, dstOffset uint32, data []byte)
}

// hashShader folds every stage of a Shader into one 64-bit shader identity: the value
// a Pool is keyed by, computed once when the pool is created (see Pool.Hash).
//
// 64 bits rather than 32 so the hash can *be* the identity. At 32 bits a match had to
// be confirmed by comparing every byte of every stage, because a collision would have
// silently handed a caller another shader's pool — and that comparison sat on the path
// a glTF import walks hundreds of times.
func hashShader(sh Shader) uint64 {
	const prime = uint64(1099511628211)
	h := HashBytes(sh.Vertex)
	for _, c := range sh.Fragment {
		h = (h ^ uint64(c)) * prime
	}
	return (h ^ 0xFF) * prime // stage separator, so concatenations can't alias
}

// HashBytes is FNV-1a over bytes. Computed once per pool, so shader identity
// checks (pool dedup, draw-pipeline dedup) are an integer compare, not a slice scan.
func HashBytes(b []byte) uint64 {
	const offset, prime = uint64(14695981039346656037), uint64(1099511628211)
	h := offset
	for _, c := range b {
		h = (h ^ uint64(c)) * prime
	}
	return h
}
