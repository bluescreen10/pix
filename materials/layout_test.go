package materials_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/colors"
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

	tex := texStore.Create([]byte{255, 255, 255, 255}, 1, 1, textures.Linear)
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

		b := m.Bytes()
		if len(b) != 88 {
			t.Fatalf("record is %d bytes, shader expects 88", len(b))
		}
		checks := []struct {
			name string
			off  int
			want float32
		}{
			{"color.r", 0, 0.1}, {"color.a", 12, 0.4},
			{"emissive.r", 16, 0.5}, {"emissive.b", 24, 0.7},
			{"metallic", 32, 0.25}, {"roughness", 36, 0.75}, {"transmission", 40, 0.5},
		}
		for _, c := range checks {
			if got := f32At(t, b, c.off); got != c.want {
				t.Errorf("%s at byte %d = %v, want %v", c.name, c.off, got, c.want)
			}
		}
		if want := materials.MatNormalMap | materials.MatTransMap; u32At(t, b, 44) != want {
			t.Errorf("flags at byte 44 = %#x, want MatNormalMap|MatTransMap (%#x)", u32At(t, b, 44), want)
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
	})

	t.Run("Basic", func(t *testing.T) {
		m := materials.NewBasicMaterial(store)
		defer m.Release()
		m.SetColor(colors.RGBA32F{0.1, 0.2, 0.3, 0.4})
		m.SetEmissive(colors.RGB32F{0.5, 0.6, 0.7})
		m.SetColorMap(tex)
		m.SetColorMapSampler(3)

		b := m.Bytes()
		if len(b) != 48 {
			t.Fatalf("record is %d bytes, shader expects 48", len(b))
		}
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
		m.SetSpecular(0.25)
		m.SetShininess(64)

		b := m.Bytes()
		if len(b) != 64 {
			t.Fatalf("record is %d bytes, shader expects 64", len(b))
		}
		if got := f32At(t, b, 0); got != 0.1 {
			t.Errorf("color.r at byte 0 = %v, want 0.1", got)
		}
		if got := f32At(t, b, 16); got != 0.5 {
			t.Errorf("emissive.r at byte 16 = %v, want 0.5", got)
		}
		if got := f32At(t, b, 32); got != 0.25 {
			t.Errorf("specular at byte 32 = %v, want 0.25", got)
		}
		if got := f32At(t, b, 36); got != 64 {
			t.Errorf("shininess at byte 36 = %v, want 64", got)
		}
		if got := u32At(t, b, 40); got != materials.NoTextureIndex {
			t.Errorf("unbound colorMap at byte 40 = %d, want the no-texture sentinel", got)
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
