package pix

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/bluescreen10/pix/colors"
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
	r, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	tex := r.TextureStore.Create([]byte{255, 255, 255, 255}, 1, 1, textures.Linear)
	defer tex.Release()

	t.Run("PBR", func(t *testing.T) {
		m := r.NewPBRMaterial()
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
		if want := MatNormalMap | MatTransMap; u32At(t, b, 44) != want {
			t.Errorf("flags at byte 44 = %#x, want MatNormalMap|MatTransMap (%#x)", u32At(t, b, 44), want)
		}
		if got := u32At(t, b, 48); got != noTextureIndex {
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
		m := r.NewBasicMaterial()
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
		if got := u32At(t, b, 40); got != MatColorMap {
			t.Errorf("flags at byte 40 = %#x, want MatColorMap (%#x)", got, MatColorMap)
		}
	})

	t.Run("BlinnPhong", func(t *testing.T) {
		m := r.NewBlinnPhongMaterial()
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
		if got := u32At(t, b, 40); got != noTextureIndex {
			t.Errorf("unbound colorMap at byte 40 = %d, want the no-texture sentinel", got)
		}
	})
}

// TestMaterialBytesHasNoSideEffects: Sync calls Bytes on every dirty material, so if
// Bytes marked the record dirty (as an earlier RawMaterial.Bytes did) every material
// would re-upload every frame forever.
func TestMaterialBytesHasNoSideEffects(t *testing.T) {
	r, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	m := r.NewPBRMaterial()
	defer m.Release()
	st := m.store
	clear(st.dirty)
	st.allDirty = false

	_ = m.Bytes()
	if len(st.dirty) != 0 || st.allDirty {
		t.Fatalf("Bytes() dirtied the store: dirty=%v allDirty=%v", st.dirty, st.allDirty)
	}
}

// TestMaterialCopyIsTheSameInstance: the material *is* its record, so Copy must return
// the same object. Two objects sharing one slot would mean Sync serializes whichever
// one the store happens to hold, silently dropping edits made through the other.
func TestMaterialCopyIsTheSameInstance(t *testing.T) {
	r, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	m := r.NewPBRMaterial()
	dup, ok := m.Copy().(*PBRMaterial)
	if !ok || dup != m {
		t.Fatal("Copy returned a different object; edits through one handle would be lost")
	}

	// Refcounting still works: two handles, so one Release must not dispose.
	m.Release()
	if !dup.Valid() {
		t.Fatal("releasing one of two handles disposed the instance")
	}
	dup.Release()
	if dup.Valid() {
		t.Fatal("instance outlived its last handle")
	}
}
