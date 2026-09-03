package pix

import (
	"testing"
	"unsafe"

	"github.com/bluescreen10/pix/glm"
)

// TestFogPacking pins the packed form the shaders read: mode in fogColor.w and
// (near, far, density) in fogParams, with nil meaning no fog.
func TestFogPacking(t *testing.T) {
	tests := []struct {
		name   string
		fog    Fog
		color  glm.Vec3f
		mode   uint32
		params [3]float32
	}{
		{"nil disables", nil, glm.Vec3f{}, fogNone, [3]float32{}},
		{"linear", NewLinearFog(glm.Vec3f{0.5, 0.6, 0.7}, 10, 200), glm.Vec3f{0.5, 0.6, 0.7}, fogLinear, [3]float32{10, 200, 0}},
		{"exp2", NewExp2Fog(glm.Vec3f{0.1, 0.2, 0.3}, 0.015), glm.Vec3f{0.1, 0.2, 0.3}, fogExp2, [3]float32{0, 0, 0.015}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := stateOf(tc.fog)
			if s.color != tc.color || s.mode != tc.mode {
				t.Errorf("color/mode = %v/%d, want %v/%d", s.color, s.mode, tc.color, tc.mode)
			}
			if got := [3]float32{s.near, s.far, s.density}; got != tc.params {
				t.Errorf("params = %v, want %v", got, tc.params)
			}
		})
	}
}

// TestFogLightTableLayout guards the offsets LightBuf in lighting.glsl depends on:
// fog sits between ambient and the light counts, so a field added above it silently
// desynchronizes every lit shader.
func TestFogLightTableLayout(t *testing.T) {
	var g gpuLights
	for _, c := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"ambient", unsafe.Offsetof(g.ambient), 0},
		{"fogColor", unsafe.Offsetof(g.fogColor), 16},
		{"fogParams", unsafe.Offsetof(g.fogParams), 32},
		{"numDir", unsafe.Offsetof(g.numDir), 48},
	} {
		if c.got != c.want {
			t.Errorf("offset of %s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// TestFogRebuild checks that fog reaches the GPU table and that changing it alone
// re-dirties the buffer — the table is only re-uploaded when rebuild sees a change.
func TestFogRebuild(t *testing.T) {
	l := &Lights{}
	l.rebuild(glm.Vec3f{}, nil, nil, nil, nil)
	if l.data.fogColor[3] != float32(fogNone) {
		t.Fatalf("nil fog wrote mode %v, want %d", l.data.fogColor[3], fogNone)
	}
	l.dirty = false

	fog := NewExp2Fog(glm.Vec3f{0.4, 0.5, 0.6}, 0.02)
	l.rebuild(glm.Vec3f{}, fog, nil, nil, nil)
	if !l.dirty {
		t.Error("setting fog did not dirty the table")
	}
	if l.data.fogColor != [4]float32{0.4, 0.5, 0.6, float32(fogExp2)} {
		t.Errorf("fogColor = %v", l.data.fogColor)
	}
	if l.data.fogParams[2] != 0.02 {
		t.Errorf("density = %v, want 0.02", l.data.fogParams[2])
	}

	// Mutating the fog object in place must be picked up: the fields are exported
	// precisely so they can be animated.
	l.dirty = false
	fog.Density = 0.05
	l.rebuild(glm.Vec3f{}, fog, nil, nil, nil)
	if !l.dirty || l.data.fogParams[2] != 0.05 {
		t.Errorf("in-place density change not picked up: dirty=%v params=%v", l.dirty, l.data.fogParams)
	}
}
