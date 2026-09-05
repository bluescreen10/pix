package pix

import (
	"bytes"
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/textures"
)

// noTextureIndex is the shader sentinel heap index for "no bound texture".
const noTextureIndex uint32 = 0xFFFFFFFF

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
// The interface is sealed: it has unexported methods, so only this package implements
// it. Each material type spells out every method itself, against its own store+ref
// fields, so a material type is entirely readable in its own file — see
// basic_material.go, blinn_phong_material.go, pbr_material.go and raw_material.go.
// A custom material outside this package is built on NewRawMaterial, not by
// implementing Material.
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

	// Cached 32-bit shader identities the renderer keys pipelines on instead of
	// comparing SPIR-V every frame (this is called once per mesh every frame).
	// shaderHash is (Vertex,Forward); deferredHash is (Vertex,Deferred) (0 if none);
	// lightingHash is Lighting alone (0 if none — it's a fullscreen pass, no vertex).
	shaderHash() uint32
	deferredHash() uint32
	lightingHash() uint32

	// Renderer-facing: the instance's index within its store, and the store's record
	// buffer address (resolved at draw time — it moves when the store grows).
	materialID() uint32
	materialsAddr() uint64
}

// materialSystem OWNS every material store (the renderer owns pipelines and issues
// draws, never a store). Stores are byte-based and created automatically, one per
// material kind (keyed by fragment shader): a material declares its shader + data
// size, and the system allocates storage — no hand-written per-type store.
type materialSystem struct {
	backend gpu.Backend
	stores  []*materialStore
}

func newMaterialSystem(backend gpu.Backend) *materialSystem {
	return &materialSystem{backend: backend}
}

// store returns the store for a material kind, creating it on first use. The kind is
// keyed by the fragment shader, which fixes the record layout; the record size comes
// from the first material registered. Textures are not the store's concern — each
// material holds its own references.
func (s *materialSystem) store(sh Shader, label string) *materialStore {
	// Fast path. Every built-in constructor hands us the same //go:embed slices on
	// every call, so matching slice headers settle it outright — worth a special case
	// because the slow path below hashes ~19KB of SPIR-V, which measured as ~99% of
	// the cost of creating a material (and a glTF scene creates hundreds).
	for _, st := range s.stores {
		if sameShaderData(st.sh, sh) {
			return st
		}
	}
	h := hashSPIRV(sh.Forward)
	for _, st := range s.stores {
		// The forward-shader hash fixes the record layout, but the whole Shader must
		// match before sharing a store: a store hands its own sh to every instance, so
		// matching on Forward alone would silently give a caller another Shader's
		// Deferred/Lighting (routing it through the G-buffer against a record layout it
		// never asked for, or losing its deferred path — depending on creation order).
		if st.forwardHash == h && sameShader(st.sh, sh) {
			return st
		}
	}
	st := newMaterialStore(s.backend, sh, label)
	s.stores = append(s.stores, st)
	return st
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

// mapIndex is the bindless heap index a material writes into its record for a bound
// texture, or the "unbound" sentinel. mapFlag is the matching presence bit. Deriving
// both from the Texture in Bytes keeps a record from ever disagreeing with what the
// material actually holds.
func mapIndex(t textures.Texture) uint32 {
	if t.Valid() {
		return t.Index()
	}
	return noTextureIndex
}

func mapFlag(t textures.Texture, flag uint32) uint32 {
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
	return sameSPIRVData(a.Vertex, b.Vertex) && sameSPIRVData(a.Forward, b.Forward) &&
		sameSPIRVData(a.Deferred, b.Deferred) && sameSPIRVData(a.Lighting, b.Lighting)
}

func sameSPIRVData(a, b []byte) bool {
	return len(a) == len(b) && unsafe.SliceData(a) == unsafe.SliceData(b)
}

// sameShader reports whether two Shaders carry identical SPIR-V in every stage.
func sameShader(a, b Shader) bool {
	return bytes.Equal(a.Vertex, b.Vertex) && bytes.Equal(a.Forward, b.Forward) &&
		bytes.Equal(a.Deferred, b.Deferred) && bytes.Equal(a.Lighting, b.Lighting)
}

// hashSPIRV is FNV-1a over SPIR-V bytes. Computed once per store, so shader identity
// checks (store dedup, draw-pipeline dedup) are a uint32 compare, not a slice scan.
func hashSPIRV(b []byte) uint32 {
	const offset, prime = uint32(2166136261), uint32(16777619)
	h := offset
	for _, c := range b {
		h = (h ^ uint32(c)) * prime
	}
	return h
}

// shaderHashOf combines a vertex + fragment SPIR-V pair into one 32-bit pipeline
// identity (nil vertex → the default vertex-pull shader, folded in as empty).
func shaderHashOf(vertex, fragment []byte) uint32 {
	const prime = uint32(16777619)
	h := hashSPIRV(vertex)
	for _, c := range fragment {
		h = (h ^ uint32(c)) * prime
	}
	return h
}

// Sync uploads every store's changed records into device memory. Material records are
// device-local, so accessor writes only touch a host shadow until this runs; call once
// per frame before recording draws. The caller flushes the uploader.
func (s *materialSystem) Sync(u *uploader) {
	for _, st := range s.stores {
		st.Sync(u)
	}
}

// Destroy releases every store.
func (s *materialSystem) Destroy() {
	for _, st := range s.stores {
		st.destroy()
	}
	s.stores = nil
}
