package pix_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/gamekit/utils"
	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
	"github.com/bluescreen10/pix/textures"
)

//go:generate go run ./cmd/shadercompile -i testdata/image_fill.comp.glsl -o spv:testdata/build/image_fill.comp.spv -o metallib:testdata/build/image_fill.comp.metalbin

// environmentImage is an equirectangular image, width x height, whose texel at (u, v) —
// its centre, in [0, 1] — has the linear colour color returns.
func environmentImage(r *pix.Renderer, width, height int, color func(u, v float64) [3]byte) textures.Texture {
	rgba := make([]byte, 0, width*height*4)
	for y := range height {
		for x := range width {
			c := color((float64(x)+0.5)/float64(width), (float64(y)+0.5)/float64(height))
			rgba = append(rgba, c[0], c[1], c[2], 255)
		}
	}
	return r.TextureStore.Create(rgba, width, height, textures.Linear)
}

// environmentScene is a sphere of the given material, 3 units down -z from a camera at
// the origin, lit by nothing but the given environment, over a black background.
func environmentScene(t *testing.T, environment *scenes.Environment, material func(*pix.Renderer) materials.Material) (*pix.Renderer, *scenes.Scene) {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	scene.SetAmbient(colors.RGB32F{}, 1)
	scene.SetEnvironment(environment)
	if material != nil {
		sphere := scene.NewMesh(r.NewSphereGeometry(1, 48, 32), material(r))
		sphere.SetPosition(glm.Vec3f{0, 0, -3})
		scene.Add(sphere)
	}
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	scene.Add(cam)
	cam.LookAt(glm.Vec3f{0, 0, -1})
	return r, scene
}

// uniform is an environment image of one colour.
func uniform(c [3]byte) func(u, v float64) [3]byte {
	return func(u, v float64) [3]byte {
		return c
	}
}

// metal is a white metal of the given roughness: it reflects the environment in full,
// without any diffuse light.
func metal(roughness float32) func(*pix.Renderer) materials.Material {
	return func(r *pix.Renderer) materials.Material {
		m := r.NewPBRMaterial()
		m.SetColor(colors.RGBA32F{1, 1, 1, 1})
		m.SetMetallic(1)
		m.SetRoughness(roughness)
		return m
	}
}

// TestEnvironmentLightsLikeAWhiteFurnace: under a uniform white environment, a white
// surface takes light 1 from every side — what a uniform ambient colour of 1 gives. For
// PBR it must hold too, whatever of it the surface reflects rather than diffuses: what
// one term gives the other takes away, the "white furnace" test of energy conservation.
func TestEnvironmentLightsLikeAWhiteFurnace(t *testing.T) {
	for _, material := range litMaterials {
		t.Run(material.name, func(t *testing.T) {
			r, scene := environmentScene(t, nil, material.new)
			white := environmentImage(r, 8, 4, uniform([3]byte{255, 255, 255}))
			environment := scenes.NewEnvironment(white)
			white.Release()
			defer environment.Release()
			scene.SetEnvironment(environment)

			r.Render(scene)

			if got := pixelAt(r, postSize/2, postSize/2); got[0] < 251 || got[0] != got[1] || got[1] != got[2] {
				t.Errorf("sphere centre = %v, want white, 255: the light of a uniform white environment", got)
			}
		})
	}
}

// TestEnvironmentDiffuseFollowsTheNormal: a surface takes its diffuse light from the half
// of the environment it faces, blurred over all of it. Under red above and blue below,
// the top of a sphere is mostly red and its underside mostly blue, but each also sees
// some of the other half.
func TestEnvironmentDiffuseFollowsTheNormal(t *testing.T) {
	r, scene := environmentScene(t, nil, litMaterials[0].new)
	sky := environmentImage(r, 8, 4, func(u, v float64) [3]byte {
		if v < 0.5 {
			return [3]byte{255, 0, 0}
		}
		return [3]byte{0, 0, 255}
	})
	environment := scenes.NewEnvironment(sky)
	sky.Release()
	defer environment.Release()
	scene.SetEnvironment(environment)

	r.Render(scene)

	// The sphere's edge is about 41 pixels from its centre, so these probes, 32 out, see
	// normals whose vertical part is about 0.78, which a cosine lobe lights 0.89 red
	// above and 0.89 blue below. The roughest mip of the reflections, 8 x 4 texels, gets
	// to about 0.76; blurring each mip at its full roughness again would leave 0.63 and
	// fail. A mirror's reflection along the normal would show no blue at the top at all.
	top, bottom := pixelAt(r, postSize/2, postSize/2-postSize/3), pixelAt(r, postSize/2, postSize/2+postSize/3)
	if int(top[0]) < int(top[2])+60 || int(bottom[2]) < int(bottom[0])+60 {
		t.Errorf("sphere top = %v, bottom = %v, want the top red and the bottom blue", top, bottom)
	}
	if int(top[2]) < srgbByte(0.1) || int(bottom[0]) < srgbByte(0.1) {
		t.Errorf("sphere top = %v, bottom = %v, want each lit a little by the other half", top, bottom)
	}
}

