package pix_test

import (
	"image"
	"math"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
	"github.com/bluescreen10/pix/textures"
)

// refractionSize is the side of the frames the refraction tests render.
const refractionSize = 64

// refractionScene is a renderer and a scene lit by nothing, so that a pane of glass in
// it reflects nothing and shows only what comes through it, and a camera 3 units up +z
// looking at the origin.
type refractionScene struct {
	r     *pix.Renderer
	scene *scenes.Scene
}

func newRefractionScene(t *testing.T) refractionScene {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(refractionSize, refractionSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	r.EnableHDR(true)
	r.SetToneMapping(pix.ToneMapNone)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	scene.SetAmbient(colors.RGB32F{}, 0)
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	cam.LookAt(glm.Vec3f{})
	return refractionScene{r: r, scene: scene}
}

// quad is a rectangle from (xMin, yMin) to (xMax, yMax) at depth z, facing +z, with
// texture coordinates running across it.
func (s refractionScene) quad(xMin, yMin, xMax, yMax, z float32) geometries.Geometry {
	return s.r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{xMin, yMin, z}, {xMax, yMin, z}, {xMax, yMax, z}, {xMin, yMax, z}}),
			geometries.NewAttribute(geometries.AttributeNormal, geometries.Float32x3, []glm.Vec3f{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}}),
			geometries.NewAttribute(geometries.AttributeUV, geometries.Float32x2, []glm.Vec2f{{0, 1}, {1, 1}, {1, 0}, {0, 0}}),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	})
}

// addBackdrop puts an unlit rectangle of material 2 units behind the origin.
func (s refractionScene) addBackdrop(material materials.Material, xMin, xMax float32) {
	s.scene.NewMesh(s.quad(xMin, -3, xMax, 3, -2), material)
}

// unlit is an unlit material of one colour.
func (s refractionScene) unlit(color colors.RGBA32F) *materials.BasicMaterial {
	material := s.r.NewBasicMaterial()
	material.SetColor(color)
	return material
}

// addPane puts a pane of glass at the origin, turned yaw radians about the vertical.
func (s refractionScene) addPane(glass *materials.PBRMaterial, yaw float32) {
	pane := s.scene.NewMesh(s.quad(-1.5, -1.5, 1.5, 1.5, 0), glass)
	pane.SetRotation(glm.Vec3f{0, yaw, 0})
}

// render renders the scene and returns its pixels.
func (s refractionScene) render() []byte {
	s.r.Render(s.scene)
	return s.r.Pixels()
}

// pixelAt is the pixel at (x, y) of a refractionSize frame.
func refractionPixel(pixels []byte, x, y int) [3]byte {
	i := (y*refractionSize + x) * 4
	return [3]byte{pixels[i], pixels[i+1], pixels[i+2]}
}

// TestGlassTintsWhatIsBehindIt looks through a pane of red glass at a white wall: the
// wall seen through it is red. Glass that only blended over what is behind it could
// not tint it, and would show the wall white.
func TestGlassTintsWhatIsBehindIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(r *pix.Renderer)
	}{
		{"HDR", func(r *pix.Renderer) {}},
		// The copy reads the scene the opaque pass resolved from its samples.
		{"HDR and MSAA", func(r *pix.Renderer) {
			r.EnableAntiAliasing(true)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newRefractionScene(t)
			tc.setup(s.r)
			s.addBackdrop(s.unlit(colors.RGBA32F{1, 1, 1, 1}), -3, 3)
			glass := newGlass(s.r)
			glass.SetColor(colors.RGBA32F{1, 0, 0, 1})
			s.addPane(glass, 0)

			got := refractionPixel(s.render(), refractionSize/2, refractionSize/2)
			if got[0] < 200 || got[1] > 30 || got[2] > 30 {
				t.Errorf("white wall through red glass = %v, want red", got)
			}
		})
	}
}

// TestRoughGlassBlursWhatIsBehindIt looks at a wall of thin black and white stripes
// through a pane of smooth glass, then of rough glass: through the smooth one the
// stripes are as sharp as without it, through the rough one they blur into grey.
func TestRoughGlassBlursWhatIsBehindIt(t *testing.T) {
	// contrast is the spread of red along the middle row, across the pane.
	contrast := func(roughness float32) int {
		s := newRefractionScene(t)
		stripes := image.NewGray(image.Rect(0, 0, 64, 1))
		for x := range stripes.Pix {
			if x/4%2 == 0 {
				stripes.Pix[x] = 255
			}
		}
		texture := s.r.TextureStore.Create(stripes, textures.SRGB)
		t.Cleanup(texture.Release)
		wall := s.unlit(colors.RGBA32F{1, 1, 1, 1})
		wall.SetColorMap(texture)
		wall.SetColorMapSampler(s.r.TextureStore.DefaultSampler())
		s.addBackdrop(wall, -2, 2)
		glass := newGlass(s.r)
		glass.SetRoughness(roughness)
		s.addPane(glass, 0)

		pixels := s.render()
		darkest, brightest := 255, 0
		for x := refractionSize/2 - 8; x < refractionSize/2+8; x++ {
			red := int(refractionPixel(pixels, x, refractionSize/2)[0])
			darkest, brightest = min(darkest, red), max(brightest, red)
		}
		return brightest - darkest
	}

	if sharp := contrast(0); sharp < 150 {
		t.Errorf("stripes through smooth glass span %d, want them sharp (at least 150)", sharp)
	}
	if blurred := contrast(1); blurred > 60 {
		t.Errorf("stripes through rough glass span %d, want them blurred (at most 60)", blurred)
	}
}

