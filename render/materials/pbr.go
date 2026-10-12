package materials

import (
	"math"
	"unsafe"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/ref"
	"github.com/bluescreen10/pix/render/shaders"
	"github.com/bluescreen10/pix/render/textures"
)

// The optional parts of the PBR shader (see Features).
const (
	// FeatureClearcoat shades a clear coat over the surface (see SetClearcoat).
	FeatureClearcoat Features = 1 << iota
	// FeatureSheen shades a sheen over the surface (see SetSheenColor).
	FeatureSheen
	// FeatureAnisotropy stretches the surface's highlights along one direction (see
	// SetAnisotropy).
	FeatureAnisotropy
)

// PBRMaterial is a metallic-roughness physically-based material (Cook-Torrance) with
// optional base-color, normal, metallic, roughness, occlusion and transmission maps.
//
// The material is its own GPU record: these fields are the source of truth and Bytes
// serializes them on demand. Every setter must call dirty(), or the change is never
// uploaded.
type PBRMaterial struct {
	pool *Pool
	ref  ref.Ref

	emissive     colors.RGB32F
	transmission float32 // 0 = opaque; >0 = see-through (glass), needs alpha blend

	// emissiveMap scales emissive per texel (glTF's emissiveTexture): a lamp's glass,
	// glowing where its filament shows through.
	emissiveMap     textures.Texture
	emissiveSampler uint32

	// Bound maps. The material holds the reference that keeps each texture alive; the
	// record's bindless index and presence flag are derived from it in Bytes, so they
	// cannot fall out of step with what is actually bound.
	color        colors.RGBA32F
	colorMap     textures.Texture
	colorSampler uint32

	normalMap     textures.Texture
	normalSampler uint32

	metallic        float32
	metallicMap     textures.Texture
	metallicSampler uint32

	roughness        float32
	roughnessMap     textures.Texture
	roughnessSampler uint32

	// transmissionMap scales transmission per texel (glTF KHR_materials_transmission
	// stores it in red). Without it a partly-glass object — a cabinet with panes, a
	// sign with a glass front — becomes uniformly transparent.
	transmissionMap     textures.Texture
	transmissionSampler uint32

	// occlusionMap is how much of the light around it reaches each texel, baked into the
	// map's red channel (glTF's occlusionTexture): it darkens ambient and environment
	// light, never direct light. occlusionStrength is how far: 0 not at all, 1 fully.
	occlusionStrength float32
	occlusionMap      textures.Texture
	occlusionSampler  uint32

	// How much a dielectric reflects, beside what its index of refraction says
	// (KHR_materials_specular; see SetSpecularIntensity and SetSpecularColor).
	specularIntensity        float32
	specularIntensityMap     textures.Texture
	specularIntensitySampler uint32
	specularColor            colors.RGB32F
	specularColorMap         textures.Texture
	specularColorSampler     uint32

	// A clear coat over the surface (KHR_materials_clearcoat; see SetClearcoat).
	clearcoat                 float32
	clearcoatMap              textures.Texture
	clearcoatSampler          uint32
	clearcoatRoughness        float32
	clearcoatRoughnessMap     textures.Texture
	clearcoatRoughnessSampler uint32
	clearcoatNormalMap        textures.Texture
	clearcoatNormalSampler    uint32

	// A sheen over the surface (KHR_materials_sheen; see SetSheenColor).
	sheenColor            colors.RGB32F
	sheenColorMap         textures.Texture
	sheenColorSampler     uint32
	sheenRoughness        float32
	sheenRoughnessMap     textures.Texture
	sheenRoughnessSampler uint32

	// How far, and along which direction, highlights stretch (KHR_materials_anisotropy;
	// see SetAnisotropy).
	anisotropy         float32
	anisotropyRotation float32
	anisotropyMap      textures.Texture
	anisotropySampler  uint32

	// uvTransforms moves each set of the mesh's texture coordinates before the maps
	// reading it are sampled (see SetUVTransform); uv2Maps flags the maps that read UV2.
	uvTransforms [2]UVTransform
	uv2Maps      uint32

	// alphaCutoff makes the material masked: where its colour's alpha falls below it,
	// there is no surface (see Masked). 0 keeps every texel.
	alphaCutoff float32
	// ignoresAlpha makes the surface whole wherever it is drawn, whatever its colour's
	// alpha (see SetIgnoresAlpha).
	ignoresAlpha bool

	// The volume behind a transmissive surface (see SetIOR, SetThickness and
	// SetAttenuation).
	ior                 float32
	thickness           float32
	attenuationColor    colors.RGB32F
	attenuationDistance float32
}

// NewPBRMaterial creates a PBR material with no maps bound.
func NewPBRMaterial(store *Store) *PBRMaterial {
	st := store.Pool(Shader{Fragment: shaders.PBRFragment}, "PBR Material")
	sampler := store.DefaultSampler()
	m := &PBRMaterial{
		color: colors.RGBA32F{1, 1, 1, 1}, roughness: 0.5, occlusionStrength: 1,
		ior: 1.5, attenuationColor: colors.RGB32F{1, 1, 1},
		colorSampler: sampler, normalSampler: sampler, metallicSampler: sampler,
		roughnessSampler: sampler, transmissionSampler: sampler, occlusionSampler: sampler,
		emissiveSampler: sampler, uvTransforms: [2]UVTransform{NoUVTransform, NoUVTransform},
		specularIntensity: 1, specularColor: colors.RGB32F{1, 1, 1},
		specularIntensitySampler: sampler, specularColorSampler: sampler,
		clearcoatSampler: sampler, clearcoatRoughnessSampler: sampler, clearcoatNormalSampler: sampler,
		sheenColorSampler: sampler, sheenRoughnessSampler: sampler,
		anisotropySampler: sampler,
	}
	m.pool = st
	m.ref = st.Create(m)
	return m
}

