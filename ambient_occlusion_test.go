package pix_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

const (
	aoTestSize = 128
	// aoTestRadius is how far, in world units, the tests' occlusion reaches.
	aoTestRadius = 1
	// cornerZ is where the corner scene's wall stands; the floor meets it along the
	// line y = 0, z = cornerZ.
	cornerZ = -3
)

// aoScene is a renderer, size pixels square, and a scene it renders, seen through cam.
type aoScene struct {
	r     *pix.Renderer
	size  int
	scene *scenes.Scene
	cam   scenes.Camera
}

// newAOScene creates a renderer aoTestSize pixels square with ambient occlusion reaching
// aoTestRadius, and an empty scene lit by white ambient light alone, which white
// surfaces reflect as white.
func newAOScene(t *testing.T) aoScene {
	t.Helper()
	return newAOSceneOfSize(t, aoTestSize)
}

// newAOSceneOfSize is newAOScene with a renderer size pixels square.
func newAOSceneOfSize(t *testing.T, size int) aoScene {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(uint32(size), uint32(size))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
	r.SetAmbientOcclusionSettings(pix.AmbientOcclusionSettings{Radius: aoTestRadius})

	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1)
	cam := scene.NewPerspectiveCamera(60, 1, 0.1, 100)
	scene.Add(cam)
	return aoScene{r: r, size: size, scene: scene, cam: cam}
}

// matte is a white Blinn-Phong material with no highlight, so a surface shows exactly
// the light that reaches it.
func matte(r *pix.Renderer) materials.Material {
	material := r.NewBlinnPhongMaterial()
	material.SetSpecular(0)
	return material
}

// addFloor lays a wide floor at y = 0.
func (s aoScene) addFloor() {
	s.scene.Add(s.scene.NewMesh(s.r.NewPlaneGeometry(40, 40, 1, 1), matte(s.r)))
}

// addWall stands a wide wall whose face looks down +z from z.
func (s aoScene) addWall(z float32) {
	wall := s.scene.NewMesh(s.r.NewBoxGeometry(40, 20, 1), matte(s.r))
	wall.SetPosition(glm.Vec3f{0, 10, z - 0.5})
	s.scene.Add(wall)
}

// newCornerScene is a floor meeting a wall at a right angle, seen from above and in
// front of the line where they meet.
func newCornerScene(t *testing.T) aoScene {
	t.Helper()
	return newCornerSceneOfSize(t, aoTestSize)
}

// newCornerSceneOfSize is newCornerScene with a renderer size pixels square.
func newCornerSceneOfSize(t *testing.T, size int) aoScene {
	t.Helper()
	s := newAOSceneOfSize(t, size)
	s.addFloor()
	s.addWall(cornerZ)
	s.cam.SetPosition(glm.Vec3f{0, 3, 3})
	s.cam.LookAt(glm.Vec3f{0, 0.5, cornerZ})
	return s
}

// floorPoint is the point on the corner scene's floor distance from the wall.
func floorPoint(distance float32) glm.Vec3f {
	return glm.Vec3f{0, 0, cornerZ + distance}
}

// pixelOf is the pixel point lands in.
func (s aoScene) pixelOf(point glm.Vec3f) (x, y int) {
	clip := s.cam.ViewProjection().Mul4x1(glm.Vec4f{point[0], point[1], point[2], 1})
	ndcX, ndcY := clip[0]/clip[3], clip[1]/clip[3]
	return int((ndcX*0.5 + 0.5) * float32(s.size)), int((0.5 - ndcY*0.5) * float32(s.size))
}

// linearAt is the linear light of the red channel at (x, y) in the last frame.
func (s aoScene) linearAt(x, y int) float64 {
	return linearFromSRGB(s.r.Pixels()[(y*s.size+x)*4])
}

// linearAtPoint is linearAt the pixel point lands in.
func (s aoScene) linearAtPoint(point glm.Vec3f) float64 {
	return s.linearAt(s.pixelOf(point))
}

// settledFrames is how many frames a test renders for occlusion to settle, as shadows
// and the renderer's frame-to-frame state do on screen.
const settledFrames = 24

// renderSettled renders the scene enough times for its occlusion to settle.
func (s aoScene) renderSettled() {
	for range settledFrames {
		s.r.Render(s.scene)
	}
}

// opennessAt renders the scene through the ambient occlusion debug view and returns
// how open the surface at point is: 1 where nothing occludes it.
func (s aoScene) opennessAt(point glm.Vec3f) float64 {
	s.r.SetDebugView(pix.DebugAmbientOcclusion)
	defer s.r.SetDebugView(pix.DebugOff)
	s.renderSettled()
	return s.linearAtPoint(point)
}

// renderShaded renders the scene shaded, with ambient occlusion on or off.
func (s aoScene) renderShaded(occlusion bool) {
	s.r.EnableAmbientOcclusion(occlusion)
	s.renderSettled()
}

