package materials

import (
	"unsafe"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/ref"
	"github.com/bluescreen10/pix/shaders"
	"github.com/bluescreen10/pix/textures"
)

// BlinnPhongMaterial is ambient + diffuse (N·L) + Blinn-Phong specular.
//
// The material is its own GPU record: these fields are the source of truth and Bytes
// serializes them on demand. Every setter must call dirty(), or the change is never
// uploaded.
type BlinnPhongMaterial struct {
	pool *Pool
	ref  ref.Ref

	emissive  colors.RGB32F
	specular  float32
	shininess float32

	// Bound maps. The material holds the reference that keeps each texture alive; the
	// record's bindless index and presence flag are derived from it in Bytes, so they
	// cannot fall out of step with what is actually bound.
	color        colors.RGBA32F
	colorMap     textures.Texture
	colorSampler uint32

	// alphaCutoff makes the material masked: where its colour's alpha falls below it,
	// there is no surface (see Masked). 0 keeps every texel.
	alphaCutoff float32
}

// NewBlinnPhongMaterial creates a Blinn-Phong material with an unbound color map.
// Defaults to white with a soft highlight.
func NewBlinnPhongMaterial(store *Store) *BlinnPhongMaterial {
	st := store.Pool(Shader{Fragment: shaders.BlinnPhongFragment}, "BlinnPhong Material")
	m := &BlinnPhongMaterial{color: colors.RGBA32F{1, 1, 1, 1}, specular: 0.3, shininess: 32, colorSampler: store.DefaultSampler()}
	m.pool = st
	m.ref = st.Create(m)
	return m
}

// Bytes implements Instance: the 64-byte record matching the Material struct
// in shaders/src/scene_lit.frag.glsl. Field order and padding here ARE the GPU layout —
// changing either without changing the shader silently misreads every material.
func (m *BlinnPhongMaterial) Bytes() []byte {
	rec := struct {
		color        colors.RGBA32F
		emissive     colors.RGB32F
		_            float32 // the shader declares vec4; the 4th channel is unused
		specular     float32
		shininess    float32
		colorMap     uint32
		colorSampler uint32
		flags        uint32
		_, _, _      uint32
	}{
		color:        m.color,
		emissive:     m.emissive,
		specular:     m.specular,
		shininess:    m.shininess,
		colorMap:     MapIndex(m.colorMap),
		colorSampler: m.colorSampler,
		flags:        MapFlag(m.colorMap, MatColorMap),
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&rec)), unsafe.Sizeof(rec))
}

// Dispose implements Instance: drop every texture this material bound.
func (m *BlinnPhongMaterial) Dispose() {
	m.colorMap.Release()
}

// dirty marks the record for re-upload in the next Sync. Every setter must call it;
// one that does not leaves the GPU rendering the previous value indefinitely.
func (m *BlinnPhongMaterial) dirty() {
	m.pool.MarkDirty(m.ref.ID())
}

func (m *BlinnPhongMaterial) Color() colors.RGBA32F {
	return m.color
}

func (m *BlinnPhongMaterial) SetColor(color colors.RGBA32F) {
	m.color = color
	m.dirty()
}

func (m *BlinnPhongMaterial) Specular() float32 {
	return m.specular
}

func (m *BlinnPhongMaterial) SetSpecular(specular float32) {
	m.specular = specular
	m.dirty()
}

func (m *BlinnPhongMaterial) Shininess() float32 {
	return m.shininess
}

func (m *BlinnPhongMaterial) SetShininess(shininess float32) {
	m.shininess = shininess
	m.dirty()
}

// Emissive is the light the surface emits on its own, added after lighting. It has no
// alpha: emitted light is not a coverage, and the record's fourth channel is padding
// the shader never reads.
func (m *BlinnPhongMaterial) Emissive() colors.RGB32F {
	return m.emissive
}

func (m *BlinnPhongMaterial) SetEmissive(color colors.RGB32F) {
	m.emissive = color
	m.dirty()
}

// The bound textures (or a zero handle).
func (m *BlinnPhongMaterial) ColorMap() textures.Texture {
	return m.colorMap
}