// Bytes implements Instance: the 284-byte record matching the Material struct
// in shaders/src/scene_pbr.frag.glsl. Field order and padding here ARE the GPU layout —
// changing either without changing the shader silently misreads every material.
func (m *PBRMaterial) Bytes() []byte {
	rec := struct {
		color               colors.RGBA32F
		emissive            colors.RGB32F
		_                   float32 // the shader declares vec4; the 4th channel is unused
		metallic            float32
		roughness           float32
		transmission        float32
		flags               uint32
		colorMap            uint32
		colorSampler        uint32
		normalMap           uint32
		normalSampler       uint32
		metallicMap         uint32
		metallicSampler     uint32
		roughnessMap        uint32
		roughnessSampler    uint32
		transMap            uint32
		transSampler        uint32
		occlusionStrength   float32
		occlusionMap        uint32
		occlusionSampler    uint32
		ior                 float32
		thickness           float32
		attenuationDistance float32
		attenuationColor    colors.RGB32F
		emissiveMap         uint32
		emissiveSampler     uint32
		uv2Maps             uint32

		dielectricSpecular       [4]float32
		specularIntensityMap     uint32
		specularIntensitySampler uint32
		specularColorMap         uint32
		specularColorSampler     uint32

		clearcoat                 float32
		clearcoatRoughness        float32
		clearcoatMap              uint32
		clearcoatSampler          uint32
		clearcoatRoughnessMap     uint32
		clearcoatRoughnessSampler uint32
		clearcoatNormalMap        uint32
		clearcoatNormalSampler    uint32

		sheenColor            colors.RGB32F
		sheenRoughness        float32
		sheenColorMap         uint32
		sheenColorSampler     uint32
		sheenRoughnessMap     uint32
		sheenRoughnessSampler uint32

		anisotropy          float32
		anisotropyDirection [2]float32
		anisotropyMap       uint32
		anisotropySampler   uint32

		uvTransforms [2]UVTransform
	}{
		color:        m.color,
		emissive:     m.emissive,
		metallic:     m.metallic,
		roughness:    m.roughness,
		transmission: m.transmission,
		flags: MapFlag(m.colorMap, MatColorMap) | MapFlag(m.normalMap, MatNormalMap) |
			MapFlag(m.metallicMap, MatMetalMap) | MapFlag(m.roughnessMap, MatRoughMap) |
			MapFlag(m.transmissionMap, MatTransMap) | MapFlag(m.occlusionMap, MatOcclusionMap) |
			MapFlag(m.emissiveMap, MatEmissiveMap) | ignoresAlphaFlag(m.ignoresAlpha) |
			MapFlag(m.specularIntensityMap, MatSpecularIntensityMap) |
			MapFlag(m.specularColorMap, MatSpecularColorMap) | MapFlag(m.clearcoatMap, MatClearcoatMap) |
			MapFlag(m.clearcoatRoughnessMap, MatClearcoatRoughnessMap) |
			MapFlag(m.clearcoatNormalMap, MatClearcoatNormalMap) | MapFlag(m.sheenColorMap, MatSheenColorMap) |
			MapFlag(m.sheenRoughnessMap, MatSheenRoughnessMap) | MapFlag(m.anisotropyMap, MatAnisotropyMap),
		colorMap: MapIndex(m.colorMap), colorSampler: m.colorSampler,
		normalMap: MapIndex(m.normalMap), normalSampler: m.normalSampler,
		metallicMap: MapIndex(m.metallicMap), metallicSampler: m.metallicSampler,
		roughnessMap: MapIndex(m.roughnessMap), roughnessSampler: m.roughnessSampler,
		transMap: MapIndex(m.transmissionMap), transSampler: m.transmissionSampler,
		occlusionStrength: m.occlusionStrength,
		occlusionMap:      MapIndex(m.occlusionMap), occlusionSampler: m.occlusionSampler,
		ior: m.ior, thickness: m.thickness,
		attenuationDistance: m.attenuationDistance, attenuationColor: m.attenuationColor,
		emissiveMap: MapIndex(m.emissiveMap), emissiveSampler: m.emissiveSampler,
		uv2Maps: m.uv2Maps,

		dielectricSpecular:   m.dielectricSpecular(),
		specularIntensityMap: MapIndex(m.specularIntensityMap), specularIntensitySampler: m.specularIntensitySampler,
		specularColorMap: MapIndex(m.specularColorMap), specularColorSampler: m.specularColorSampler,

		clearcoat:          m.clearcoat,
		clearcoatRoughness: m.clearcoatRoughness,
		clearcoatMap:       MapIndex(m.clearcoatMap), clearcoatSampler: m.clearcoatSampler,
		clearcoatRoughnessMap: MapIndex(m.clearcoatRoughnessMap), clearcoatRoughnessSampler: m.clearcoatRoughnessSampler,
		clearcoatNormalMap: MapIndex(m.clearcoatNormalMap), clearcoatNormalSampler: m.clearcoatNormalSampler,

		sheenColor:     m.sheenColor,
		sheenRoughness: m.sheenRoughness,
		sheenColorMap:  MapIndex(m.sheenColorMap), sheenColorSampler: m.sheenColorSampler,
		sheenRoughnessMap: MapIndex(m.sheenRoughnessMap), sheenRoughnessSampler: m.sheenRoughnessSampler,

		anisotropy:          m.anisotropy,
		anisotropyDirection: anisotropyDirection(m.anisotropyRotation),
		anisotropyMap:       MapIndex(m.anisotropyMap), anisotropySampler: m.anisotropySampler,

		uvTransforms: m.uvTransforms,
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&rec)), unsafe.Sizeof(rec))
}