// linearFromSRGB decodes an 8-bit sRGB value to linear light.
func linearFromSRGB(b byte) float64 {
	v := float64(b) / 255
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// TestAmbientOcclusionDarkensCorners: on the floor in front of a wall, how open the
// surface is grows steadily with the distance from the wall, from well occluded at the
// corner to fully open past the occlusion's radius; and shading darkens with it, while
// leaving the open floor as bright as without occlusion.
func TestAmbientOcclusionDarkensCorners(t *testing.T) {
	s := newCornerScene(t)

	distances := []float32{0.05, 0.25, 0.5, 2 * aoTestRadius}
	openness := make([]float64, len(distances))
	for i, d := range distances {
		openness[i] = s.opennessAt(floorPoint(d))
	}
	t.Logf("openness at %v from the wall: %.3f", distances, openness)
	if openness[0] > 0.85 {
		t.Errorf("openness at the corner = %.3f, want it occluded below 0.85", openness[0])
	}
	for i := 1; i < len(openness); i++ {
		if openness[i] < openness[i-1] {
			t.Errorf("openness %.3f at %v < %.3f at %v, want it to grow away from the wall", openness[i], distances[i], openness[i-1], distances[i-1])
		}
	}
	if last := openness[len(openness)-1]; last < 0.97 {
		t.Errorf("openness beyond the radius = %.3f, want fully open (at least 0.97)", last)
	}

	for _, hdr := range []bool{false, true} {
		s.r.EnableHDR(hdr)
		s.r.SetToneMapping(pix.ToneMapNone)
		s.renderShaded(false)
		cornerOff, openOff := s.linearAtPoint(floorPoint(0.05)), s.linearAtPoint(floorPoint(2*aoTestRadius))
		s.renderShaded(true)
		cornerOn, openOn := s.linearAtPoint(floorPoint(0.05)), s.linearAtPoint(floorPoint(2*aoTestRadius))
		if cornerOn >= cornerOff-0.1 {
			t.Errorf("HDR %v: corner shading = %.3f with occlusion, %.3f without; want it darker", hdr, cornerOn, cornerOff)
		}
		if math.Abs(openOn-openOff) > 0.02 {
			t.Errorf("HDR %v: open floor shading = %.3f with occlusion, %.3f without; want it unchanged", hdr, openOn, openOff)
		}
	}
}

// TestAmbientOcclusionLeavesFlatSurfacesOpen: a flat floor, seen at a grazing angle
// where reconstructing its slope from depth is hardest, occludes nothing of itself — out
// to 8 units ahead. Farther, the floor reaches the pixels beside the horizon, where the
// blur has empty sky on one side and averages fewer directions, and reads a few percent
// short of open.
func TestAmbientOcclusionLeavesFlatSurfacesOpen(t *testing.T) {
	s := newAOScene(t)
	s.addFloor()
	s.cam.SetPosition(glm.Vec3f{0, 0.5, 4})
	s.cam.LookAt(glm.Vec3f{0, 0, -6})

	s.r.SetDebugView(pix.DebugAmbientOcclusion)
	s.renderSettled()
	for d := float32(1); d <= 8; d++ {
		point := glm.Vec3f{0, 0, 4 - d}
		if open := s.linearAtPoint(point); open < 0.97 {
			x, y := s.pixelOf(point)
			t.Errorf("openness of the flat floor %v ahead (pixel %d,%d) = %.3f, want fully open (at least 0.97)", d, x, y, open)
		}
	}
}

// TestAmbientOcclusionHasNoHalos: a box floating far in front of a wall does not darken
// the wall around its outline, however close the two are on screen: the gap between
// them is many times the occlusion's radius.
func TestAmbientOcclusionHasNoHalos(t *testing.T) {
	s := newAOScene(t)
	wall := s.scene.NewMesh(s.r.NewBoxGeometry(40, 40, 1), matte(s.r))
	wall.SetPosition(glm.Vec3f{0, 0, -10.5})
	s.scene.Add(wall)
	box := s.scene.NewMesh(s.r.NewBoxGeometry(2, 2, 2), matte(s.r))
	s.scene.Add(box)
	s.cam.SetPosition(glm.Vec3f{0, 0, 6})
	s.cam.LookAt(glm.Vec3f{0, 0, 0})

	s.r.SetDebugView(pix.DebugAmbientOcclusion)
	s.renderSettled()
	_, centreY := s.pixelOf(glm.Vec3f{0, 0, 1})
	right, _ := s.pixelOf(glm.Vec3f{1, 0, 1})
	for gap := 2; gap <= 6; gap++ {
		if open := s.linearAt(right+gap, centreY); open < 0.97 {
			t.Errorf("openness of the wall %d pixels right of the box = %.3f, want fully open (at least 0.97)", gap, open)
		}
	}
	if open := s.linearAtPoint(glm.Vec3f{0, 0, 1}); open < 0.97 {
		t.Errorf("openness of the box's face = %.3f, want fully open (at least 0.97)", open)
	}
}

// TestAmbientOcclusionLeavesBackgroundAlone: where no geometry is, nothing is occluded,
// and the background shows the clear colour as it would without occlusion.
func TestAmbientOcclusionLeavesBackgroundAlone(t *testing.T) {
	s := newAOScene(t)
	box := s.scene.NewMesh(s.r.NewBoxGeometry(1, 1, 1), matte(s.r))
	s.scene.Add(box)
	s.cam.SetPosition(glm.Vec3f{0, 1, 4})
	s.cam.LookAt(glm.Vec3f{0, 0, 0})
	s.r.SetClearColor(colors.RGBA32F{0.2, 0.4, 0.6, 1})

	s.r.SetDebugView(pix.DebugAmbientOcclusion)
	s.renderSettled()
	for _, corner := range [][2]int{{1, 1}, {s.size - 2, 1}, {1, s.size - 2}, {s.size - 2, s.size - 2}} {
		if open := s.linearAt(corner[0], corner[1]); open < 0.99 {
			t.Errorf("openness of the background at %v = %.3f, want 1", corner, open)
		}
	}
	s.r.SetDebugView(pix.DebugOff)

	s.renderShaded(false)
	without := s.r.Pixels()[:4]
	s.renderShaded(true)
	with := s.r.Pixels()[:4]
	if [4]byte(with) != [4]byte(without) {
		t.Errorf("background = %v with occlusion, %v without; want it unchanged", with, without)
	}
}

// TestAmbientOcclusionLeavesDirectLightAlone: lit by a directional light alone, the
// corner is shaded the same with occlusion as without — occlusion takes away ambient
// light, and there is none.
func TestAmbientOcclusionLeavesDirectLightAlone(t *testing.T) {
	s := newCornerScene(t)
	s.scene.SetAmbient(colors.RGB32F{}, 1)
	s.scene.AddDirectionalLight(glm.Vec3f{0, -1, -1}, colors.RGB32F{1, 1, 1}, 1)

	for _, hdr := range []bool{false, true} {
		s.r.EnableHDR(hdr)
		s.r.SetToneMapping(pix.ToneMapNone)
		s.renderShaded(false)
		without := append([]byte(nil), s.r.Pixels()...)
		s.renderShaded(true)
		with := s.r.Pixels()
		for i := range with {
			if d := int(with[i]) - int(without[i]); d < -1 || d > 1 {
				t.Fatalf("HDR %v: byte %d = %d with occlusion, %d without; want direct light unchanged", hdr, i, with[i], without[i])
			}
		}
	}
}

// TestAmbientOcclusionScalesOnlyIndirectLight: lit by ambient and directional light
// together, the corner keeps all of its direct light and the open share of its
// ambient light — direct + openness × ambient, each measured on its own.
func TestAmbientOcclusionScalesOnlyIndirectLight(t *testing.T) {
	s := newCornerScene(t)
	const ambient, sunIntensity = 0.3, 0.6
	sun := s.scene.AddDirectionalLight(glm.Vec3f{0, -1, -1}, colors.RGB32F{1, 1, 1}, sunIntensity)
	points := []glm.Vec3f{floorPoint(0.05), floorPoint(0.2), floorPoint(0.5), {0, 0.1, cornerZ}}

	s.scene.SetAmbient(colors.RGB32F{}, 1)
	s.renderShaded(false)
	direct := make([]float64, len(points))
	for i, p := range points {
		direct[i] = s.linearAtPoint(p)
	}

	sun.Intensity = 0
	s.scene.SetAmbient(colors.RGB32F{ambient, ambient, ambient}, 1)
	s.renderShaded(false)
	indirect := make([]float64, len(points))
	for i, p := range points {
		indirect[i] = s.linearAtPoint(p)
	}
	openness := make([]float64, len(points))
	for i, p := range points {
		openness[i] = s.opennessAt(p)
	}

	sun.Intensity = sunIntensity
	s.renderShaded(true)
	for i, p := range points {
		want := direct[i] + openness[i]*indirect[i]
		if got := s.linearAtPoint(p); math.Abs(got-want) > 0.03 {
			t.Errorf("shading at %v = %.3f, want direct %.3f + openness %.3f × ambient %.3f = %.3f", p, got, direct[i], openness[i], indirect[i], want)
		}
	}
}

// TestAmbientOcclusionMatchesWithMSAA: drawing the scene multisampled does not change
// how occluded its surfaces are, away from the edges multisampling smooths — among them
// the crease itself, where a pixel's samples lie on both surfaces. The camera is close
// to the corner, so that the band the wall occludes spans many pixels.
func TestAmbientOcclusionMatchesWithMSAA(t *testing.T) {
	s := newCornerScene(t)
	s.cam.SetPosition(glm.Vec3f{0, 1.5, cornerZ + 2})
	s.cam.LookAt(glm.Vec3f{0, 0, cornerZ})
	points := []glm.Vec3f{floorPoint(0.1), floorPoint(0.25), floorPoint(0.5), floorPoint(1.2), {0, 0.1, cornerZ}, {0, 0.25, cornerZ}, {0, 0.5, cornerZ}}
	single := make([]float64, len(points))
	for i, p := range points {
		single[i] = s.opennessAt(p)
	}

	s.r.SetAntiAliasing(pix.AntiAliasingMSAA4x)
	s.r.EnableAntiAliasing(true)
	for i, p := range points {
		if got := s.opennessAt(p); math.Abs(got-single[i]) > 0.02 {
			t.Errorf("openness at %v = %.3f with MSAA, %.3f without; want them equal", p, got, single[i])
		}
	}

	s.renderShaded(false)
	without := s.linearAtPoint(floorPoint(0.1))
	s.renderShaded(true)
	if with := s.linearAtPoint(floorPoint(0.1)); with >= without-0.1 {
		t.Errorf("corner shading with MSAA = %.3f with occlusion, %.3f without; want it darker", with, without)
	}
}

// TestAmbientOcclusionSkipsTransparentSurfaces: a half-transparent white pane in front of
// the corner lets through half of the corner's light, occluded or not, and adds its own,
// which occlusion leaves alone: with the pane, occlusion takes away exactly half of what
// it takes away without it.
func TestAmbientOcclusionSkipsTransparentSurfaces(t *testing.T) {
	s := newCornerScene(t)
	corner := floorPoint(0.05)

	s.renderShaded(false)
	bareOff := s.linearAtPoint(corner)
	s.renderShaded(true)
	bareOn := s.linearAtPoint(corner)

	pane := s.r.NewBasicMaterial()
	pane.SetColor(colors.RGBA32F{0.5, 0.5, 0.5, 0.5})
	pane.SetBlend(materials.BlendAlpha)
	quad := s.scene.NewMesh(s.r.NewPlaneGeometry(4, 4, 1, 1), pane)
	quad.SetRotationXYZ(math.Pi/2, 0, 0)
	quad.SetPosition(glm.Vec3f{0, 1, 0})
	s.scene.Add(quad)

	s.renderShaded(false)
	paneOff := s.linearAtPoint(corner)
	s.renderShaded(true)
	paneOn := s.linearAtPoint(corner)

	bareLoss, paneLoss := bareOff-bareOn, paneOff-paneOn
	t.Logf("occlusion takes %.3f bare, %.3f behind the pane", bareLoss, paneLoss)
	if bareLoss < 0.1 {
		t.Fatalf("occlusion takes %.3f from the bare corner, want a clear loss to compare", bareLoss)
	}
	if math.Abs(paneLoss-bareLoss/2) > 0.03 {
		t.Errorf("occlusion takes %.3f behind the pane, want half the bare %.3f", paneLoss, bareLoss)
	}
}

// TestAmbientOcclusionRadiusIsInWorldUnits: the band of floor the wall occludes is as
// wide from twice as far away — the radius is a distance in the scene, not on screen.
//
// The blur that smooths occlusion reaches a few pixels, and so twice as far across the
// floor from twice as far away, so the image is large and occlusion measured at every
// pixel, to keep the blur's reach small beside the radius's.
func TestAmbientOcclusionRadiusIsInWorldUnits(t *testing.T) {
	s := newCornerSceneOfSize(t, 512)
	s.r.SetAmbientOcclusionSettings(pix.AmbientOcclusionSettings{Radius: aoTestRadius, FullResolution: true})

	// reach is how far from the wall the floor first comes out fully open.
	reach := func() float32 {
		for d := float32(0.05); d < 3*aoTestRadius; d += 0.05 {
			if s.opennessAt(floorPoint(d)) >= 0.95 {
				return d
			}
		}
		return 3 * aoTestRadius
	}
	near := reach()
	s.cam.SetPosition(glm.Vec3f{0, 6, 9})
	s.cam.LookAt(glm.Vec3f{0, 0.5, cornerZ})
	far := reach()
	t.Logf("occlusion reaches %.2f from near, %.2f from far", near, far)
	if near >= 3*aoTestRadius {
		t.Fatalf("the floor is still occluded %v from the wall, want it open within the radius", near)
	}
	if far < near*0.7 || far > near*1.3 {
		t.Errorf("occlusion reaches %.2f from twice as far, %.2f from near; want the same distance", far, near)
	}
}

// TestAmbientOcclusionFinishesBeforeStepsAfterTheOpaqueScene: a frame step after the
// opaque scene is given it finished, occlusion included. The scene comes out the same
// with a step there that draws nothing; and what a step draws there is not darkened —
// even all of it marked as indirect light, alpha 0, which occlusion would darken.
func TestAmbientOcclusionFinishesBeforeStepsAfterTheOpaqueScene(t *testing.T) {
	for _, msaa := range []bool{false, true} {
		t.Run(map[bool]string{false: "single sample", true: "MSAA"}[msaa], func(t *testing.T) {
			s := newCornerScene(t)
			s.r.EnableAntiAliasing(msaa)
			s.renderShaded(true)
			withoutStep := append([]byte(nil), s.r.Pixels()...)

			var log []string
			step := &recordingStep{name: "after opaque", log: &log}
			s.r.AddFrameStep(pix.FrameStageAfterOpaque, step)
			s.renderShaded(true)
			withStep := s.r.Pixels()
			for i := range withStep {
				// Within a few levels: the two are rendered at different points of the
				// sixteen frames occlusion cycles its samples through.
				if d := int(withStep[i]) - int(withoutStep[i]); d < -3 || d > 3 {
					t.Fatalf("byte %d = %d with a step after the opaque scene, %d without; want them equal", i, withStep[i], withoutStep[i])
				}
			}

			step.clearColor = &[4]float32{0.5, 0.5, 0.5, 0}
			s.renderShaded(true)
			want := linearFromSRGB(byte(srgbByte(0.5)))
			if got := s.linearAtPoint(floorPoint(0.05)); math.Abs(got-want) > 0.01 {
				t.Errorf("corner after a step cleared the scene to 0.5 = %.3f, want %.3f, not darkened", got, want)
			}
		})
	}
}

// TestAmbientOcclusionIsSmooth: along the floor at one distance from the wall, the
// occlusion is the same all the way across — the wall is the same all the way across.
// What the per-pixel sampling pattern leaves as noise, the blur smooths away.
func TestAmbientOcclusionIsSmooth(t *testing.T) {
	s := newCornerScene(t)
	s.r.SetDebugView(pix.DebugAmbientOcclusion)
	s.renderSettled()
	for _, distance := range []float32{0.1, 0.3} {
		lowest, highest := 1.0, 0.0
		for x := float32(-1); x <= 1; x += 0.05 {
			open := s.linearAtPoint(glm.Vec3f{x, 0, cornerZ + distance})
			lowest, highest = min(lowest, open), max(highest, open)
		}
		if highest-lowest > 0.05 {
			t.Errorf("openness %v from the wall ranges from %.3f to %.3f across the floor, want it even within 0.05", distance, lowest, highest)
		}
	}
}

// TestAmbientOcclusionQualityReducesGrain: higher quality takes more samples and blurs
// more, so the occlusion under the corner's wall is less grainy at High than at Low, by
// either method. Grain is how far each pixel strays from the mean of the 5x5 around it.
func TestAmbientOcclusionQualityReducesGrain(t *testing.T) {
	for _, method := range []pix.AmbientOcclusion{pix.AmbientOcclusionVBAO, pix.AmbientOcclusionASSAO} {
		t.Run(method.String(), func(t *testing.T) {
			low := occlusionGrain(t, method, pix.AmbientOcclusionQualityLow)
			high := occlusionGrain(t, method, pix.AmbientOcclusionQualityHigh)
			if high > 0.85*low {
				t.Errorf("grain at high quality = %.4f, at low = %.4f, want high at most 85%% of low", high, low)
			}
		})
	}
}

// occlusionGrain renders the corner scene's occlusion, darkened enough for its grain to
// show, and returns the root mean square of each pixel's difference from the mean of the
// 5x5 around it, over the floor in front of the wall.
func occlusionGrain(t *testing.T, method pix.AmbientOcclusion, quality pix.AmbientOcclusionQuality) float64 {
	const size = 256
	s := newCornerSceneOfSize(t, size)
	s.r.SetAmbientOcclusion(method)
	s.r.SetAmbientOcclusionSettings(pix.AmbientOcclusionSettings{Radius: aoTestRadius, Intensity: 4, Quality: quality})
	s.r.SetDebugView(pix.DebugAmbientOcclusion)
	s.renderSettled()

	pixels := s.r.Pixels()
	openness := func(x, y int) float64 {
		return linearFromSRGB(pixels[(y*size+x)*4])
	}
	var sumOfSquares float64
	var count int
	for y := size / 2; y < size*7/8; y++ {
		for x := size / 4; x < size*3/4; x++ {
			var mean float64
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					mean += openness(x+dx, y+dy)
				}
			}
			mean /= 25
			difference := openness(x, y) - mean
			sumOfSquares += difference * difference
			count++
		}
	}
	return math.Sqrt(sumOfSquares / float64(count))
}