// TestThickGlassBendsWhatIsBehindIt looks through a pane turned 45 degrees to face up
// and to the right, at a wall white left of x = -0.1 and black right of it. The middle
// of the frame sees the wall at x = 0: black. Through a thin pane it still does. Through
// a pane 1 unit thick, of index 1.5, the view ray bends toward the pane's inward normal
// as it enters — to the left, here, by 0.29 units over that thickness — and the middle
// of the frame sees the wall's white.
func TestThickGlassBendsWhatIsBehindIt(t *testing.T) {
	middle := func(thickness float32) [3]byte {
		s := newRefractionScene(t)
		s.addBackdrop(s.unlit(colors.RGBA32F{1, 1, 1, 1}), -3, -0.1)
		s.addBackdrop(s.unlit(colors.RGBA32F{0, 0, 0, 1}), -0.1, 3)
		glass := newGlass(s.r)
		glass.SetThickness(thickness)
		s.addPane(glass, math.Pi/4)
		return refractionPixel(s.render(), refractionSize/2, refractionSize/2)
	}

	if thin := middle(0); thin[0] > 30 {
		t.Errorf("wall through a thin pane = %v, want the black it is unbent", thin)
	}
	if thick := middle(1); thick[0] < 150 {
		t.Errorf("wall through a thick pane = %v, want the white left of it, bent", thick)
	}
}

// TestThinGlassReflectsFromBothSurfaces looks head-on at a pane of clear glass with no
// thickness, under a uniform white sky and with nothing behind it: all it shows is what
// it reflects. A pane has two surfaces, each reflecting R = 4% of the light meeting it,
// and the light bouncing between them comes out too, so a pane reflects 2R/(1+R), 7.7%
// — not the 4% of one surface. Drawn as one surface, a window pane in a bright scene
// reflects half of what it should, and all but disappears.
func TestThinGlassReflectsFromBothSurfaces(t *testing.T) {
	s := newRefractionScene(t)
	white := environmentImage(s.r, 8, 4, uniform([3]byte{255, 255, 255}))
	environment := scenes.NewEnvironment(white)
	white.Release()
	defer environment.Release()
	s.scene.SetEnvironment(environment)
	s.addPane(newGlass(s.r), 0)

	const r = 0.04
	want := srgbByte(2 * r / (1 + r))
	if got := refractionPixel(s.render(), refractionSize/2, refractionSize/2); absDiff(int(got[1]), want) > 3 {
		t.Errorf("pane reflecting a white sky = %v, want %d: both its surfaces' reflections (one alone is %d)", got, want, srgbByte(r))
	}
}

// TestGlassRefractsOnceTransmissionIsSet draws a red, premultiplied pane that does not
// yet let light through, then gives it transmission: the next frame shows the white
// wall behind it through it, tinted red. The scene is only copied for blended surfaces
// that refract, so the frame has to notice the pane now does — though nothing else
// that decides how it is drawn has changed: its blend mode was premultiplied already,
// and with an alpha cut-off, which its opaque colour leaves whole, it was already
// masked.
func TestGlassRefractsOnceTransmissionIsSet(t *testing.T) {
	s := newRefractionScene(t)
	s.addBackdrop(s.unlit(colors.RGBA32F{1, 1, 1, 1}), -3, 3)
	pane := s.r.NewPBRMaterial()
	pane.SetColor(colors.RGBA32F{1, 0, 0, 1})
	pane.SetMetallic(0)
	pane.SetRoughness(0)
	pane.SetBlend(materials.BlendPremultiplied)
	pane.SetAlphaCutoff(0.5)
	s.addPane(pane, 0)
	s.render()

	pane.SetTransmission(1)
	got := refractionPixel(s.render(), refractionSize/2, refractionSize/2)
	if got[0] < 200 || got[1] > 30 || got[2] > 30 {
		t.Errorf("white wall through the pane once it lets light through = %v, want red", got)
	}
}
