package pix

import (
	"bytes"

	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/shaders"
)

// noTextureIndex is the shader sentinel heap index for "no bound texture".
const noTextureIndex uint32 = 0xFFFFFFFF

// Material flag bits (mirror the per-type material structs in the shaders).
const (
	MatColorMap  uint32 = 1 << 0 // base-color map bound
	MatNormalMap uint32 = 1 << 1 // tangent-space normal map bound (PBR)
	MatMetalMap  uint32 = 1 << 2 // metallic map bound (PBR; sampled .b)
	MatRoughMap  uint32 = 1 << 3 // roughness map bound (PBR; sampled .g)
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

// Shader is a material type's GPU program. The vertex-pull stage is shared, so Vertex
// is usually nil (defaults to the scene vertex shader); Fragment is the material's
// fragment SPIR-V. Custom material types supply their own.
type Shader struct {
	Vertex   []byte // SPIR-V; nil => the default scene vertex-pull shader
	Fragment []byte // SPIR-V
}

// Built-in material shaders (fragment stage; the vertex stage is shared).
var (
	ShaderBasic      = Shader{Fragment: shaders.SceneBasic}
	ShaderBlinnPhong = Shader{Fragment: shaders.SceneLit}
	ShaderPBR        = Shader{Fragment: shaders.ScenePBR}
)

// Material is the handle a mesh holds. It is an interface: each material type owns
// its own storage (a per-type record buffer), and a Material value is a ref-counted
// instance in one of those stores. The renderer issues all draws; a Material reports
// the pieces the renderer needs to build its pipeline — the vertex + fragment shaders
// and the rasterization state (cull/blend) — plus where its record lives.
type Material interface {
	// Copy returns another handle to the same instance (refcount++).
	Copy() Material
	// Release drops this handle's reference (freed at refcount 0).
	Release()
	// Valid reports whether the underlying instance is still alive.
	Valid() bool

	// Pipeline identity — the renderer builds/caches one pipeline per distinct
	// (Vertex, Fragment, Cull, Blend). Vertex nil means the default vertex-pull shader.
	Vertex() []byte
	Fragment() []byte
	Cull() CullMode
	Blend() BlendMode

	// Data is the material's per-instance uniform bytes — what the fragment shader
	// reads from the material buffer (via BDA). The material system allocates storage
	// from its size, so a material only declares its layout (no hand-written store).
	Data() []byte

	// shaderHash is the cached 32-bit identity of (Vertex, Fragment) — the renderer
	// keys draw pipelines on it instead of comparing SPIR-V every frame.
	shaderHash() uint32

	// Renderer-facing: the instance's index within its store, and the store's record
	// buffer address (resolved at draw time — it moves when the store grows).
	materialID() uint32
	materialsAddr() uint64
}

// genericMaterial is the minimal Material a mesh holds and the base the typed
// material handles embed. It carries the store and the ref-counted instance handle;
// shaders come from the store, raster + data from the store's per-instance record.
type genericMaterial struct {
	store *materialStore
	ref   Ref
}

func (m genericMaterial) Copy() Material {
	return genericMaterial{store: m.store, ref: m.ref.Copy()}
}

func (m genericMaterial) Release() {
	m.ref.Release()
}

func (m genericMaterial) Valid() bool {
	return m.ref.Valid()
}

func (m genericMaterial) Vertex() []byte {
	return m.store.shader().Vertex
}

func (m genericMaterial) Fragment() []byte {
	return m.store.shader().Fragment
}

func (m genericMaterial) Cull() CullMode {
	return m.store.cullOf(m.ref.id)
}

// shaderHash is the cached draw-pipeline identity for this material's shaders (see
// the Material interface). It's precomputed once per store, so the renderer keys
// pipelines on a uint32 instead of comparing SPIR-V slices every frame.
func (m genericMaterial) shaderHash() uint32 {
	return m.store.pipeHash
}

func (m genericMaterial) Data() []byte {
	return m.store.dataOf(m.ref.id)
}

func (m genericMaterial) materialID() uint32 {
	return m.ref.id
}

func (m genericMaterial) materialsAddr() uint64 {
	return m.store.bufAddr()
}

// SetCull sets which faces are culled (CullNone = double-sided). Shared by every
// typed material via embedding.
func (m genericMaterial) SetCull(c CullMode) {
	m.store.setCullOf(m.ref.id, c)
}

// SetDoubleSided is a convenience for SetCull(CullNone|CullBack).
func (m genericMaterial) SetDoubleSided(v bool) {
	if v {
		m.SetCull(CullNone)
	} else {
		m.SetCull(CullBack)
	}
}

func (m genericMaterial) Blend() BlendMode {
	return m.store.blendOf(m.ref.id)
}

// SetBlend sets the material's blend mode (Opaque/Alpha/Additive).
func (m genericMaterial) SetBlend(b BlendMode) {
	m.store.setBlendOf(m.ref.id, b)
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
// keyed by the fragment shader (which fixes the record layout); stride is the record
// size in bytes, textureSlots the number of texture slots per instance.
func (s *materialSystem) store(sh Shader, stride uint32, textureSlots int, label string) *materialStore {
	h := hashSPIRV(sh.Fragment)
	for _, st := range s.stores {
		// The fragment hash fixes the record layout; verify the bytes on a hash hit
		// (collisions are vanishingly unlikely, but a false share would corrupt records).
		if st.fragHash == h && bytes.Equal(st.sh.Fragment, sh.Fragment) {
			return st
		}
	}
	st := newMaterialStore(s.backend, sh, stride, textureSlots, label)
	s.stores = append(s.stores, st)
	return st
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

// shaderHash combines a Shader's vertex + fragment SPIR-V into one 32-bit pipeline
// identity (nil vertex → the default vertex-pull shader, folded in as empty).
func shaderHash(sh Shader) uint32 {
	const prime = uint32(16777619)
	h := hashSPIRV(sh.Vertex)
	for _, c := range sh.Fragment {
		h = (h ^ uint32(c)) * prime
	}
	return h
}

// Destroy releases every store.
func (s *materialSystem) Destroy() {
	for _, st := range s.stores {
		st.destroy()
	}
	s.stores = nil
}
