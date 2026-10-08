package scenes_test

import (
	"testing"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// TestDirectionalShadowDefaults: a light that only turns shadows on gets the uniform
// method with two cascades over the first 100 units, split at a tenth of that; the
// later splits are there for when more cascades are asked for.
func TestDirectionalShadowDefaults(t *testing.T) {
	scene := scenes.New()
	defer scene.Destroy()
	light := scene.AddDirectionalLight(glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 1)
	light.SetCastShadow(true)
	shadow := light.Shadow()

	if got := shadow.Method(); got != scenes.ShadowUniform {
		t.Errorf("Method() = %v, want ShadowUniform", got)
	}
	if got := shadow.Cascades(); got != 2 {
		t.Errorf("Cascades() = %d, want 2", got)
	}
	if got := shadow.Distance(); got != 100 {
		t.Errorf("Distance() = %v, want 100", got)
	}
	if got, want := shadow.Splits(), [3]float32{0.1, 0.2, 0.5}; got != want {
		t.Errorf("Splits() = %v, want %v", got, want)
	}
	if got := shadow.Size(); got != scenes.DefaultShadowSize {
		t.Errorf("Size() = %d, want %d", got, scenes.DefaultShadowSize)
	}
}

// TestDirectionalShadowSettingsAreGuarded: out-of-range settings must not reach a
// renderer as something it would allocate or divide by.
func TestDirectionalShadowSettingsAreGuarded(t *testing.T) {
	scene := scenes.New()
	defer scene.Destroy()
	light := scene.AddDirectionalLight(glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 1)
	light.SetCastShadow(true)
	shadow := light.Shadow()

	shadow.SetCascades(99)
	if got := shadow.Cascades(); got != scenes.MaxShadowCascades {
		t.Errorf("SetCascades(99): Cascades() = %d, want %d", got, scenes.MaxShadowCascades)
	}
	shadow.SetCascades(0)
	if got := shadow.Cascades(); got != 1 {
		t.Errorf("SetCascades(0): Cascades() = %d, want 1", got)
	}

	shadow.SetMethod(scenes.ShadowMethod(200))
	if got := shadow.Method(); got != scenes.ShadowUniform {
		t.Errorf("SetMethod(200): Method() = %v, want ShadowUniform", got)
	}

	shadow.SetDistance(40)
	shadow.SetDistance(0)
	shadow.SetDistance(-5)
	if got := shadow.Distance(); got != 40 {
		t.Errorf("Distance() = %v after setting 40 then 0 and -5, want 40", got)
	}
}

// TestDirectionalShadowReachesThePacket: the renderer only sees the packet, so every
// setting has to arrive there.
func TestDirectionalShadowReachesThePacket(t *testing.T) {
	scene := scenes.New()
	defer scene.Destroy()
	light := scene.AddDirectionalLight(glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 1)
	light.SetCastShadow(true)
	light.Shadow().SetMethod(scenes.ShadowUniform)
	light.Shadow().SetCascades(3)
	light.Shadow().SetDistance(72)
	light.Shadow().SetSplits([3]float32{0.125, 0.5, 0.75})

	var packet scenes.FramePacket
	scene.Extract(&packet)
	if len(packet.Lights.Data) != 1 {
		t.Fatalf("packet has %d lights, want 1", len(packet.Lights.Data))
	}
	lp := packet.Lights.Data[0]
	if lp.ShadowMethod != scenes.ShadowUniform || lp.ShadowCascades() != 3 || lp.ShadowDistance() != 72 || lp.ShadowSplits != [3]float32{0.125, 0.5, 0.75} {
		t.Errorf("packet carries method %v, cascades %d, distance %v, splits %v; "+
			"want uniform, 3, 72, [0.125 0.5 0.75]", lp.ShadowMethod, lp.ShadowCascades(), lp.ShadowDistance(), lp.ShadowSplits)
	}
}

// TestLightPacketShadowDefaults: a packet that states no map size, cascade count or
// distance — a light that does not cast, or a producer that leaves them alone — reads
// back the defaults rather than an empty map split into zero cascades over zero units.
func TestLightPacketShadowDefaults(t *testing.T) {
	var lp scenes.LightPacket
	if got := lp.ShadowSize(); got != scenes.DefaultShadowSize {
		t.Errorf("ShadowSize() = %d, want %d", got, scenes.DefaultShadowSize)
	}
	if got := lp.ShadowCascades(); got != scenes.DefaultShadowCascades {
		t.Errorf("ShadowCascades() = %d, want %d", got, scenes.DefaultShadowCascades)
	}
	if got := lp.ShadowDistance(); got != scenes.DefaultShadowDistance {
		t.Errorf("ShadowDistance() = %v, want %v", got, scenes.DefaultShadowDistance)
	}
}