// Dispose implements Instance: drop every texture this material bound.
func (m *PBRMaterial) Dispose() {
	m.colorMap.Release()
	m.normalMap.Release()
	m.metallicMap.Release()
	m.roughnessMap.Release()
	m.transmissionMap.Release()
	m.occlusionMap.Release()
	m.emissiveMap.Release()
	m.specularIntensityMap.Release()
	m.specularColorMap.Release()
	m.clearcoatMap.Release()
	m.clearcoatRoughnessMap.Release()
	m.clearcoatNormalMap.Release()
	m.sheenColorMap.Release()
	m.sheenRoughnessMap.Release()
	m.anisotropyMap.Release()
}

// dielectricSpecular is what the surface reflects where it is not metal, before its
// specular maps: head-on, in its first three, by its index of refraction (0.04 for the
// usual 1.5) tinted by its specular colour, and at a grazing angle, in its fourth, its
// specular intensity — which scales the head-on reflectance too (KHR_materials_ior and
// KHR_materials_specular). It depends on nothing that varies across the surface, so it
// is worked out here, once, rather than per fragment.
func (m *PBRMaterial) dielectricSpecular() [4]float32 {
	ratio := (m.ior - 1) / (m.ior + 1)
	var specular [4]float32
	for i, c := range m.specularColor {
		specular[i] = min(ratio*ratio*c, 1) * m.specularIntensity
	}
	specular[3] = m.specularIntensity
	return specular
}

// anisotropyDirection is the rotation's cosine and sine, as the shader turns the
// direction of anisotropy by them: worked out here once rather than per fragment.
func anisotropyDirection(rotation float32) [2]float32 {
	sin, cos := math.Sincos(float64(rotation))
	return [2]float32{float32(cos), float32(sin)}
}

// dirty marks the record for re-upload in the next Sync. Every setter must call it;
// one that does not leaves the GPU rendering the previous value indefinitely.
func (m *PBRMaterial) dirty() {
	m.pool.MarkDirty(m.ref.ID())
}

func (m *PBRMaterial) Color() colors.RGBA32F {
	return m.color
}

func (m *PBRMaterial) SetColor(color colors.RGBA32F) {
	m.color = color
	m.dirty()
}

func (m *PBRMaterial) Metallic() float32 {
	return m.metallic
}

func (m *PBRMaterial) SetMetallic(metallic float32) {
	m.metallic = metallic
	m.dirty()
}

func (m *PBRMaterial) Roughness() float32 {
	return m.roughness
}

func (m *PBRMaterial) SetRoughness(roughness float32) {
	m.roughness = roughness
	m.dirty()
}

// Transmission returns the transmission factor [0,1] (glass-like see-through).
func (m *PBRMaterial) Transmission() float32 {
	return m.transmission
}

// SetTransmission sets how much light passes through the surface [0,1] — glass. A
// value > 0 switches an opaque or alpha-blended material to BlendPremultiplied, which
// glass needs: the shader writes the light the surface reflects as it is, and alpha as
// how much of the scene behind the surface it keeps out. Under BlendAlpha that alpha
// would scale the reflection too, and a clear pane would all but vanish. Never switches
// back on its own; call SetBlend explicitly if you clear transmission.
func (m *PBRMaterial) SetTransmission(transmission float32) {
	m.transmission = transmission
	m.dirty()
	if transmission > 0 && (m.Blend() == BlendOpaque || m.Blend() == BlendAlpha) {
		m.SetBlend(BlendPremultiplied)
	}
}

// IOR returns the index of refraction (see SetIOR).
func (m *PBRMaterial) IOR() float32 {
	return m.ior
}

// SetIOR sets the index of refraction of what the surface is made of: how much it
// reflects of the light meeting it head-on where it is not metal, ((ior-1)/(ior+1))^2,
// and how sharply it bends the light passing through it, with transmission (glTF's
// KHR_materials_ior). 1.5 — glass, reflecting 4% — by default; water is 1.33.
func (m *PBRMaterial) SetIOR(ior float32) {
	m.ior = ior
	m.dirty()
}

// Thickness returns how far light crosses the volume behind the surface (see
// SetThickness).
func (m *PBRMaterial) Thickness() float32 {
	return m.thickness
}

// SetThickness sets how far, in world units, light that passes through the surface
// travels through the volume behind it before leaving it: what it is carried along, by
// its refraction, and absorbed over (glTF KHR_materials_volume's thickness, which glTF
// measures in the mesh's own units). 0, the default, is a thin wall: a whole window
// pane, front and back surface in one, which reflects from both — about 8% head-on, for
// glass, where one surface reflects 4% — and blurs what is behind it by its roughness,
// but does not move it. Model a pane as a single sheet: a box with two glass faces and
// no thickness reflects from both of those too where the scene is not copied (see
// below).
//
// Refraction needs a copy of the scene to read, which the renderer takes only when it
// draws in HDR, or with FXAA; without one, glass shows what is behind it by blending,
// unbent and untinted.
func (m *PBRMaterial) SetThickness(thickness float32) {
	m.thickness = thickness
	m.dirty()
}

// AttenuationColor and AttenuationDistance return how the volume behind the surface
// absorbs light (see SetAttenuation).
func (m *PBRMaterial) AttenuationColor() colors.RGB32F {
	return m.attenuationColor
}

func (m *PBRMaterial) AttenuationDistance() float32 {
	return m.attenuationDistance
}

// SetAttenuation sets how the volume behind a transmissive surface absorbs the light
// crossing it: light that has travelled distance through it keeps color of itself, and
// farther, proportionally less (glTF KHR_materials_volume's attenuationColor and
// attenuationDistance). A distance of 0, the default, absorbs nothing. It only matters
// with a thickness.
func (m *PBRMaterial) SetAttenuation(color colors.RGB32F, distance float32) {
	m.attenuationColor = color
	m.attenuationDistance = distance
	m.dirty()
}

