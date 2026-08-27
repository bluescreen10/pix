package pix

import (
	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/shaders"
)

// noTextureIndex is the shader sentinel heap index for "no bound texture".
const noTextureIndex uint32 = 0xFFFFFFFF

// Material flag bits (mirror the per-type material structs in the shaders).
const MatColorMap uint32 = 1 << 0

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
	ShaderUnlit      = Shader{Fragment: shaders.SceneUnlit}
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

	// Renderer-facing: the instance's index within its store, and the store's record
	// buffer address (resolved at draw time — it moves when the store grows).
	materialID() uint32
	materialsAddr() uint64
}

// genericMaterial is the minimal Material a mesh holds and the base the typed
// material handles embed. It carries the store and the ref-counted instance handle;
// shaders come from the store, raster state from the store's per-instance record.
type genericMaterial struct {
	store materialStore
	ref   Ref
}

func (m genericMaterial) Copy() Material        { return genericMaterial{store: m.store, ref: m.ref.Copy()} }
func (m genericMaterial) Release()              { m.ref.Release() }
func (m genericMaterial) Valid() bool           { return m.ref.Valid() }
func (m genericMaterial) Vertex() []byte        { return m.store.shader().Vertex }
func (m genericMaterial) Fragment() []byte      { return m.store.shader().Fragment }
func (m genericMaterial) Cull() CullMode        { return m.store.rasterOf(m.ref.id).cull }
func (m genericMaterial) Blend() BlendMode      { return m.store.rasterOf(m.ref.id).blend }
func (m genericMaterial) materialID() uint32    { return m.ref.id }
func (m genericMaterial) materialsAddr() uint64 { return m.store.bufAddr() }

// SetCull sets which faces are culled (CullNone = double-sided). Shared by every
// typed material via embedding.
func (m genericMaterial) SetCull(c CullMode) {
	r := m.store.rasterOf(m.ref.id)
	r.cull = c
	m.store.setRasterOf(m.ref.id, r)
}

// SetDoubleSided is a convenience for SetCull(CullNone|CullBack).
func (m genericMaterial) SetDoubleSided(v bool) {
	if v {
		m.SetCull(CullNone)
	} else {
		m.SetCull(CullBack)
	}
}

// SetBlend sets the material's blend mode (Opaque/Alpha/Additive).
func (m genericMaterial) SetBlend(b BlendMode) {
	r := m.store.rasterOf(m.ref.id)
	r.blend = b
	m.store.setRasterOf(m.ref.id, r)
}

// materialSystem OWNS every per-type material store (the renderer owns pipelines and
// issues draws, never a store). It creates stores lazily, syncs and destroys them.
type materialSystem struct {
	backend gpu.Backend
	stores  []materialStore

	basic *materialSlab[basicRecord]
	blinn *materialSlab[blinnPhongRecord]
	pbr   *materialSlab[pbrRecord]
}

func newMaterialSystem(b gpu.Backend) *materialSystem {
	return &materialSystem{backend: b}
}

func (s *materialSystem) basicStore() *materialSlab[basicRecord] {
	if s.basic == nil {
		s.basic = newMaterialSlab[basicRecord](s.backend, ShaderUnlit, 1, "Basic Materials")
		s.stores = append(s.stores, s.basic)
	}
	return s.basic
}

func (s *materialSystem) blinnStore() *materialSlab[blinnPhongRecord] {
	if s.blinn == nil {
		s.blinn = newMaterialSlab[blinnPhongRecord](s.backend, ShaderBlinnPhong, 1, "BlinnPhong Materials")
		s.stores = append(s.stores, s.blinn)
	}
	return s.blinn
}

func (s *materialSystem) pbrStore() *materialSlab[pbrRecord] {
	if s.pbr == nil {
		s.pbr = newMaterialSlab[pbrRecord](s.backend, ShaderPBR, 1, "PBR Materials")
		s.stores = append(s.stores, s.pbr)
	}
	return s.pbr
}

// Sync uploads every dirty store.
func (s *materialSystem) Sync() {
	for _, st := range s.stores {
		st.sync()
	}
}

// Destroy releases every store.
func (s *materialSystem) Destroy() {
	for _, st := range s.stores {
		st.destroy()
	}
	s.stores, s.basic, s.blinn, s.pbr = nil, nil, nil, nil
}
