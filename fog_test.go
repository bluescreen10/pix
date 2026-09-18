package pix

import (
	"testing"
	"unsafe"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/scenes"
)

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
	l.rebuild(scenes.EnvironmentPacket{}, nil, nil, false)
	if l.data.fogColor[3] != float32(scenes.FogNone) {
		t.Fatalf("nil fog wrote mode %v, want %d", l.data.fogColor[3], scenes.FogNone)
	}
	l.dirty = false

	fog := scenes.NewExp2Fog(colors.RGB32F{0.4, 0.5, 0.6}, 500)
	l.rebuild(scenes.EnvironmentPacket{Fog: scenes.StateOf(fog)}, nil, nil, false)
	if !l.dirty {
		t.Error("setting fog did not dirty the table")
	}
	if l.data.fogColor != [4]float32{0.4, 0.5, 0.6, float32(scenes.FogExp2)} {
		t.Errorf("fogColor = %v", l.data.fogColor)
	}
	if want := scenes.StateOf(scenes.NewExp2Fog(colors.RGB32F{}, 500)).Density; l.data.fogParams[2] != want {
		t.Errorf("density = %v, want %v", l.data.fogParams[2], want)
	}

	// Mutating the fog object in place must be picked up: the fields are exported
	// precisely so they can be animated.
	l.dirty = false
	fog.Distance = 200
	l.rebuild(scenes.EnvironmentPacket{Fog: scenes.StateOf(fog)}, nil, nil, false)
	if want := scenes.StateOf(scenes.NewExp2Fog(colors.RGB32F{}, 200)).Density; !l.dirty || l.data.fogParams[2] != want {
		t.Errorf("in-place distance change not picked up: dirty=%v params=%v", l.dirty, l.data.fogParams)
	}
}