// greenAhead is an environment that is black but for a green patch around +z: what the
// centre of a sphere seen down -z mirrors straight back at the camera.
func greenAhead(u, v float64) [3]byte {
	// +z sits at u = 0.75, v = 0.5 (see scenes.Environment's layout).
	if math.Abs(u-0.75) < 0.1 && math.Abs(v-0.5) < 0.15 {
		return [3]byte{0, 255, 0}
	}
	return [3]byte{0, 0, 0}
}

// TestEnvironmentReflectionMirrorsAndBlurs: a smooth white metal mirrors the
// environment: the centre of the sphere shows the green patch straight behind the
// camera at full strength. A rough one blurs it over the whole hemisphere around its
// normal, of which the patch is a small part, so the centre keeps less than a tenth of
// that light, though still green. Merely shrinking the image mip by mip, without the
// blur, keeps about three times as much.
func TestEnvironmentReflectionMirrorsAndBlurs(t *testing.T) {
	var mirror *materials.PBRMaterial
	r, scene := environmentScene(t, nil, func(r *pix.Renderer) materials.Material {
		mirror = metal(0)(r).(*materials.PBRMaterial)
		return mirror
	})
	image := environmentImage(r, 64, 32, greenAhead)
	environment := scenes.NewEnvironment(image)
	image.Release()
	defer environment.Release()
	scene.SetEnvironment(environment)

	r.Render(scene)
	smooth := pixelAt(r, postSize/2, postSize/2)
	if smooth[1] < 240 || smooth[0] > 10 || smooth[2] > 10 {
		t.Errorf("smooth metal centre = %v, want the green patch it mirrors, at full strength", smooth)
	}

	mirror.SetRoughness(1)
	r.Render(scene)
	rough := pixelAt(r, postSize/2, postSize/2)
	if rough[1] == 0 || int(rough[1]) >= srgbByte(0.1) {
		t.Errorf("rough metal centre = %v, want under a tenth of the light the smooth one's %v mirrors, but still green", rough, smooth)
	}
}

// TestEnvironmentRotationTurnsIt: rotating the environment half a turn about the
// vertical takes the green patch from behind the camera to in front of it, out of what
// the mirror's centre reflects.
func TestEnvironmentRotationTurnsIt(t *testing.T) {
	r, scene := environmentScene(t, nil, metal(0))
	image := environmentImage(r, 64, 32, greenAhead)
	environment := scenes.NewEnvironment(image)
	image.Release()
	defer environment.Release()
	scene.SetEnvironment(environment)

	environment.Rotation = math.Pi
	r.Render(scene)

	if got := pixelAt(r, postSize/2, postSize/2); got[1] > 10 {
		t.Errorf("mirror centre = %v with the environment turned half a turn, want black: the patch is behind the sphere now", got)
	}
}

// TestEnvironmentDrawsBehindTheScene: an environment set to be the background shows,
// in the direction the camera looks, wherever no geometry is; one that is not leaves
// the clear colour.
func TestEnvironmentDrawsBehindTheScene(t *testing.T) {
	r, scene := environmentScene(t, nil, nil)
	grey := environmentImage(r, 8, 4, uniform([3]byte{128, 128, 128}))
	environment := scenes.NewEnvironment(grey)
	grey.Release()
	defer environment.Release()
	scene.SetEnvironment(environment)

	r.Render(scene)
	if got := pixelAt(r, postSize/2, postSize/2); got != [3]byte{} {
		t.Errorf("without Background, centre = %v, want the black clear colour", got)
	}

	environment.Background = true
	r.Render(scene)
	want := srgbByte(128.0 / 255)
	if got := pixelAt(r, postSize/2, postSize/2); !isNear(got[0], 128.0/255, 1) || got[0] != got[1] || got[1] != got[2] {
		t.Errorf("with Background, centre = %v, want grey %d, the environment", got, want)
	}
}