// Emissive is the light the surface emits on its own, added after lighting. It has no
// alpha: emitted light is not a coverage, and the record's fourth channel is padding
// the shader never reads.
func (m *PBRMaterial) Emissive() colors.RGB32F {
	return m.emissive
}

func (m *PBRMaterial) SetEmissive(color colors.RGB32F) {
	m.emissive = color
	m.dirty()
}

// EmissiveMap returns the bound emissive map.
func (m *PBRMaterial) EmissiveMap() textures.Texture {
	return m.emissiveMap
}

// SetEmissiveMap binds a map of the light each texel emits, in sRGB colour, multiplied
// with Emissive (glTF's emissiveTexture, which glTF scales by its emissiveFactor). With
// a map bound and Emissive black, the surface emits nothing: set both.
func (m *PBRMaterial) SetEmissiveMap(texture textures.Texture) {
	newRef := texture.Copy()
	m.emissiveMap.Release()
	m.emissiveMap = newRef
	m.dirty()
}

// SetEmissiveMapSampler sets the heap index of the sampler used for the emissive map.
func (m *PBRMaterial) SetEmissiveMapSampler(sampler uint32) {
	m.emissiveSampler = sampler
	m.dirty()
}

// EmissiveMapUV is the set of texture coordinates the emissive map reads.
func (m *PBRMaterial) EmissiveMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatEmissiveMap)
}

// SetEmissiveMapUV sets which of the mesh's sets of texture coordinates the emissive map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetEmissiveMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatEmissiveMap, set)
	m.dirty()
}

// SpecularIntensity is how much of its specular reflection a dielectric keeps (see
// SetSpecularIntensity).
func (m *PBRMaterial) SpecularIntensity() float32 {
	return m.specularIntensity
}

// SetSpecularIntensity scales how much a dielectric reflects, at every angle: 1, the
// default, reflects what its index of refraction says, 0 nothing — a surface with no
// highlight and no reflection, all diffuse. What it does not reflect it scatters
// diffusely. It does not affect metal (KHR_materials_specular's specularFactor).
func (m *PBRMaterial) SetSpecularIntensity(intensity float32) {
	m.specularIntensity = intensity
	m.dirty()
}

// SpecularIntensityMap returns the bound specular intensity map.
func (m *PBRMaterial) SpecularIntensityMap() textures.Texture {
	return m.specularIntensityMap
}

// SetSpecularIntensityMap binds a map whose ALPHA channel scales SpecularIntensity per
// texel (glTF's specularTexture). The material takes its own reference.
func (m *PBRMaterial) SetSpecularIntensityMap(texture textures.Texture) {
	newRef := texture.Copy()
	m.specularIntensityMap.Release()
	m.specularIntensityMap = newRef
	m.dirty()
}

// SetSpecularIntensityMapSampler sets the heap index of the sampler used for the
// specular intensity map.
func (m *PBRMaterial) SetSpecularIntensityMapSampler(sampler uint32) {
	m.specularIntensitySampler = sampler
	m.dirty()
}

// SpecularIntensityMapUV is the set of texture coordinates the specular intensity map reads.
func (m *PBRMaterial) SpecularIntensityMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatSpecularIntensityMap)
}

// SetSpecularIntensityMapUV sets which of the mesh's sets of texture coordinates the specular intensity map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetSpecularIntensityMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatSpecularIntensityMap, set)
	m.dirty()
}

// SpecularColor tints a dielectric's reflection seen head-on (see SetSpecularColor).
func (m *PBRMaterial) SpecularColor() colors.RGB32F {
	return m.specularColor
}

// SetSpecularColor tints what a dielectric reflects seen head-on; toward grazing angles
// its reflection turns white, as every surface's does. White, the default, leaves it
// untinted. It does not affect metal, whose reflection is its colour
// (KHR_materials_specular's specularColorFactor).
func (m *PBRMaterial) SetSpecularColor(color colors.RGB32F) {
	m.specularColor = color
	m.dirty()
}

// SpecularColorMap returns the bound specular colour map.
func (m *PBRMaterial) SpecularColorMap() textures.Texture {
	return m.specularColorMap
}

// SetSpecularColorMap binds a map, in sRGB colour, that multiplies SpecularColor per
// texel (glTF's specularColorTexture). The material takes its own reference.
func (m *PBRMaterial) SetSpecularColorMap(texture textures.Texture) {
	newRef := texture.Copy()
	m.specularColorMap.Release()
	m.specularColorMap = newRef
	m.dirty()
}

// SetSpecularColorMapSampler sets the heap index of the sampler used for the specular
// colour map.
func (m *PBRMaterial) SetSpecularColorMapSampler(sampler uint32) {
	m.specularColorSampler = sampler
	m.dirty()
}

// SpecularColorMapUV is the set of texture coordinates the specular colour map reads.
func (m *PBRMaterial) SpecularColorMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatSpecularColorMap)
}

// SetSpecularColorMapUV sets which of the mesh's sets of texture coordinates the specular colour map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetSpecularColorMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatSpecularColorMap, set)
	m.dirty()
}

// Clearcoat is how much of a clear lacquer covers the surface (see SetClearcoat).
func (m *PBRMaterial) Clearcoat() float32 {
	return m.clearcoat
}

// SetClearcoat covers the surface with a clear, thin, reflective layer — car paint,
// varnished wood, a lacquered box: 0, the default, none, 1 a whole coat. The coat has
// its own roughness and normal (see SetClearcoatRoughness and SetClearcoatNormalMap),
// reflects like a dielectric of index 1.5, and what it reflects does not reach the
// surface under it (KHR_materials_clearcoat's clearcoatFactor).
//
// Shading the coat is a second specular reflection for every light, which the
// material's pipeline compiles in only while some material drawn with it has a coat
// (see FeatureClearcoat).
func (m *PBRMaterial) SetClearcoat(clearcoat float32) {
	m.clearcoat = clearcoat
	m.dirty()
}