// TestAmbientOcclusionHoldsStillAsTheCameraMoves: moving the camera a centimetre moves
// the image a fraction of a pixel, and occlusion should move with it, no more. Along the
// silhouettes of pillars standing before a wall, no more pixels of the occlusion should
// change visibly than of the depth view, which shows the motion there is. Occlusion that
// crawls changes many more.
func TestAmbientOcclusionHoldsStillAsTheCameraMoves(t *testing.T) {
	s := newAOSceneOfSize(t, 256)
	s.addFloor()
	s.addWall(-8)
	for i := range 5 {
		pillar := s.scene.NewMesh(s.r.NewBoxGeometry(0.15, 4, 0.15), matte(s.r))
		pillar.SetPosition(glm.Vec3f{-2 + float32(i), 2, -3})
		s.scene.Add(pillar)
	}

	// changedPixels counts the pixels of view that change by more than a few levels
	// when the camera moves forward a centimetre.
	changedPixels := func(view pix.DebugView) int {
		s.r.SetDebugView(view)
		frame := func(z float32) []byte {
			s.cam.SetPosition(glm.Vec3f{0.3, 1.5, z})
			s.cam.LookAt(glm.Vec3f{0, 1, z - 10})
			s.renderSettled()
			return append([]byte(nil), s.r.Pixels()...)
		}
		before, after := frame(4), frame(3.99)
		changed := 0
		for i := 0; i < len(before); i += 4 {
			if d := int(before[i]) - int(after[i]); d > 8 || d < -8 {
				changed++
			}
		}
		return changed
	}
	depth := changedPixels(pix.DebugDepth)
	occlusion := changedPixels(pix.DebugAmbientOcclusion)
	t.Logf("pixels changed by a 1 cm move: %d of the depth view, %d of the occlusion", depth, occlusion)
	if occlusion > depth {
		t.Errorf("a 1 cm move changes %d pixels of the occlusion, more than the %d it changes of the depth view", occlusion, depth)
	}
}

