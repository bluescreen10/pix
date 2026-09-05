package materials

import (
	"bytes"
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
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

// Shader is a material type's GPU programs. The vertex-pull stage is shared, so Vertex
// is usually nil (defaults to the scene vertex shader). Forward is the material's
// always-required forward shader (surface + inline lighting, in one pass). Deferred and
// Lighting are optional: a material that supplies BOTH renders through the G-buffer
// (Deferred fills it, Lighting shades it in a fullscreen pass keyed to that shading
// model); a material that supplies neither always renders forward. Custom material
// types may supply any combination — see the Material interface's Deferred/Lighting
// docs for the eligibility rule.
type Shader struct {
	Vertex   []byte // SPIR-V; nil => the default scene vertex-pull shader
	Forward  []byte // SPIR-V; required — surface + lighting in one pass, outputs color
	Deferred []byte // SPIR-V; optional — fills the G-buffer (Surface only, no lighting)
	Lighting []byte // SPIR-V; optional — fullscreen deferred lighting pass for this model
}

// Each built-in material builds its own Shader inside its constructor, from SPIR-V
// embedded in the same file — see basic_material.go, blinn_phong_material.go and
// pbr_material.go.

// Alpha-testing (a "masked" material) has no representation yet: the G-buffer shaders
// do not discard on alpha, so a masked material routed deferred would silently render
// as opaque. Add the flag together with the discard support, not before.

// Material is the handle a mesh holds. Each material type owns its own storage (a
// per-type record buffer), and a Material value is a ref-counted instance in one of
// those stores. The renderer issues all draws; a Material reports the pieces the
// renderer needs to build its pipelines — the vertex/forward/deferred/lighting shaders
// and the rasterization state (cull/blend) — plus where its record lives.
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
	Valid() bool

	// Pipeline identity. Vertex nil means the default vertex-pull shader. Forward is
	// always used for blended materials, and for opaque ones that do not supply both
	// Deferred and Lighting. An opaque material supplying both renders through the
	// G-buffer instead: Deferred fills it every frame, and Lighting shades it once per
	// unique shading model (materials sharing a Lighting shader share a model, so one
	// fullscreen pass lights all of them).
	Vertex() []byte
	Forward() []byte
	Deferred() []byte // nil => this material always renders forward
	Lighting() []byte // nil => this material always renders forward
	Cull() CullMode
	Blend() BlendMode

	// Hash is this material's pipeline identity: the renderer keys its pipeline cache
	// on it instead of comparing SPIR-V every frame (this runs once per mesh per
	// frame). What goes into it is the implementation's call — whatever distinguishes
	// one of its pipelines from another. Anything the built-ins vary, their shaders,
	// is covered by hashing the whole Shader (see Pool.Hash); a material that also
	// varies on flags or specialization constants folds those in here too.
	//
	// The contract is only this: two materials returning the same Hash must be
	// interchangeable in every pass. Return a constant, or leave out something that
	// really does change the pipeline, and the renderer will quietly draw one of them
	// with the other's shaders.
	Hash() uint32

	// Renderer-facing: the instance's index within its pool, and the pool's record
	// buffer address (resolved at draw time — it moves when the pool grows).
	ID() uint32
	RecordsAddr() uint64
}

// Every material type implements Material by hand against its own store+ref, so a
// method that is missing, misspelled, or on the wrong receiver would otherwise only
// surface at the call site that passes the type to NewMesh. These pin it at build time.
var (
	_ Material = (*BasicMaterial)(nil)
	_ Material = (*BlinnPhongMaterial)(nil)
	_ Material = (*PBRMaterial)(nil)
	_ Material = (*RawMaterial)(nil)
)

// MapIndex is the bindless heap index a material writes into its record for a bound
// texture, or the "unbound" sentinel. MapFlag is the matching presence bit. Derive
// both from the Texture inside Bytes, as the built-ins do, and a record can never
// disagree with what the material actually holds.
func MapIndex(t textures.Texture) uint32 {
	if t.Valid() {
		return t.Index()
	}
	return NoTextureIndex
}

func MapFlag(t textures.Texture, flag uint32) uint32 {
	if t.Valid() {
		return flag
	}
	return 0
}

// sameShaderData reports whether two Shaders point at the very same bytes in every
// stage (same backing array and length), which makes them the same shader without
// reading any of it. Distinct arrays holding equal bytes return false — that is a
// miss, not an error, and falls through to the hash + compare path.
func sameShaderData(a, b Shader) bool {
	return SameSPIRV(a.Vertex, b.Vertex) && SameSPIRV(a.Forward, b.Forward) &&
		SameSPIRV(a.Deferred, b.Deferred) && SameSPIRV(a.Lighting, b.Lighting)
}

// SameSPIRV reports whether two SPIR-V slices are the very same bytes (same backing
// array and length), which settles identity without reading any of it. Distinct
// arrays holding equal bytes return false — a miss, not an error.
func SameSPIRV(a, b []byte) bool {
	return len(a) == len(b) && unsafe.SliceData(a) == unsafe.SliceData(b)
}

// sameShader reports whether two Shaders carry identical SPIR-V in every stage.
func sameShader(a, b Shader) bool {
	return bytes.Equal(a.Vertex, b.Vertex) && bytes.Equal(a.Forward, b.Forward) &&
		bytes.Equal(a.Deferred, b.Deferred) && bytes.Equal(a.Lighting, b.Lighting)
}

// Uploader is the minimal capability Sync needs to stage a copy into device memory.
// Declared here, by the consumer, rather than imported from wherever an
// implementation lives — this package takes no dependency on that package at all;
// anything with a matching Copy method satisfies it for free.
type Uploader interface {
	Copy(dst gpu.Buffer, dstOffset uint32, data []byte)
}

// hashShader folds every stage of a Shader into one pipeline identity — the default
// Material.Hash for anything whose pipelines vary only by shader (see Pool.Hash).
func hashShader(sh Shader) uint32 {
	const prime = uint32(16777619)
	h := HashSPIRV(sh.Vertex)
	for _, b := range [][]byte{sh.Forward, sh.Deferred, sh.Lighting} {
		for _, c := range b {
			h = (h ^ uint32(c)) * prime
		}
		h = (h ^ 0xFF) * prime // stage separator, so concatenations can't alias
	}
	return h
}

// HashSPIRV is FNV-1a over SPIR-V bytes. Computed once per pool, so shader identity
// checks (pool dedup, draw-pipeline dedup) are a uint32 compare, not a slice scan.
// Exported as a building block for a custom Material's Hash.
func HashSPIRV(b []byte) uint32 {
	const offset, prime = uint32(2166136261), uint32(16777619)
	h := offset
	for _, c := range b {
		h = (h ^ uint32(c)) * prime
	}
	return h
}