// ClearcoatRoughness is how rough the clear coat is (see SetClearcoatRoughness).
func (m *PBRMaterial) ClearcoatRoughness() float32 {
	return m.clearcoatRoughness
}

// SetClearcoatRoughness sets how rough the clear coat is, from 0, a mirror — the
// default — to 1 (KHR_materials_clearcoat's clearcoatRoughnessFactor).
func (m *PBRMaterial) SetClearcoatRoughness(roughness float32) {
	m.clearcoatRoughness = roughness
	m.dirty()
}

// ClearcoatMap returns the bound clear coat map.
func (m *PBRMaterial) ClearcoatMap() textures.Texture {
	return m.clearcoatMap
}

// SetClearcoatMap binds a map whose RED channel scales Clearcoat per texel (glTF's clearcoatTexture). The material takes its own reference.
func (m *PBRMaterial) SetClearcoatMap(texture textures.Texture) {
	newRef := texture.Copy()
	m.clearcoatMap.Release()
	m.clearcoatMap = newRef
	m.dirty()
}

// SetClearcoatMapSampler sets the heap index of the sampler used for the clear coat map.
func (m *PBRMaterial) SetClearcoatMapSampler(sampler uint32) {
	m.clearcoatSampler = sampler
	m.dirty()
}

// ClearcoatMapUV is the set of texture coordinates the clear coat map reads.
func (m *PBRMaterial) ClearcoatMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatClearcoatMap)
}

// SetClearcoatMapUV sets which of the mesh's sets of texture coordinates the clear coat map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetClearcoatMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatClearcoatMap, set)
	m.dirty()
}

// ClearcoatRoughnessMap returns the bound clear coat roughness map.
func (m *PBRMaterial) ClearcoatRoughnessMap() textures.Texture {
	return m.clearcoatRoughnessMap
}

// SetClearcoatRoughnessMap binds a map whose GREEN channel scales ClearcoatRoughness per texel (glTF's
// clearcoatRoughnessTexture). The material takes its own reference.
func (m *PBRMaterial) SetClearcoatRoughnessMap(texture textures.Texture) {
	newRef := texture.Copy()
	m.clearcoatRoughnessMap.Release()
	m.clearcoatRoughnessMap = newRef
	m.dirty()
}

// SetClearcoatRoughnessMapSampler sets the heap index of the sampler used for the clear coat roughness map.
func (m *PBRMaterial) SetClearcoatRoughnessMapSampler(sampler uint32) {
	m.clearcoatRoughnessSampler = sampler
	m.dirty()
}

// ClearcoatRoughnessMapUV is the set of texture coordinates the clear coat roughness map reads.
func (m *PBRMaterial) ClearcoatRoughnessMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatClearcoatRoughnessMap)
}

// SetClearcoatRoughnessMapUV sets which of the mesh's sets of texture coordinates the clear coat roughness map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetClearcoatRoughnessMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatClearcoatRoughnessMap, set)
	m.dirty()
}

// ClearcoatNormalMap returns the bound clear coat normal map.
func (m *PBRMaterial) ClearcoatNormalMap() textures.Texture {
	return m.clearcoatNormalMap
}

// SetClearcoatNormalMap binds a tangent-space normal map for the clear coat alone (glTF's
// clearcoatNormalTexture). Without one the coat is smooth over the surface's own
// normal map: a lacquer fills the bumps under it. The material takes its own reference.
func (m *PBRMaterial) SetClearcoatNormalMap(texture textures.Texture) {
	newRef := texture.Copy()
	m.clearcoatNormalMap.Release()
	m.clearcoatNormalMap = newRef
	m.dirty()
}

// SetClearcoatNormalMapSampler sets the heap index of the sampler used for the clear coat normal map.
func (m *PBRMaterial) SetClearcoatNormalMapSampler(sampler uint32) {
	m.clearcoatNormalSampler = sampler
	m.dirty()
}

// ClearcoatNormalMapUV is the set of texture coordinates the clear coat normal map reads.
func (m *PBRMaterial) ClearcoatNormalMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatClearcoatNormalMap)
}

// SetClearcoatNormalMapUV sets which of the mesh's sets of texture coordinates the clear coat normal map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetClearcoatNormalMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatClearcoatNormalMap, set)
	m.dirty()
}

// SheenColor is the colour of the sheen over the surface (see SetSheenColor).
func (m *PBRMaterial) SheenColor() colors.RGB32F {
	return m.sheenColor
}

// SetSheenColor gives the surface a sheen of the given colour: the soft glow at grazing
// angles of cloth and velvet, whose fibres catch the light edge-on. Black, the default,
// is none (KHR_materials_sheen's sheenColorFactor). What the sheen reflects does not
// reach the surface under it.
//
// Shading the sheen is a further reflection for every light, which the material's
// pipeline compiles in only while some material drawn with it has one (see
// FeatureSheen).
func (m *PBRMaterial) SetSheenColor(color colors.RGB32F) {
	m.sheenColor = color
	m.dirty()
}

// SheenRoughness is how far the sheen spreads (see SetSheenRoughness).
func (m *PBRMaterial) SheenRoughness() float32 {
	return m.sheenRoughness
}

// SetSheenRoughness sets how far the sheen spreads from the grazing angles: 0, the
// default, a thin rim, 1 a soft glow over most of the surface
// (KHR_materials_sheen's sheenRoughnessFactor).
func (m *PBRMaterial) SetSheenRoughness(roughness float32) {
	m.sheenRoughness = roughness
	m.dirty()
}

// SheenColorMap returns the bound sheen colour map.
func (m *PBRMaterial) SheenColorMap() textures.Texture {
	return m.sheenColorMap
}

