package materials_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/textures"
)

func u32At(t *testing.T, b []byte, off int) uint32 {
	t.Helper()
	return binary.LittleEndian.Uint32(b[off : off+4])
}

func f32At(t *testing.T, b []byte, off int) float32 {
	t.Helper()
	return math.Float32frombits(binary.LittleEndian.Uint32(b[off : off+4]))
}

// countingUploader counts Copy calls, to observe whether a Sync actually re-uploaded
// anything without reaching into the store's dirty-tracking fields.
type countingUploader struct {
	calls int
}

func (u *countingUploader) Copy(dst gpu.Buffer, dstOffset uint32, data []byte) {
	u.calls++
}

// TestMaterialRecordLayouts pins each material's GPU record byte-for-byte against the
// struct its shader declares. The layout now lives in an anonymous struct inside
// Bytes(), so nothing else would catch a reordered or repadded field — and the failure
// mode is not a crash but every material in the scene reading the wrong values.
//
// Offsets below must match:
//
//	PBR         shaders/src/scene_pbr.frag.glsl
//	Basic       shaders/src/scene_basic.frag.glsl
//	BlinnPhong  shaders/src/scene_lit.frag.glsl
func TestMaterialRecordLayouts(t *testing.T) {
	store, backend := testStore(t)
	texStore := textures.NewStore(backend)
	defer texStore.Destroy()

	tex := texStore.Create(nrgbaImage([]byte{255, 255, 255, 255}, 1, 1), textures.Linear)
	defer tex.Release()

	t.Run("PBR", func(t *testing.T) {
		m := materials.NewPBRMaterial(store)
		defer m.Release()
		m.SetColor(colors.RGBA32F{0.1, 0.2, 0.3, 0.4})
		m.SetEmissive(colors.RGB32F{0.5, 0.6, 0.7})
		m.SetMetallic(0.25)
		m.SetRoughness(0.75)
		m.SetTransmission(0.5)
		m.SetNormalMap(tex)
		m.SetNormalMapSampler(7)
		m.SetTransmissionMap(tex)
		m.SetTransmissionMapSampler(9)
		m.SetOcclusionMap(tex)
		m.SetOcclusionMapSampler(11)
		m.SetOcclusionStrength(0.6)
		m.SetIOR(1.33)
		m.SetThickness(0.2)
		m.SetAttenuation(colors.RGB32F{0.8, 0.9, 1}, 3)
		m.SetEmissiveMap(tex)
		m.SetEmissiveMapSampler(13)
		m.SetColorMapUV(materials.UV2)
		m.SetUVTransform(materials.UV1, materials.UVTransform{Scale: glm.Vec2f{2, 3}, Offset: glm.Vec2f{0.25, 0.5}})
		m.SetUVTransform(materials.UV2, materials.UVTransform{Scale: glm.Vec2f{4, 5}, Offset: glm.Vec2f{0.75, 1}})
		m.SetSpecularIntensity(0.4)
		m.SetSpecularColor(colors.RGB32F{0.1, 0.2, 0.3})
		m.SetSpecularIntensityMap(tex)
		m.SetSpecularIntensityMapSampler(15)
		m.SetSpecularColorMap(tex)
		m.SetSpecularColorMapSampler(17)
		m.SetClearcoat(0.7)
		m.SetClearcoatRoughness(0.2)
		m.SetClearcoatMap(tex)
		m.SetClearcoatMapSampler(19)
		m.SetClearcoatRoughnessMap(tex)
		m.SetClearcoatRoughnessMapSampler(21)
		m.SetClearcoatNormalMap(tex)
		m.SetClearcoatNormalMapSampler(23)
		m.SetSheenColor(colors.RGB32F{0.3, 0.4, 0.5})
		m.SetSheenRoughness(0.6)
		m.SetSheenColorMap(tex)
		m.SetSheenColorMapSampler(25)
		m.SetSheenRoughnessMap(tex)
		m.SetSheenRoughnessMapSampler(27)
		m.SetAnisotropy(0.8)
		m.SetAnisotropyRotation(math.Pi / 2)
		m.SetAnisotropyMap(tex)
		m.SetAnisotropyMapSampler(29)

		b := m.Bytes()
		if len(b) != 284 {
			t.Fatalf("record is %d bytes, shader expects 284", len(b))
		}
		checkUVTransformsAt(t, b, 252)
		if got := f32At(t, b, 232); got != 0.8 {
			t.Errorf("anisotropy at byte 232 = %v, want 0.8", got)
		}
		// The rotation is stored as its cosine and sine: a quarter turn is (0, 1).
		if cos, sin := f32At(t, b, 236), f32At(t, b, 240); math.Abs(float64(cos)) > 1e-6 || sin != 1 {
			t.Errorf("anisotropy direction at byte 236 = (%v, %v), want (0, 1)", cos, sin)
		}
		if got := u32At(t, b, 244); got != tex.Index() {
			t.Errorf("anisotropyMap at byte 244 = %d, want %d", got, tex.Index())
		}
		if got := u32At(t, b, 248); got != 29 {
			t.Errorf("anisotropySampler at byte 248 = %d, want 29", got)
		}
		for i, want := range []float32{0.3, 0.4, 0.5, 0.6} {
			if got := f32At(t, b, 200+4*i); got != want {
				t.Errorf("sheen colour or roughness at byte %d = %v, want %v", 200+4*i, got, want)
			}
		}
		for i, want := range []uint32{tex.Index(), 25, tex.Index(), 27} {
			if got := u32At(t, b, 216+4*i); got != want {
				t.Errorf("sheen map or sampler at byte %d = %d, want %d", 216+4*i, got, want)
			}
		}
		if got := f32At(t, b, 168); got != 0.7 {
			t.Errorf("clearcoat at byte 168 = %v, want 0.7", got)
		}
		if got := f32At(t, b, 172); got != 0.2 {
			t.Errorf("clearcoatRoughness at byte 172 = %v, want 0.2", got)
		}
		for i, want := range []uint32{tex.Index(), 19, tex.Index(), 21, tex.Index(), 23} {
			if got := u32At(t, b, 176+4*i); got != want {
				t.Errorf("clear coat map or sampler at byte %d = %d, want %d", 176+4*i, got, want)
			}
		}
		checkUV2MapsAt(t, b, 132, materials.MatColorMap)
		// What the surface reflects head-on, by its ior of 1.33 tinted by its specular
		// colour and scaled by its intensity, then at a grazing angle, its intensity.
		ratio := (1.33 - 1) / (1.33 + 1)
		for i, want := range []float64{ratio * ratio * 0.1 * 0.4, ratio * ratio * 0.2 * 0.4, ratio * ratio * 0.3 * 0.4, 0.4} {
			if got := f32At(t, b, 136+4*i); math.Abs(float64(got)-want) > 1e-6 {
				t.Errorf("dielectric specular at byte %d = %v, want %v", 136+4*i, got, want)
			}
		}
		if got := u32At(t, b, 152); got != tex.Index() {
			t.Errorf("specularIntensityMap at byte 152 = %d, want %d", got, tex.Index())
		}
		if got := u32At(t, b, 156); got != 15 {
			t.Errorf("specularIntensitySampler at byte 156 = %d, want 15", got)
		}
		if got := u32At(t, b, 160); got != tex.Index() {
			t.Errorf("specularColorMap at byte 160 = %d, want %d", got, tex.Index())
		}
		if got := u32At(t, b, 164); got != 17 {
			t.Errorf("specularColorSampler at byte 164 = %d, want 17", got)
		}
		checks := []struct {
			name string
			off  int
			want float32
		}{
			{"color.r", 0, 0.1}, {"color.a", 12, 0.4},
			{"emissive.r", 16, 0.5}, {"emissive.b", 24, 0.7},
			{"metallic", 32, 0.25}, {"roughness", 36, 0.75}, {"transmission", 40, 0.5},
			{"occlusionStrength", 88, 0.6},
			{"ior", 100, 1.33}, {"thickness", 104, 0.2}, {"attenuationDistance", 108, 3},
			{"attenuationColor.r", 112, 0.8}, {"attenuationColor.b", 120, 1},
		}
		for _, c := range checks {
			if got := f32At(t, b, c.off); got != c.want {
				t.Errorf("%s at byte %d = %v, want %v", c.name, c.off, got, c.want)
			}
		}
		if want := materials.MatNormalMap | materials.MatTransMap | materials.MatOcclusionMap | materials.MatEmissiveMap |
			materials.MatSpecularIntensityMap | materials.MatSpecularColorMap |
			materials.MatClearcoatMap | materials.MatClearcoatRoughnessMap | materials.MatClearcoatNormalMap |
			materials.MatSheenColorMap | materials.MatSheenRoughnessMap | materials.MatAnisotropyMap; u32At(t, b, 44) != want {
			t.Errorf("flags at byte 44 = %#x, want every map's but the colour, metallic and roughness maps' (%#x)", u32At(t, b, 44), want)
		}
		if got := u32At(t, b, 124); got != tex.Index() {
			t.Errorf("emissiveMap index at byte 124 = %d, want %d", got, tex.Index())
		}
		if got := u32At(t, b, 128); got != 13 {
			t.Errorf("emissiveSampler at byte 128 = %d, want 13", got)
		}
		if got := u32At(t, b, 48); got != materials.NoTextureIndex {
			t.Errorf("unbound colorMap at byte 48 = %d, want the no-texture sentinel", got)
		}
		if got := u32At(t, b, 56); got != tex.Index() {
			t.Errorf("normalMap index at byte 56 = %d, want %d", got, tex.Index())
		}
		if got := u32At(t, b, 60); got != 7 {
			t.Errorf("normalSampler at byte 60 = %d, want 7", got)
		}
		if got := u32At(t, b, 80); got != tex.Index() {
			t.Errorf("transmissionMap index at byte 80 = %d, want %d", got, tex.Index())
		}
		if got := u32At(t, b, 84); got != 9 {
			t.Errorf("transmissionSampler at byte 84 = %d, want 9", got)
		}
		if got := u32At(t, b, 92); got != tex.Index() {
			t.Errorf("occlusionMap index at byte 92 = %d, want %d", got, tex.Index())
		}
		if got := u32At(t, b, 96); got != 11 {
			t.Errorf("occlusionSampler at byte 96 = %d, want 11", got)
		}
	})

	t.Run("Basic", func(t *testing.T) {
		m := materials.NewBasicMaterial(store)
		defer m.Release()
		m.SetColor(colors.RGBA32F{0.1, 0.2, 0.3, 0.4})
		m.SetEmissive(colors.RGB32F{0.5, 0.6, 0.7})
		m.SetColorMap(tex)
		m.SetColorMapSampler(3)
		m.SetColorMapUV(materials.UV2)
		m.SetUVTransform(materials.UV1, materials.UVTransform{Scale: glm.Vec2f{2, 3}, Offset: glm.Vec2f{0.25, 0.5}})
		m.SetUVTransform(materials.UV2, materials.UVTransform{Scale: glm.Vec2f{4, 5}, Offset: glm.Vec2f{0.75, 1}})

		b := m.Bytes()
		if len(b) != 80 {
			t.Fatalf("record is %d bytes, shader expects 80", len(b))
		}
		checkUVTransformsAt(t, b, 48)
		checkUV2MapsAt(t, b, 44, materials.MatColorMap)
		if got := f32At(t, b, 0); got != 0.1 {
			t.Errorf("color.r at byte 0 = %v, want 0.1", got)
		}
		if got := f32At(t, b, 16); got != 0.5 {
			t.Errorf("emissive.r at byte 16 = %v, want 0.5", got)
		}
		if got := u32At(t, b, 32); got != tex.Index() {
			t.Errorf("colorMap at byte 32 = %d, want %d", got, tex.Index())
		}
		if got := u32At(t, b, 36); got != 3 {
			t.Errorf("colorSampler at byte 36 = %d, want 3", got)
		}
		if got := u32At(t, b, 40); got != materials.MatColorMap {
			t.Errorf("flags at byte 40 = %#x, want MatColorMap (%#x)", got, materials.MatColorMap)
		}
	})

	t.Run("BlinnPhong", func(t *testing.T) {
		m := materials.NewBlinnPhongMaterial(store)
		defer m.Release()
		m.SetColor(colors.RGBA32F{0.1, 0.2, 0.3, 0.4})
		m.SetEmissive(colors.RGB32F{0.5, 0.6, 0.7})
		m.SetSpecular(colors.RGB32F{0.25, 0.35, 0.45})
		m.SetShininess(64)
		m.SetColorMapUV(materials.UV2)
		m.SetUVTransform(materials.UV1, materials.UVTransform{Scale: glm.Vec2f{2, 3}, Offset: glm.Vec2f{0.25, 0.5}})
		m.SetUVTransform(materials.UV2, materials.UVTransform{Scale: glm.Vec2f{4, 5}, Offset: glm.Vec2f{0.75, 1}})

		b := m.Bytes()
		if len(b) != 96 {
			t.Fatalf("record is %d bytes, shader expects 96", len(b))
		}
		checkUVTransformsAt(t, b, 64)
		checkUV2MapsAt(t, b, 56, materials.MatColorMap)
		if got := f32At(t, b, 0); got != 0.1 {
			t.Errorf("color.r at byte 0 = %v, want 0.1", got)
		}
		if got := f32At(t, b, 16); got != 0.5 {
			t.Errorf("emissive.r at byte 16 = %v, want 0.5", got)
		}
		if got := f32At(t, b, 28); got != 64 {
			t.Errorf("shininess at byte 28 = %v, want 64", got)
		}
		if got := f32At(t, b, 32); got != 0.25 {
			t.Errorf("specular.r at byte 32 = %v, want 0.25", got)
		}
		if got := f32At(t, b, 40); got != 0.45 {
			t.Errorf("specular.b at byte 40 = %v, want 0.45", got)
		}
		if got := u32At(t, b, 44); got != materials.NoTextureIndex {
			t.Errorf("unbound colorMap at byte 44 = %d, want the no-texture sentinel", got)
		}
	})
}