// TestAmbientOcclusionIsTheSameFromNearAndFar: how occluded a point is belongs to the
// scene, not to the view, so a point on the floor just in front of a wall reads about
// the same seen from a metre and a half away as from eight.
func TestAmbientOcclusionIsTheSameFromNearAndFar(t *testing.T) {
	s := newCornerSceneOfSize(t, 256)
	point := floorPoint(0.05)
	lowest, highest := 1.0, 0.0
	for _, eye := range []glm.Vec3f{{0, 1, cornerZ + 1.5}, {0, 2, cornerZ + 4}, {0, 4, cornerZ + 8}} {
		s.cam.SetPosition(eye)
		s.cam.LookAt(glm.Vec3f{0, 0.3, cornerZ})
		open := s.opennessAt(point)
		t.Logf("seen from %v: %.3f", eye, open)
		lowest, highest = min(lowest, open), max(highest, open)
	}
	if highest-lowest > 0.1 {
		t.Errorf("openness of %v ranges from %.3f to %.3f with the distance it is seen from, want it within 0.1", point, lowest, highest)
	}
}

// TestAmbientOcclusionMatchesTheSkyAPointSees: a point on the floor at the foot of a
// tall wall has half its sky hidden by the wall, so it is half open — what a ray tracer
// would find — at either resolution occlusion is measured at.
func TestAmbientOcclusionMatchesTheSkyAPointSees(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "half resolution", true: "full resolution"}[full], func(t *testing.T) {
			s := newCornerSceneOfSize(t, 512)
			s.r.SetAmbientOcclusionSettings(pix.AmbientOcclusionSettings{Radius: aoTestRadius, FullResolution: full})
			s.cam.SetPosition(glm.Vec3f{0, 1.5, cornerZ + 3})
			s.cam.LookAt(glm.Vec3f{0, 0.2, cornerZ})
			if open := s.opennessAt(floorPoint(0.02)); math.Abs(open-0.5) > 0.06 {
				t.Errorf("openness at the foot of the wall = %.3f, want about half (0.5 ± 0.06)", open)
			}
		})
	}
}