// SetSheenColorMap binds a map, in sRGB colour, that multiplies SheenColor per texel (glTF's
// sheenColorTexture). The material takes its own reference.
func (m *PBRMaterial) SetSheenColorMap(texture textures.Texture) {
	newRef := texture.Copy()
	m.sheenColorMap.Release()
	m.sheenColorMap = newRef
	m.dirty()
}

// SetSheenColorMapSampler sets the heap index of the sampler used for the sheen colour map.
func (m *PBRMaterial) SetSheenColorMapSampler(sampler uint32) {
	m.sheenColorSampler = sampler
	m.dirty()
}

// SheenColorMapUV is the set of texture coordinates the sheen colour map reads.
func (m *PBRMaterial) SheenColorMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatSheenColorMap)
}

// SetSheenColorMapUV sets which of the mesh's sets of texture coordinates the sheen colour map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetSheenColorMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatSheenColorMap, set)
	m.dirty()
}

// SheenRoughnessMap returns the bound sheen roughness map.
func (m *PBRMaterial) SheenRoughnessMap() textures.Texture {
	return m.sheenRoughnessMap
}

// SetSheenRoughnessMap binds a map whose ALPHA channel scales SheenRoughness per texel (glTF's
// sheenRoughnessTexture). The material takes its own reference.
func (m *PBRMaterial) SetSheenRoughnessMap(texture textures.Texture) {
	newRef := texture.Copy()
	m.sheenRoughnessMap.Release()
	m.sheenRoughnessMap = newRef
	m.dirty()
}

// SetSheenRoughnessMapSampler sets the heap index of the sampler used for the sheen roughness map.
func (m *PBRMaterial) SetSheenRoughnessMapSampler(sampler uint32) {
	m.sheenRoughnessSampler = sampler
	m.dirty()
}

// SheenRoughnessMapUV is the set of texture coordinates the sheen roughness map reads.
func (m *PBRMaterial) SheenRoughnessMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatSheenRoughnessMap)
}

// SetSheenRoughnessMapUV sets which of the mesh's sets of texture coordinates the sheen roughness map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetSheenRoughnessMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatSheenRoughnessMap, set)
	m.dirty()
}

// Anisotropy is how much the surface's highlights stretch along one direction (see
// SetAnisotropy).
func (m *PBRMaterial) Anisotropy() float32 {
	return m.anisotropy
}

// SetAnisotropy stretches the surface's highlights and reflections along one direction
// across it, as brushed metal's stretch across its brushing and hair's along its
// strands: 0, the default, not at all, 1 as far as they go. They stretch along the
// texture's u direction, turned by AnisotropyRotation (KHR_materials_anisotropy's
// anisotropyStrength).
//
// The stretched reflection is a costlier one for every light, which the material's
// pipeline compiles in only while some material drawn with it has one (see
// FeatureAnisotropy).
func (m *PBRMaterial) SetAnisotropy(anisotropy float32) {
	m.anisotropy = anisotropy
	m.dirty()
}

// AnisotropyRotation is the angle, in radians, from the texture's u direction to the
// one highlights stretch along (see SetAnisotropyRotation).
func (m *PBRMaterial) AnisotropyRotation() float32 {
	return m.anisotropyRotation
}

// SetAnisotropyRotation turns the direction highlights stretch along from the texture's
// u direction toward its v direction by rotation radians (KHR_materials_anisotropy's
// anisotropyRotation).
func (m *PBRMaterial) SetAnisotropyRotation(rotation float32) {
	m.anisotropyRotation = rotation
	m.dirty()
}

// AnisotropyMap returns the bound anisotropy map.
func (m *PBRMaterial) AnisotropyMap() textures.Texture {
	return m.anisotropyMap
}

// SetAnisotropyMap binds a map of the direction highlights stretch along, texel by
// texel, in its RED and GREEN channels — a direction in the texture's own u and v,
// mapped from [-1, 1] to [0, 1], which AnisotropyRotation then turns — and in its BLUE
// channel what scales Anisotropy (glTF's anisotropyTexture). Upload it as linear data.
// The material takes its own reference.
func (m *PBRMaterial) SetAnisotropyMap(texture textures.Texture) {
	newRef := texture.Copy()
	m.anisotropyMap.Release()
	m.anisotropyMap = newRef
	m.dirty()
}

// SetAnisotropyMapSampler sets the heap index of the sampler used for the anisotropy
// map.
func (m *PBRMaterial) SetAnisotropyMapSampler(sampler uint32) {
	m.anisotropySampler = sampler
	m.dirty()
}

// AnisotropyMapUV is the set of texture coordinates the anisotropy map reads.
func (m *PBRMaterial) AnisotropyMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatAnisotropyMap)
}

// SetAnisotropyMapUV sets which of the mesh's sets of texture coordinates the anisotropy map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetAnisotropyMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatAnisotropyMap, set)
	m.dirty()
}

// Features implements Featured: the optional parts of the PBR shader this material
// uses.
func (m *PBRMaterial) Features() Features {
	var features Features
	if m.clearcoat > 0 {
		features |= FeatureClearcoat
	}
	if m.sheenColor != (colors.RGB32F{}) {
		features |= FeatureSheen
	}
	if m.anisotropy > 0 {
		features |= FeatureAnisotropy
	}
	return features
}

// The bound textures (or a zero handle).
func (m *PBRMaterial) ColorMap() textures.Texture {
	return m.colorMap
}

// SetColorMap binds the base-color (albedo) map; pass a zero textures.Texture to clear it. The
// material takes its own reference, so the caller may release theirs.
func (m *PBRMaterial) SetColorMap(texture textures.Texture) {
	old := m.colorMap
	m.colorMap = texture.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

// Per-map sampler getters/setters (bindless sampler heap indices).
func (m *PBRMaterial) ColorMapSampler() uint32 {
	return m.colorSampler
}

func (m *PBRMaterial) SetColorMapSampler(sampler uint32) {
	m.colorSampler = sampler
	m.dirty()
}

// ColorMapUV is the set of texture coordinates the colour map reads.
func (m *PBRMaterial) ColorMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatColorMap)
}