// TestMaterialBytesHasNoSideEffects: Sync calls Bytes on every dirty material, so if
// Bytes marked the record dirty (as an earlier RawMaterial.Bytes did) every material
// would re-upload every frame forever. Observed through Sync's actual upload count
// rather than the pool's internal dirty set: create a material (which dirties and
// syncs it once), call Bytes on its own, then sync again — a second upload here means
// Bytes had a side effect.
func TestMaterialBytesHasNoSideEffects(t *testing.T) {
	store, _ := testStore(t)

	m := materials.NewPBRMaterial(store)
	defer m.Release()

	u := &countingUploader{}
	store.Sync(u) // uploads the record created above
	u.calls = 0

	_ = m.Bytes()
	store.Sync(u)
	if u.calls != 0 {
		t.Fatalf("Bytes() dirtied the material: Sync issued %d uploads, want 0", u.calls)
	}
}

// TestMaterialCopyIsTheSameInstance: the material *is* its record, so Copy must return
// the same object. Two objects sharing one slot would mean Sync serializes whichever
// one the store happens to hold, silently dropping edits made through the other.
func TestMaterialCopyIsTheSameInstance(t *testing.T) {
	store, _ := testStore(t)

	m := materials.NewPBRMaterial(store)
	dup, ok := m.Copy().(*materials.PBRMaterial)
	if !ok || dup != m {
		t.Fatal("Copy returned a different object; edits through one handle would be lost")
	}

	// Refcounting still works: two handles, so one Release must not dispose.
	m.Release()
	if !dup.IsValid() {
		t.Fatal("releasing one of two handles disposed the instance")
	}
	dup.Release()
	if dup.IsValid() {
		t.Fatal("instance outlived its last handle")
	}
}

