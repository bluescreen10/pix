package gltf_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/loaders/gltf"
	"github.com/bluescreen10/pix/scenes"
)

func avgColor(px []byte) (r, g, b float64, lit int) {
	for i := 0; i+3 < len(px); i += 4 {
		rr, gg, bb := px[i], px[i+1], px[i+2]
		if rr == 0 && gg == 0 && bb == 0 {
			continue
		}
		r += float64(rr)
		g += float64(gg)
		b += float64(bb)
		lit++
	}
	if lit == 0 {
		return 0, 0, 0, 0
	}
	return r / float64(lit), g / float64(lit), b / float64(lit), lit
}

// TestMSFTLod loads a hand-authored glTF using the MSFT_lod extension (one node,
// "LOD_high", carrying extensions.MSFT_lod.ids=[1,2] and
// extras.MSFT_screencoverage=[0.5,0.2,0.01], each id a plain cube-mesh node not
// otherwise in the scene — see testdata/msft_lod_cube.gltf) and confirms it comes
// back as one scenes.Mesh with 3 real AddLOD levels (red/green/blue, matching the
// asset's own materials), by rendering it at three camera distances chosen to land
// in each level's range.
func TestMSFTLod(t *testing.T) {
	const size = 128
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.9, 0.9, 0.9})

	if _, err := gltf.Load(r, scene, "testdata/msft_lod_cube.gltf"); err != nil {
		t.Fatal(err)
	}
	if got := scene.MeshCount(); got != 1 {
		t.Fatalf("expected exactly 1 Mesh (the alternates must not become their own scene nodes), got %d", got)
	}

	cam := scene.NewPerspectiveCamera(45, 1, 0.01, 1000)
	scene.Add(cam) // looks down -Z, at the origin from every position below

	// Cube radius is ~0.866 (half-diagonal of a unit cube). Coverage 0.5 -> distance
	// ~1.22, coverage 0.2 -> ~1.94 (see applyMSFTLod's radius/sqrt(coverage)
	// conversion). applyMSFTLod also sets a real hysteresis (15% of the outermost
	// threshold, ~0.29 here) instead of leaving it at 0 — see its doc comment — so
	// each sample below is picked clearly past the previous level's widened band,
	// not just past the raw boundary, or this would flakily stay on the prior level.
	cases := []struct {
		dist        float32
		wantChannel string
	}{
		{0.6, "red"},   // < 1.22: level 0
		{1.8, "green"}, // > 1.22+0.29 widened edge, < 1.94: level 1
		{5.0, "blue"},  // > 1.94+0.29 widened edge: level 2
	}
	for _, c := range cases {
		cam.SetPosition(glm.Vec3f{0, 0, c.dist})
		r.Render(scene)
		rr, gg, bb, lit := avgColor(r.Pixels())
		t.Logf("distance %.2f: avg=(%.0f,%.0f,%.0f) lit=%d", c.dist, rr, gg, bb, lit)
		if lit < 20 {
			t.Fatalf("distance %.2f: nothing rendered", c.dist)
		}
		dominant := map[string]float64{"red": rr, "green": gg, "blue": bb}
		for ch, v := range dominant {
			if ch == c.wantChannel {
				if v < 150 {
					t.Errorf("distance %.2f: expected %s to dominate, got avg=(%.0f,%.0f,%.0f)", c.dist, c.wantChannel, rr, gg, bb)
				}
			} else if v > 80 {
				t.Errorf("distance %.2f: expected %s NOT to be present, got avg=(%.0f,%.0f,%.0f)", c.dist, ch, rr, gg, bb)
			}
		}
	}
}