// TestAmbientOcclusionSettlesWithAStillCamera: with nothing moving, occlusion settles
// and stays: each frame samples differently, but what the frames gather together does
// not keep swinging with the samples of the latest few. Measured with the occlusion
// deepened, as it is when the swing is easiest to see.
func TestAmbientOcclusionSettlesWithAStillCamera(t *testing.T) {
	s := newCornerScene(t)
	s.r.SetAmbientOcclusionSettings(pix.AmbientOcclusionSettings{Radius: aoTestRadius, Intensity: 4})
	box := s.scene.NewMesh(s.r.NewBoxGeometry(1, 1, 1), matte(s.r))
	box.SetPosition(glm.Vec3f{1.5, 0.5, -1})
	s.scene.Add(box)
	s.r.SetDebugView(pix.DebugAmbientOcclusion)
	for range 64 {
		s.r.Render(s.scene)
	}

	points := []glm.Vec3f{floorPoint(0.1), floorPoint(0.3), {1.5, 0, -0.4}, {0.95, 0.1, -1}}
	lowest := make([]float64, len(points))
	highest := make([]float64, len(points))
	for i := range points {
		lowest[i], highest[i] = 1, 0
	}
	for range 16 {
		s.r.Render(s.scene)
		for i, p := range points {
			open := s.linearAtPoint(p)
			lowest[i], highest[i] = min(lowest[i], open), max(highest[i], open)
		}
	}
	for i, p := range points {
		if highest[i]-lowest[i] > 0.01 {
			t.Errorf("openness at %v swings from %.3f to %.3f with a still camera, want it settled within 0.01", p, lowest[i], highest[i])
		}
	}
}