// fillStep fills a writable texture with a colour at the start of every frame.
type fillStep struct {
	texture  textures.Texture
	side     uint32
	color    [4]float32
	shader   []byte
	backend  gpu.Backend
	pipeline gpu.Pipeline
}

func (s *fillStep) Encode(frame *pix.Frame, cmd gpu.CommandBuffer) {
	if !s.pipeline.IsValid() {
		s.backend = frame.Backend
		s.pipeline = frame.Backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: s.shader, Label: "texture-fill"})
	}
	cmd.SetPipeline(s.pipeline)
	data := struct {
		image, side uint32
		_           [2]uint32
		color       [4]float32
	}{image: s.texture.Index(), side: s.side, color: s.color}
	cmd.Dispatch(utils.ToBytes(&data), 1, 1, 1)
}

func (s *fillStep) Release() {
	if s.pipeline.IsValid() {
		s.backend.DestroyPipeline(s.pipeline)
		s.pipeline = gpu.Pipeline{}
	}
}

// TestEnvironmentRederivedOnlyWhenInvalidated: an environment the GPU writes, as a sky
// does, is lit by what it held when its light was last derived. A frame step fills it
// white, and the furnace lights the sphere white; the step then fills it black, which
// changes nothing until Invalidate says the image changed — after which the sphere goes
// dark.
func TestEnvironmentRederivedOnlyWhenInvalidated(t *testing.T) {
	r, scene := environmentScene(t, nil, litMaterials[0].new)
	const side = 8
	image := r.TextureStore.CreateWritable(textures.WritableConfig{
		Kind: gpu.Texture2D, Width: side, Height: side, Format: gpu.FormatRGBA16F, Label: "environment",
	})
	step := &fillStep{texture: image, side: side, color: [4]float32{1, 1, 1, 1}, shader: testShader(t, r, "image_fill.comp")}
	r.AddFrameStep(pix.FrameStageStart, step)
	environment := scenes.NewEnvironment(image)
	image.Release()
	defer environment.Release()
	scene.SetEnvironment(environment)

	centre := func() byte {
		r.Render(scene)
		return pixelAt(r, postSize/2, postSize/2)[0]
	}
	if got := centre(); got < 251 {
		t.Fatalf("filled white, sphere centre = %d, want 255", got)
	}
	step.color = [4]float32{0, 0, 0, 1}
	if got := centre(); got < 251 {
		t.Errorf("filled black but not invalidated, sphere centre = %d, want still 255: its light derived from the white", got)
	}
	environment.Invalidate()
	if got := centre(); got > 2 {
		t.Errorf("filled black and invalidated, sphere centre = %d, want 0", got)
	}
}

// TestEnvironmentRemovedAndSetAgain: a scene that drops its environment is lit by its
// ambient colour again, and the renderer frees what it derived; setting the environment
// back derives its light anew, though the environment itself has not changed.
func TestEnvironmentRemovedAndSetAgain(t *testing.T) {
	r, scene := environmentScene(t, nil, litMaterials[0].new)
	white := environmentImage(r, 8, 4, uniform([3]byte{255, 255, 255}))
	environment := scenes.NewEnvironment(white)
	white.Release()
	defer environment.Release()

	centre := func() byte {
		r.Render(scene)
		return pixelAt(r, postSize/2, postSize/2)[0]
	}
	scene.SetEnvironment(environment)
	if got := centre(); got < 251 {
		t.Fatalf("with the white environment, sphere centre = %d, want 255", got)
	}
	scene.SetEnvironment(nil)
	if got := centre(); got > 2 {
		t.Errorf("with no environment and no ambient light, sphere centre = %d, want 0", got)
	}
	scene.SetEnvironment(environment)
	if got := centre(); got < 251 {
		t.Errorf("with the environment set again, sphere centre = %d, want 255: its light derived anew", got)
	}
}