// SetColorMapUV sets which of the mesh's sets of texture coordinates the colour map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetColorMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatColorMap, set)
	m.dirty()
}

func (m *PBRMaterial) NormalMap() textures.Texture {
	return m.normalMap
}

// SetNormalMap binds a tangent-space normal map (linear RGB, xyz in [0,1]).
func (m *PBRMaterial) SetNormalMap(texture textures.Texture) {
	old := m.normalMap
	m.normalMap = texture.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

func (m *PBRMaterial) NormalMapSampler() uint32 {
	return m.normalSampler
}

func (m *PBRMaterial) SetNormalMapSampler(sampler uint32) {
	m.normalSampler = sampler
	m.dirty()
}

// NormalMapUV is the set of texture coordinates the normal map reads.
func (m *PBRMaterial) NormalMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatNormalMap)
}

// SetNormalMapUV sets which of the mesh's sets of texture coordinates the normal map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetNormalMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatNormalMap, set)
	m.dirty()
}

func (m *PBRMaterial) MetallicMap() textures.Texture {
	return m.metallicMap
}

// SetMetallicMap binds a metallic map; its blue channel modulates the metallic factor
// (matches the glTF metallic-roughness texture convention, and works for grayscale).
func (m *PBRMaterial) SetMetallicMap(texture textures.Texture) {
	old := m.metallicMap
	m.metallicMap = texture.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

func (m *PBRMaterial) MetallicMapSampler() uint32 {
	return m.metallicSampler
}

func (m *PBRMaterial) SetMetallicMapSampler(sampler uint32) {
	m.metallicSampler = sampler
	m.dirty()
}

// MetallicMapUV is the set of texture coordinates the metallic map reads.
func (m *PBRMaterial) MetallicMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatMetalMap)
}

// SetMetallicMapUV sets which of the mesh's sets of texture coordinates the metallic map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetMetallicMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatMetalMap, set)
	m.dirty()
}

func (m *PBRMaterial) RoughnessMap() textures.Texture {
	return m.roughnessMap
}

// SetRoughnessMap binds a roughness map; its green channel modulates the roughness
// factor (glTF convention; works for grayscale too).
func (m *PBRMaterial) SetRoughnessMap(texture textures.Texture) {
	old := m.roughnessMap
	m.roughnessMap = texture.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

func (m *PBRMaterial) RoughnessMapSampler() uint32 {
	return m.roughnessSampler
}

func (m *PBRMaterial) SetRoughnessMapSampler(sampler uint32) {
	m.roughnessSampler = sampler
	m.dirty()
}

// RoughnessMapUV is the set of texture coordinates the roughness map reads.
func (m *PBRMaterial) RoughnessMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatRoughMap)
}

// SetRoughnessMapUV sets which of the mesh's sets of texture coordinates the roughness map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetRoughnessMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatRoughMap, set)
	m.dirty()
}

// TransmissionMap returns the bound transmission map.
func (m *PBRMaterial) TransmissionMap() textures.Texture {
	return m.transmissionMap
}

// SetTransmissionMap binds a per-texel transmission mask, multiplied with the scalar
// Transmission (glTF KHR_materials_transmission keeps it in the red channel). Use it
// when only part of a surface is glass — a cabinet's panes, a sign's window — since
// the scalar alone makes the whole object see-through.
func (m *PBRMaterial) SetTransmissionMap(texture textures.Texture) {
	newRef := texture.Copy()
	m.transmissionMap.Release()
	m.transmissionMap = newRef
	m.dirty()
}

// SetTransmissionMapSampler sets the heap index of the sampler used for the
// transmission map.
func (m *PBRMaterial) SetTransmissionMapSampler(sampler uint32) {
	m.transmissionSampler = sampler
	m.dirty()
}

// TransmissionMapUV is the set of texture coordinates the transmission map reads.
func (m *PBRMaterial) TransmissionMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatTransMap)
}

// SetTransmissionMapUV sets which of the mesh's sets of texture coordinates the transmission map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetTransmissionMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatTransMap, set)
	m.dirty()
}

// OcclusionMap returns the bound occlusion map.
func (m *PBRMaterial) OcclusionMap() textures.Texture {
	return m.occlusionMap
}