// TestAmbientOcclusionLeavesAFloorOpenUpClose: a bare floor seen from close by, where a
// step far out along a slice reads one of the coarse images of the depth chain, stays
// open even with the occlusion deepened four times, as it is when what little a floor
// occludes of itself would show — as contour lines, wherever steps cross from one image
// of the chain to the next.
func TestAmbientOcclusionLeavesAFloorOpenUpClose(t *testing.T) {
	s := newAOSceneOfSize(t, 512)
	s.addFloor()
	s.r.SetAmbientOcclusionSettings(pix.AmbientOcclusionSettings{Radius: 0.5, FullResolution: true, Intensity: 4})
	s.cam.SetPosition(glm.Vec3f{-1.4, 1.1, -4.4})
	s.cam.LookAt(glm.Vec3f{0, 0.6, -6.5})
	s.r.SetDebugView(pix.DebugAmbientOcclusion)
	s.renderSettled()

	// The floor fills the frame below the horizon, which is a little above the middle.
	pixels := s.r.Pixels()
	lowest := 1.0
	for y := s.size * 2 / 3; y < s.size; y++ {
		for x := range s.size {
			lowest = min(lowest, linearFromSRGB(pixels[(y*s.size+x)*4]))
		}
	}
	if lowest < 0.98 {
		t.Errorf("openness of the bare floor goes down to %.4f, want it open (at least 0.98)", lowest)
	}
}