// TestMapsReadWithTheStoresDefaultSampler: a map bound without a sampler of its own reads
// with the store's default one — not with heap index 0, which need not be meant for
// colour — and one set explicitly replaces it.
func TestMapsReadWithTheStoresDefaultSampler(t *testing.T) {
	backend := gpu.Instance(nil)
	if err := backend.Init(); err != nil {
		t.Fatal(err)
	}
	const defaultSampler = 5
	store := materials.NewStore(backend, defaultSampler)
	defer store.Destroy()

	m := materials.NewPBRMaterial(store)
	defer m.Release()
	samplers := map[string]uint32{
		"color": m.ColorMapSampler(), "normal": m.NormalMapSampler(), "metallic": m.MetallicMapSampler(),
		"roughness": m.RoughnessMapSampler(), "occlusion": m.OcclusionMapSampler(),
	}
	for name, got := range samplers {
		if got != defaultSampler {
			t.Errorf("PBR %s map sampler = %d, want the store's default %d", name, got, defaultSampler)
		}
	}
	m.SetOcclusionMapSampler(9)
	if got := m.OcclusionMapSampler(); got != 9 {
		t.Errorf("OcclusionMapSampler() = %d after SetOcclusionMapSampler(9), want 9", got)
	}

	basic := materials.NewBasicMaterial(store)
	defer basic.Release()
	if got := basic.ColorMapSampler(); got != defaultSampler {
		t.Errorf("Basic color map sampler = %d, want the store's default %d", got, defaultSampler)
	}
	lit := materials.NewBlinnPhongMaterial(store)
	defer lit.Release()
	if got := lit.ColorMapSampler(); got != defaultSampler {
		t.Errorf("BlinnPhong color map sampler = %d, want the store's default %d", got, defaultSampler)
	}
}