// SetColorMap binds the base-color map; pass a zero textures.Texture to clear it. The material
// takes its own reference, so the caller may release theirs.
func (m *BlinnPhongMaterial) SetColorMap(texture textures.Texture) {
	old := m.colorMap
	m.colorMap = texture.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

// Per-map sampler getters/setters (bindless sampler heap indices).
func (m *BlinnPhongMaterial) ColorMapSampler() uint32 {
	return m.colorSampler
}

func (m *BlinnPhongMaterial) SetColorMapSampler(sampler uint32) {
	m.colorSampler = sampler
	m.dirty()
}

// --- Material ---
//
// Every method below is a plain store lookup. They are spelled out here, rather than
// inherited from a shared base, so that this file is the whole of BlinnPhongMaterial.

// Copy returns another handle to the same instance (refcount++). The material is its
// own record, so there is exactly one BlinnPhongMaterial per instance and this returns
// the same pointer — every handle observes the same fields.
func (m *BlinnPhongMaterial) Copy() Material {
	m.ref.Copy() // bumps the shared count; the ref.Ref it returns is identical to m.ref
	return m
}

// Release drops this handle's reference. At refcount 0 the store disposes the slot,
// which releases the textures the material bound.
func (m *BlinnPhongMaterial) Release() {
	m.ref.Release()
}

// Valid reports whether the underlying instance is still alive.
func (m *BlinnPhongMaterial) IsValid() bool {
	return m.ref.IsValid()
}

// Cull reports which triangle faces are discarded.
func (m *BlinnPhongMaterial) Cull() CullMode {
	return m.pool.CullAt(m.ref.ID())
}

// SetCull sets which faces are culled (CullNone = double-sided).
func (m *BlinnPhongMaterial) SetCull(mode CullMode) {
	m.pool.SetCullAt(m.ref.ID(), mode)
}

// SetDoubleSided is a convenience for SetCull(CullNone) / SetCull(CullBack).
func (m *BlinnPhongMaterial) SetDoubleSided(enabled bool) {
	if enabled {
		m.SetCull(CullNone)
	} else {
		m.SetCull(CullBack)
	}
}

// Blend reports the material's blend mode. Anything but BlendOpaque also pins the
// material to the forward path — the G-buffer holds one surface per pixel, so it
// cannot represent a fragment that composites over what is behind it.
func (m *BlinnPhongMaterial) Blend() BlendMode {
	return m.pool.BlendAt(m.ref.ID())
}

// SetBlend sets the material's blend mode (Opaque/Alpha/Additive/Premultiplied).
func (m *BlinnPhongMaterial) SetBlend(mode BlendMode) {
	m.pool.SetBlendAt(m.ref.ID(), mode)
}

// Pool returns the pool this material's records live in — its shader, pipeline
// identity and record buffer.
func (m *BlinnPhongMaterial) Pool() *Pool {
	return m.pool
}

// ID is the instance's index within its store; RecordsAddr is the store's
// record buffer address, resolved at draw time because it moves when the store grows.
func (m *BlinnPhongMaterial) ID() ID {
	return m.pool.IDOf(m.ref)
}

// AlphaCutoff is the alpha below which the material has no surface; 0, the default,
// keeps all of it.
func (m *BlinnPhongMaterial) AlphaCutoff() float32 {
	return m.alphaCutoff
}

// SetAlphaCutoff makes the material masked: where its colour's alpha — the colour map's
// times the colour's own — falls below cutoff, there is no surface, in shading, shadows
// and depth alike (glTF's alphaMode MASK, whose alphaCutoff defaults to 0.5). 0 makes it
// whole again.
func (m *BlinnPhongMaterial) SetAlphaCutoff(cutoff float32) {
	m.alphaCutoff = cutoff
	m.dirty()
}

// AlphaMask implements Masked: the material cuts its surface out by its colour's alpha
// once it has a cut-off.
func (m *BlinnPhongMaterial) AlphaMask() (AlphaMask, bool) {
	mask := AlphaMask{Map: m.colorMap, Sampler: m.colorSampler, Alpha: m.color[3], Cutoff: m.alphaCutoff}
	return mask, m.alphaCutoff > 0
}