// TestAmbientOcclusionLeavesNoTrail: occlusion follows what casts it at once — the frame
// after a box moves away, the floor where it stood is open, though the floor itself did
// not move.
func TestAmbientOcclusionLeavesNoTrail(t *testing.T) {
	s := newCornerScene(t)
	box := s.scene.NewMesh(s.r.NewBoxGeometry(1, 1, 1), matte(s.r))
	box.SetPosition(glm.Vec3f{0, 0.5, -1})
	s.scene.Add(box)
	beside := glm.Vec3f{0, 0, -0.45}
	if open := s.opennessAt(beside); open > 0.9 {
		t.Fatalf("openness beside the box = %.3f, want it occluded to measure the trail by", open)
	}

	box.SetPosition(glm.Vec3f{3, 0.5, -1})
	s.r.SetDebugView(pix.DebugAmbientOcclusion)
	s.r.Render(s.scene)
	if open := s.linearAtPoint(beside); open < 0.97 {
		t.Errorf("openness where the box stood, the frame after it moved = %.3f, want it open (at least 0.97)", open)
	}
}

// TestAmbientOcclusionASSAO: measured by ASSAO, occlusion darkens the foot of a wall
// and not the floor past the radius, nor flat floor or the background; it leaves direct
// light alone; and, having no history, nothing trails behind a box that moves away.
func TestAmbientOcclusionASSAO(t *testing.T) {
	s := newCornerSceneOfSize(t, 256)
	s.r.SetAmbientOcclusion(pix.AmbientOcclusionASSAO)

	corner, open := s.opennessAt(floorPoint(0.05)), s.opennessAt(floorPoint(2*aoTestRadius))
	if corner > 0.85 || open < 0.97 {
		t.Errorf("openness at the foot of the wall = %.3f and past the radius = %.3f, want below 0.85 and at least 0.97", corner, open)
	}
	if high := s.opennessAt(glm.Vec3f{0, 3, cornerZ}); high < 0.97 {
		t.Errorf("openness of the wall high above the floor = %.3f, want open", high)
	}

	box := s.scene.NewMesh(s.r.NewBoxGeometry(1, 1, 1), matte(s.r))
	box.SetPosition(glm.Vec3f{0, 0.5, -1})
	s.scene.Add(box)
	beside := glm.Vec3f{0, 0, -0.45}
	if near := s.opennessAt(beside); near > 0.9 {
		t.Fatalf("openness beside the box = %.3f, want it occluded", near)
	}
	box.SetPosition(glm.Vec3f{3, 0.5, -1})
	s.r.SetDebugView(pix.DebugAmbientOcclusion)
	s.r.Render(s.scene)
	if after := s.linearAtPoint(beside); after < 0.97 {
		t.Errorf("openness where the box stood, the frame after it moved = %.3f, want it open", after)
	}
	s.r.SetDebugView(pix.DebugOff)

	s.scene.SetAmbient(colors.RGB32F{}, 1)
	s.scene.AddDirectionalLight(glm.Vec3f{0, -1, -1}, colors.RGB32F{1, 1, 1}, 1)
	s.renderShaded(false)
	without := append([]byte(nil), s.r.Pixels()...)
	s.renderShaded(true)
	with := s.r.Pixels()
	for i := range with {
		if d := int(with[i]) - int(without[i]); d < -1 || d > 1 {
			t.Fatalf("byte %d = %d with occlusion, %d without; want direct light unchanged", i, with[i], without[i])
		}
	}
}