// SetOcclusionMap binds a map of how much of the light around it reaches each texel, in
// its red channel (glTF's occlusionTexture): baked occlusion, which darkens ambient and
// environment light and leaves direct light alone. Packed with roughness and metalness,
// as glTF's occlusion-roughness-metallic textures are, the same texture can be bound to
// all three maps.
func (m *PBRMaterial) SetOcclusionMap(texture textures.Texture) {
	old := m.occlusionMap
	m.occlusionMap = texture.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

func (m *PBRMaterial) OcclusionMapSampler() uint32 {
	return m.occlusionSampler
}

func (m *PBRMaterial) SetOcclusionMapSampler(sampler uint32) {
	m.occlusionSampler = sampler
	m.dirty()
}

// OcclusionMapUV is the set of texture coordinates the occlusion map reads.
func (m *PBRMaterial) OcclusionMapUV() UVSet {
	return uvSetOf(m.uv2Maps, MatOcclusionMap)
}

// SetOcclusionMapUV sets which of the mesh's sets of texture coordinates the occlusion map reads:
// UV1, the default, or UV2.
func (m *PBRMaterial) SetOcclusionMapUV(set UVSet) {
	m.uv2Maps = withUVSet(m.uv2Maps, MatOcclusionMap, set)
	m.dirty()
}

// OcclusionStrength is how far the occlusion map darkens: 0 not at all, 1 fully, the
// default (glTF's occlusionTexture.strength).
func (m *PBRMaterial) OcclusionStrength() float32 {
	return m.occlusionStrength
}

func (m *PBRMaterial) SetOcclusionStrength(strength float32) {
	m.occlusionStrength = strength
	m.dirty()
}

// UVTransform is how the material moves the mesh's set of texture coordinates set
// before the maps reading it sample them (see UVTransform).
func (m *PBRMaterial) UVTransform(set UVSet) UVTransform {
	return m.uvTransforms[set]
}

// SetUVTransform sets how the material moves the mesh's set of texture coordinates set
// before the maps reading it sample them, the alpha its surface is cut out by among
// them. NoUVTransform leaves them as they are.
func (m *PBRMaterial) SetUVTransform(set UVSet, transform UVTransform) {
	m.uvTransforms[set] = transform
	m.dirty()
}

// AlphaCutoff is the alpha below which the material has no surface; 0, the default,
// keeps all of it.
func (m *PBRMaterial) AlphaCutoff() float32 {
	return m.alphaCutoff
}

// SetAlphaCutoff makes the material masked: where its colour's alpha — the colour map's
// times the colour's own — falls below cutoff, there is no surface, in shading, shadows
// and depth alike (glTF's alphaMode MASK, whose alphaCutoff defaults to 0.5). 0 makes it
// whole again.
func (m *PBRMaterial) SetAlphaCutoff(cutoff float32) {
	m.alphaCutoff = cutoff
	m.dirty()
}

// IgnoresAlpha reports whether the material ignores its colour's alpha (see
// SetIgnoresAlpha).
func (m *PBRMaterial) IgnoresAlpha() bool {
	return m.ignoresAlpha
}

// SetIgnoresAlpha makes the surface whole wherever it is drawn, whatever its colour's
// alpha — the colour map's times the colour's own: glTF's alphaMode OPAQUE, whose alpha
// "is ignored". Otherwise alpha is how much of the surface is there, which a blended
// material and glass are drawn by. A material with an alpha cut-off still cuts its
// surface out by alpha (see SetAlphaCutoff), and is whole where it does not.
func (m *PBRMaterial) SetIgnoresAlpha(ignores bool) {
	m.ignoresAlpha = ignores
	m.dirty()
}

// ignoresAlphaFlag is MatIgnoresAlpha when ignores is set, and no flag otherwise.
func ignoresAlphaFlag(ignores bool) uint32 {
	if ignores {
		return MatIgnoresAlpha
	}
	return 0
}

// AlphaMask implements Masked: the material cuts its surface out by its colour's alpha
// once it has a cut-off.
func (m *PBRMaterial) AlphaMask() AlphaMask {
	return AlphaMask{Map: m.colorMap, Sampler: m.colorSampler, UV: m.ColorMapUV(), Alpha: m.color[3], Cutoff: m.alphaCutoff}
}

// TransmissionMask implements Transmissive: the surface lets light through wherever its
// transmission, times its transmission map, reaches half, and casts no shadow there.
func (m *PBRMaterial) TransmissionMask() TransmissionMask {
	return TransmissionMask{Map: m.transmissionMap, Sampler: m.transmissionSampler, UV: m.TransmissionMapUV(), Factor: m.transmission}
}

// --- Material ---
//
// PBRMaterial supplies all three passes, so it renders through the G-buffer
// whenever deferred rendering is enabled and it is unblended.
// Every method below is a plain store lookup. They are spelled out here, rather than
// inherited from a shared base, so that this file is the whole of PBRMaterial.

// Copy returns another handle to the same instance (refcount++). The material is its
// own record, so there is exactly one PBRMaterial per instance and this returns the
// same pointer — every handle observes the same fields and the same bound textures.
func (m *PBRMaterial) Copy() Material {
	m.ref.Copy() // bumps the shared count; the ref.Ref it returns is identical to m.ref
	return m
}

// Release drops this handle's reference. At refcount 0 the store disposes the slot,
// which releases the textures the material bound.
func (m *PBRMaterial) Release() {
	m.ref.Release()
}

// Valid reports whether the underlying instance is still alive.
func (m *PBRMaterial) IsValid() bool {
	return m.ref.IsValid()
}

// Cull reports which triangle faces are discarded.
func (m *PBRMaterial) Cull() CullMode {
	return m.pool.CullAt(m.ref.ID())
}

// SetCull sets which faces are culled (CullNone = double-sided).
func (m *PBRMaterial) SetCull(mode CullMode) {
	m.pool.SetCullAt(m.ref.ID(), mode)
}

// SetDoubleSided is a convenience for SetCull(CullNone) / SetCull(CullBack).
func (m *PBRMaterial) SetDoubleSided(enabled bool) {
	if enabled {
		m.SetCull(CullNone)
	} else {
		m.SetCull(CullBack)
	}
}

// Blend reports the material's blend mode. Anything but BlendOpaque also pins the
// material to the forward path — the G-buffer holds one surface per pixel, so it
// cannot represent a fragment that composites over what is behind it.
func (m *PBRMaterial) Blend() BlendMode {
	return m.pool.BlendAt(m.ref.ID())
}

// SetBlend sets the material's blend mode (Opaque/Alpha/Additive/Premultiplied).
func (m *PBRMaterial) SetBlend(mode BlendMode) {
	m.pool.SetBlendAt(m.ref.ID(), mode)
}

// Pool returns the pool this material's records live in — its shader, pipeline
// identity and record buffer.
func (m *PBRMaterial) Pool() *Pool {
	return m.pool
}

// ID is the instance's index within its store; RecordsAddr is the store's
// record buffer address, resolved at draw time because it moves when the store grows.
func (m *PBRMaterial) ID() ID {
	return m.pool.IDOf(m.ref)
}