// TestAlphaCutoffMakesAMaterialMasked: a built-in material is masked once it has a cut-off
// and whole again without one, with the mask its colour map and colour describe; and
// becoming masked, or whole, is a change of rasterization state.
func TestAlphaCutoffMakesAMaterialMasked(t *testing.T) {
	store, backend := testStore(t)
	texStore := textures.NewStore(backend)
	defer texStore.Destroy()
	tex := texStore.Create(nrgbaImage([]byte{255, 255, 255, 128}, 1, 1), textures.SRGB)
	defer tex.Release()

	m := materials.NewPBRMaterial(store)
	defer m.Release()
	m.SetColorMap(tex)
	m.SetColor(colors.RGBA32F{1, 1, 1, 0.5})
	if m.AlphaMask().IsMasked() {
		t.Fatal("a new PBR material is masked, want it whole")
	}
	before := m.Pool().RasterRevision()
	m.SetAlphaCutoff(0.4)
	mask := m.AlphaMask()
	if !mask.IsMasked() || mask.Cutoff != 0.4 || mask.Alpha != 0.5 || mask.Map.Index() != tex.Index() {
		t.Errorf("AlphaMask() = %+v after SetAlphaCutoff(0.4); want masked by the colour map at cut-off 0.4, alpha 0.5", mask)
	}
	if !m.Pool().IsMaskedAt(m.ID().Slot) {
		t.Error("the pool does not see the material as masked")
	}
	if m.Pool().RasterRevision() == before {
		t.Error("becoming masked left the pool's raster revision as it was")
	}
	m.SetAlphaCutoff(0)
	if m.Pool().IsMaskedAt(m.ID().Slot) {
		t.Error("the pool still sees the material as masked after SetAlphaCutoff(0)")
	}
}

// checkUV2MapsAt checks that record holds, at byte off, the flags of the maps that read
// UV2: want, the layout tests' colour map.
func checkUV2MapsAt(t *testing.T, record []byte, off int, want uint32) {
	t.Helper()
	if got := u32At(t, record, off); got != want {
		t.Errorf("uv2Maps at byte %d = %#x, want %#x", off, got, want)
	}
}

// checkUVTransformsAt checks that record holds, at byte off, the UV transforms the layout
// tests set: UV1's scale (2, 3) and offset (0.25, 0.5), then UV2's (4, 5) and (0.75, 1).
func checkUVTransformsAt(t *testing.T, record []byte, off int) {
	t.Helper()
	want := [8]float32{2, 3, 0.25, 0.5, 4, 5, 0.75, 1}
	for i, w := range want {
		if got := f32At(t, record, off+4*i); got != w {
			t.Errorf("uvTransforms component %d at byte %d = %v, want %v", i, off+4*i, got, w)
		}
	}
}