// TestAmbientOcclusionASSAOKeepsEdgesSharp: a surface in front of an occluded corner
// stays open right up to its outline, while the corner beside it stays dark. ASSAO
// measures at a fraction of the pixels and puts the measures back together for each
// pixel; taken from the wrong image, or from across the outline, they would bring the
// corner's darkness onto the surface in front.
func TestAmbientOcclusionASSAOKeepsEdgesSharp(t *testing.T) {
	for _, fullResolution := range []bool{false, true} {
		name := "HalfResolution"
		if fullResolution {
			name = "FullResolution"
		}
		t.Run(name, func(t *testing.T) {
			s := newCornerSceneOfSize(t, 256)
			s.r.SetAmbientOcclusion(pix.AmbientOcclusionASSAO)
			s.r.SetAmbientOcclusionSettings(pix.AmbientOcclusionSettings{Radius: aoTestRadius, FullResolution: fullResolution})
			// Floating halfway to the camera, well beyond the radius from anything,
			// and over the line where the floor meets the wall.
			plate := s.scene.NewMesh(s.r.NewBoxGeometry(1, 0.6, 0.1), matte(s.r))
			plate.SetPosition(glm.Vec3f{0, 1.5, 0})
			s.scene.Add(plate)

			s.r.SetDebugView(pix.DebugAmbientOcclusion)
			s.renderSettled()
			left, row := s.pixelOf(glm.Vec3f{-0.5, 1.5, 0.05})
			right, _ := s.pixelOf(glm.Vec3f{0.5, 1.5, 0.05})
			for _, outside := range []int{left - 3, right + 3} {
				if corner := s.linearAt(outside, row); corner > 0.85 {
					t.Fatalf("openness of the corner beside the plate, at x = %d = %.3f, want below 0.85", outside, corner)
				}
			}
			// Without the edges the corner reaches 0.97 two pixels in; with them the
			// plate is open from the first block of pixels wholly on it. The pixels
			// before are left out: at half resolution a block of 2x2 takes one image's
			// texel whole, from whichever side of the outline it fell on. Rows a few
			// centimetres apart put pixels in every place within their blocks; the
			// camera looks down, so the plate's sides lean, and each row finds its own.
			for height := float32(1.44); height <= 1.56; height += 0.015 {
				left, y := s.pixelOf(glm.Vec3f{-0.5, height, 0.05})
				right, _ := s.pixelOf(glm.Vec3f{0.5, height, 0.05})
				for inset := 2; inset <= 6; inset++ {
					for _, x := range []int{left + inset, right - 1 - inset} {
						if open := s.linearAt(x, y); open < 0.99 {
							t.Errorf("openness of the plate %d pixels inside its outline, at (%d, %d) = %.3f, want open (at least 0.99)", inset, x, y, open)
						}
					}
				}
			}
		})
	}
}

// TestAmbientOcclusionSeesBehindThinThings: a thin post standing a little in front of a
// wall hides only a sliver of the wall's sky. Taking every surface to be thin, VBAO
// darkens the wall behind it far less than taking every surface to run back for ever —
// a thickness far past the radius — which darkens it as a solid block would.
func TestAmbientOcclusionSeesBehindThinThings(t *testing.T) {
	behindPost := glm.Vec3f{0.12, 1.5, cornerZ}
	opennessWith := func(thickness float32) float64 {
		s := newAOScene(t)
		s.addWall(cornerZ)
		post := s.scene.NewMesh(s.r.NewBoxGeometry(0.1, 3, 0.1), matte(s.r))
		post.SetPosition(glm.Vec3f{0, 1.5, cornerZ + 0.4})
		s.scene.Add(post)
		s.cam.SetPosition(glm.Vec3f{1.5, 1.5, cornerZ + 4})
		s.cam.LookAt(glm.Vec3f{0, 1.5, cornerZ})
		s.r.SetAmbientOcclusionSettings(pix.AmbientOcclusionSettings{Radius: aoTestRadius, Thickness: thickness})
		return s.opennessAt(behindPost)
	}
	thin, solid := opennessWith(0.1), opennessWith(100)
	t.Logf("openness of the wall behind the post: thin %.3f, solid %.3f", thin, solid)
	if thin < solid+0.05 {
		t.Errorf("openness of the wall behind the post = %.3f taking surfaces as thin, %.3f as solid; want it clearly more open", thin, solid)
	}
}
