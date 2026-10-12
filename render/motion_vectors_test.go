package render_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/render"
	"github.com/bluescreen10/pix/render/geometries"
	"github.com/bluescreen10/pix/render/materials"
	"github.com/bluescreen10/pix/render/postprocess"
	"github.com/bluescreen10/pix/render/scenes"
	"github.com/bluescreen10/pix/render/shaders"
)

// motionScene is a unit box in front of an orthographic camera that frames two world
// units across motionSceneSize pixels, so a world offset converts to pixels exactly.
type motionScene struct {
	renderer *render.Renderer
	scene    *scenes.Scene
	box      scenes.Mesh
	camera   scenes.Camera
	// size is the frame's width and height in pixels.
	size int
}

const (
	motionSceneSize = 64
	pixelsPerUnit   = motionSceneSize / 2
)

func newMotionScene(t *testing.T) motionScene {
	t.Helper()
	return newMotionSceneOfSize(t, motionSceneSize)
}

// newMotionSceneOfSize is the motion scene in a size x size frame, the camera still
// framing two world units across it.
func newMotionSceneOfSize(t *testing.T, size int) motionScene {
	t.Helper()
	r, err := render.NewOffscreenRenderer(uint32(size), uint32(size))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)

	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1)

	box := scene.NewMesh(r.GeometryStore.Create(render.BoxGeometry(1, 1, 1)), r.NewBasicMaterial())
	cam := scene.NewOrthographicCamera(-1, 1, -1, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})

	r.SetDebugView(render.DebugMotion)
	return motionScene{renderer: r, scene: scene, box: box, camera: cam, size: size}
}

// motionAt decodes the motion view at a pixel: how far, in pixels, what is drawn there
// moved since the previous frame — x rightward and y downward, as the image is laid out.
// The view stores it in red and green as 0.5 + motion/64, as displayed.
func (s motionScene) motionAt(x, y int) (motionX, motionY float64) {
	i := (y*s.size + x) * 4
	px := s.renderer.Pixels()[i : i+2]
	return (float64(px[0])/255 - 0.5) * 64, (float64(px[1])/255 - 0.5) * 64
}

// pixel is the motion view's red and green bytes at a pixel, as stored.
func (s motionScene) pixel(x, y int) [2]byte {
	i := (y*s.size + x) * 4
	pixels := s.renderer.Pixels()
	return [2]byte{pixels[i], pixels[i+1]}
}

// TestMotionViewShowsHowFarABoxMoved moves a box 8 pixels right between two frames: the
// motion view must read 8 pixels right, and none down, where the box is drawn.
func TestMotionViewShowsHowFarABoxMoved(t *testing.T) {
	s := newMotionScene(t)
	s.renderer.Render(s.scene)

	s.box.SetPosition(glm.Vec3f{8.0 / pixelsPerUnit, 0, 0})
	s.renderer.Render(s.scene)

	motionX, motionY := s.motionAt(motionSceneSize/2, motionSceneSize/2)
	if math.Abs(motionX-8) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the box = (%.2f, %.2f) px, want (8, 0)", motionX, motionY)
	}
}

// TestMotionViewShowsAStoppedBoxStill moves a box once, then renders again without
// moving it: on that frame it did not move, so the view must read no motion — not the
// move from the frame before, carried along because nothing rewrote the matrices.
func TestMotionViewShowsAStoppedBoxStill(t *testing.T) {
	s := newMotionScene(t)
	s.renderer.Render(s.scene)

	s.box.SetPosition(glm.Vec3f{8.0 / pixelsPerUnit, 0, 0})
	s.renderer.Render(s.scene)
	s.renderer.Render(s.scene)

	motionX, motionY := s.motionAt(motionSceneSize/2, motionSceneSize/2)
	if math.Abs(motionX) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the box = (%.2f, %.2f) px, want (0, 0)", motionX, motionY)
	}
}

// TestMotionViewMeasuresDownward moves a box 8 pixels up the screen: the view measures
// motion downward, as the image is laid out, so it must read -8.
func TestMotionViewMeasuresDownward(t *testing.T) {
	s := newMotionScene(t)
	s.renderer.Render(s.scene)

	s.box.SetPosition(glm.Vec3f{0, 8.0 / pixelsPerUnit, 0})
	s.renderer.Render(s.scene)

	motionX, motionY := s.motionAt(motionSceneSize/2, motionSceneSize/2)
	if math.Abs(motionX) > 0.5 || math.Abs(motionY+8) > 0.5 {
		t.Errorf("motion at the box = (%.2f, %.2f) px, want (0, -8)", motionX, motionY)
	}
}

// TestMotionViewIncludesTheCamerasMotion moves the camera 4 pixels right past a box that
// stays put: on screen the box moved 4 pixels left.
func TestMotionViewIncludesTheCamerasMotion(t *testing.T) {
	s := newMotionScene(t)
	s.renderer.Render(s.scene)

	s.camera.SetPosition(glm.Vec3f{4.0 / pixelsPerUnit, 0, 3})
	s.renderer.Render(s.scene)

	motionX, motionY := s.motionAt(motionSceneSize/2, motionSceneSize/2)
	if math.Abs(motionX+4) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the box = (%.2f, %.2f) px, want (-4, 0)", motionX, motionY)
	}
}

// TestMotionViewKeepsInstancesStillWhenANodeIsAdded adds a node between two frames to a
// scene with instances that never move. Instance transforms are laid out after every
// node's, so adding one moves them all along by a slot; read from where they sat the
// frame before, the front instance would take its previous matrix from the one behind
// it, and appear to move by the 8 pixels between them.
func TestMotionViewKeepsInstancesStillWhenANodeIsAdded(t *testing.T) {
	s := newMotionScene(t)
	s.box.SetPosition(glm.Vec3f{0, 5, 0})
	front := glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatfIdentity, glm.Vec3f{0, 0, 0})
	behind := glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatfIdentity, glm.Vec3f{8.0 / pixelsPerUnit, 0, -2})
	s.scene.NewInstancedMesh(s.box.Geometry(), s.box.Material(), []glm.Mat4f{front, behind})
	s.renderer.Render(s.scene)

	s.scene.NewGroup()
	s.renderer.Render(s.scene)

	motionX, motionY := s.motionAt(motionSceneSize/2, motionSceneSize/2)
	if math.Abs(motionX) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the front instance = (%.2f, %.2f) px, want (0, 0)", motionX, motionY)
	}
}

// TestFrameStepsSeeSceneMotionOnceDepthIsFilled: with motion vectors on, the depth
// prepass measures them, so a step sees SceneMotion from FrameStageAfterDepth on and
// not before; with them off, no step sees it.
func TestFrameStepsSeeSceneMotionOnceDepthIsFilled(t *testing.T) {
	for _, isEnabled := range []bool{true, false} {
		r, scene := stepScene(t)
		r.EnableMotionVectors(isEnabled)
		var log []string
		steps := map[render.FrameStage]*recordingStep{}
		for _, stage := range []render.FrameStage{render.FrameStageStart, render.FrameStageAfterShadows, render.FrameStageAfterDepth, render.FrameStageAfterOpaque, render.FrameStageAfterTransparent} {
			steps[stage] = &recordingStep{name: stageName(stage), log: &log}
			r.AddFrameStep(stage, steps[stage])
		}

		r.Render(scene)

		for stage, step := range steps {
			got, want := step.frames[0].SceneMotion.IsValid(), isEnabled && stage >= render.FrameStageAfterDepth
			if got != want {
				t.Errorf("motion vectors enabled %v, %s: SceneMotion.IsValid() = %v, want %v", isEnabled, stageName(stage), got, want)
			}
		}
	}
}

// TestMotionViewMeasuresMaskedMaterials moves a box whose material is masked — cut out
// where its alpha is low, which here is nowhere — 8 pixels right. Masked materials are
// drawn into depth by a pipeline of their own, which has to measure motion too.
func TestMotionViewMeasuresMaskedMaterials(t *testing.T) {
	s := newMotionScene(t)
	masked := s.renderer.NewBasicMaterial()
	masked.SetAlphaCutoff(0.5)
	s.box.SetMaterial(masked)
	s.renderer.Render(s.scene)

	s.box.SetPosition(glm.Vec3f{8.0 / pixelsPerUnit, 0, 0})
	s.renderer.Render(s.scene)

	motionX, motionY := s.motionAt(motionSceneSize/2, motionSceneSize/2)
	if math.Abs(motionX-8) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the masked box = (%.2f, %.2f) px, want (8, 0)", motionX, motionY)
	}
}

// TestMotionViewMeasuresUnderMSAA moves a box 8 pixels right with MSAA on. The prepass
// then fills multisampled depth, and motion has to be measured alongside it and
// resolved to one value a pixel.
func TestMotionViewMeasuresUnderMSAA(t *testing.T) {
	s := newMotionScene(t)
	s.renderer.SetAntiAliasing(render.AntiAliasingMSAA4x)
	s.renderer.EnableAntiAliasing(true)
	s.renderer.Render(s.scene)

	s.box.SetPosition(glm.Vec3f{8.0 / pixelsPerUnit, 0, 0})
	s.renderer.Render(s.scene)

	motionX, motionY := s.motionAt(motionSceneSize/2, motionSceneSize/2)
	if math.Abs(motionX-8) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the box = (%.2f, %.2f) px, want (8, 0)", motionX, motionY)
	}
}

// motionRecordingStep is a post-processing step that notes whether it was given the
// frame's motion, and otherwise passes the image through a small blur.
type motionRecordingStep struct {
	postprocess.ShaderStep
	sawSceneMotion bool
}

func (s *motionRecordingStep) Encode(frame *postprocess.Frame, cmd gpu.CommandBuffer) {
	s.sawSceneMotion = frame.SceneMotion.IsValid()
	s.ShaderStep.Encode(frame, cmd)
}

// TestPostProcessingSeesSceneMotionWhenEnabled: an effect that blurs along motion runs
// in the post-processing chain, so the chain gets the motion whenever it is measured.
func TestPostProcessingSeesSceneMotionWhenEnabled(t *testing.T) {
	for _, isEnabled := range []bool{true, false} {
		s := newMotionScene(t)
		s.renderer.SetDebugView(render.DebugOff)
		s.renderer.EnableHDR(true)
		s.renderer.EnableMotionVectors(isEnabled)
		step := &motionRecordingStep{ShaderStep: postprocess.ShaderStep{Fragment: shaders.BloomDownsample, Params: make([]byte, 4)}}
		s.renderer.SetPostProcessing([]postprocess.Step{step})

		s.renderer.Render(s.scene)

		if step.sawSceneMotion != isEnabled {
			t.Errorf("motion vectors enabled %v: SceneMotion.IsValid() = %v, want %v", isEnabled, step.sawSceneMotion, isEnabled)
		}
	}
}

// TestMotionViewMovesTheBackgroundWithTheCamera moves the camera 4 pixels right: where
// nothing is drawn, what shows — the far plane — moved 4 pixels left on screen, as the
// box did.
func TestMotionViewMovesTheBackgroundWithTheCamera(t *testing.T) {
	s := newMotionScene(t)
	s.renderer.Render(s.scene)

	s.camera.SetPosition(glm.Vec3f{4.0 / pixelsPerUnit, 0, 3})
	s.renderer.Render(s.scene)

	motionX, motionY := s.motionAt(2, 2)
	if math.Abs(motionX+4) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the background = (%.2f, %.2f) px, want (-4, 0)", motionX, motionY)
	}
}

// TestMotionViewTurnsTheBackgroundWithAPerspectiveCamera turns a perspective camera
// left on the spot, in an empty scene. What is now at the centre of the frame was the
// angle turned to the left of it, tan(angle)/tan(fov/2) of half the frame away, so it
// moved right by that much.
func TestMotionViewTurnsTheBackgroundWithAPerspectiveCamera(t *testing.T) {
	const fovDegrees, angle = 60.0, 0.1
	s := newMotionScene(t)
	s.box.SetPosition(glm.Vec3f{0, 50, 0})
	cam := s.scene.NewPerspectiveCamera(fovDegrees, 1, 0.1, 100)
	s.camera.Destroy()
	s.renderer.Render(s.scene)

	cam.SetRotationQuat(glm.NewQuat(angle, glm.Vec3f{0, 1, 0}))
	s.renderer.Render(s.scene)

	want := math.Tan(angle) / math.Tan(fovDegrees/2*math.Pi/180) * motionSceneSize / 2
	motionX, motionY := s.motionAt(motionSceneSize/2, motionSceneSize/2)
	if math.Abs(motionX-want) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the background = (%.2f, %.2f) px, want (%.2f, 0)", motionX, motionY, want)
	}
}

// TestMotionViewMovesTheBackgroundUnderMSAA is TestMotionViewMovesTheBackgroundWithTheCamera
// with MSAA on, where the background is measured into the samples beside the depth's.
func TestMotionViewMovesTheBackgroundUnderMSAA(t *testing.T) {
	s := newMotionScene(t)
	s.renderer.SetAntiAliasing(render.AntiAliasingMSAA4x)
	s.renderer.EnableAntiAliasing(true)
	s.renderer.Render(s.scene)

	s.camera.SetPosition(glm.Vec3f{4.0 / pixelsPerUnit, 0, 3})
	s.renderer.Render(s.scene)

	motionX, motionY := s.motionAt(2, 2)
	if math.Abs(motionX+4) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the background = (%.2f, %.2f) px, want (-4, 0)", motionX, motionY)
	}
}

// newMorphingSquare adds to the motion scene a 16-pixel square whose one morph target
// moves it 8 pixels right, drawn in place of the box.
func (s motionScene) newMorphingSquare(t *testing.T) scenes.Mesh {
	t.Helper()
	s.box.SetPosition(glm.Vec3f{0, 50, 0})
	shift := glm.Vec3f{8.0 / pixelsPerUnit, 0, 0}
	geo := s.renderer.GeometryStore.Create(morphSquare(geometries.MorphTarget{
		Name: "right", PositionDeltas: []glm.Vec3f{shift, shift, shift, shift},
	}))
	t.Cleanup(geo.Release)
	return s.scene.NewMesh(geo, s.renderer.NewBasicMaterial())
}

// TestMotionViewMeasuresMorphTargets sets a morph target that moves a square 8 pixels
// right: its vertices are computed every frame rather than moved by a matrix, so their
// previous positions have to be kept too.
func TestMotionViewMeasuresMorphTargets(t *testing.T) {
	s := newMotionScene(t)
	square := s.newMorphingSquare(t)
	s.renderer.Render(s.scene)

	square.SetMorphTargetWeight(0, 1)
	s.renderer.Render(s.scene)

	// 4 pixels right of centre, which the square covers before and after.
	motionX, motionY := s.motionAt(motionSceneSize/2+4, motionSceneSize/2)
	if math.Abs(motionX-8) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the square = (%.2f, %.2f) px, want (8, 0)", motionX, motionY)
	}
}

// TestMotionViewShowsAStoppedMorphStill sets a morph target, then renders twice more
// without changing it. A morph whose weights did not change is not computed again, but
// on the first frame after the change its previous positions still trail its output,
// and must be brought up to it.
func TestMotionViewShowsAStoppedMorphStill(t *testing.T) {
	s := newMotionScene(t)
	square := s.newMorphingSquare(t)
	s.renderer.Render(s.scene)

	square.SetMorphTargetWeight(0, 1)
	s.renderer.Render(s.scene)
	s.renderer.Render(s.scene)

	motionX, motionY := s.motionAt(motionSceneSize/2+4, motionSceneSize/2)
	if math.Abs(motionX) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the square = (%.2f, %.2f) px, want (0, 0)", motionX, motionY)
	}
}

// TestMotionViewMeasuresSkinnedMeshes moves the root bone of a skinned quad 8 pixels
// right. The quad's own transform stays put: the bone moves its vertices, which the
// deform pass computes.
func TestMotionViewMeasuresSkinnedMeshes(t *testing.T) {
	s := newMotionScene(t)
	s.box.SetPosition(glm.Vec3f{0, 50, 0})
	geo := s.renderer.GeometryStore.Create(riggedQuad())
	t.Cleanup(geo.Release)
	material := s.renderer.NewBasicMaterial()
	material.SetCull(materials.CullNone)
	skeleton := s.scene.NewSkeleton(twoBoneSkeleton())
	s.scene.NewSkinnedMesh(geo, material, skeleton)
	s.renderer.Render(s.scene)

	skeleton.Bone(0).SetPosition(glm.Vec3f{8.0 / pixelsPerUnit, 0, 0})
	s.renderer.Render(s.scene)

	// Just above the quad's base, which it covers before and after.
	motionX, motionY := s.motionAt(motionSceneSize/2+4, motionSceneSize/2-4)
	if math.Abs(motionX-8) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the quad = (%.2f, %.2f) px, want (8, 0)", motionX, motionY)
	}
}

// TestMotionViewMeasuresMorphTargetsAtACoarserLevel draws the morphing square at a
// coarser level of detail, made from its geometry so that it draws the same, morphed,
// vertices: its previous positions are the square's.
func TestMotionViewMeasuresMorphTargetsAtACoarserLevel(t *testing.T) {
	s := newMotionScene(t)
	// Another morphing mesh, out of view and morphed already, deformed first: the square's
	// previous positions then start somewhere other than the beginning, and differ from
	// the ones there.
	decoy := s.newMorphingSquare(t)
	decoy.SetPosition(glm.Vec3f{0, 50, 0})
	decoy.SetMorphTargetWeight(0, 1)
	square := s.newMorphingSquare(t)
	coarser := square.Geometry().CreateLOD([]uint32{0, 1, 2, 0, 2, 3, 0, 2, 1, 0, 3, 2})
	t.Cleanup(coarser.Release)
	// The camera is 3 units away, so a level from 1 unit on is the one drawn.
	square.AddLOD(coarser, square.Material(), 1)
	s.renderer.Render(s.scene)

	square.SetMorphTargetWeight(0, 1)
	s.renderer.Render(s.scene)

	motionX, motionY := s.motionAt(motionSceneSize/2+4, motionSceneSize/2)
	if math.Abs(motionX-8) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the square = (%.2f, %.2f) px, want (8, 0)", motionX, motionY)
	}
}

// TestMotionViewKeepsAMorphStillAcrossAGeometryStoreGrow holds a morphed square still
// while the geometry store grows, which discards every deform output's contents. They
// are computed again, and the square's previous positions with them: copied from the
// discarded output instead, the square would appear to jump.
func TestMotionViewKeepsAMorphStillAcrossAGeometryStoreGrow(t *testing.T) {
	s := newMotionScene(t)
	square := s.newMorphingSquare(t)
	square.SetMorphTargetWeight(0, 1)
	for range 3 {
		s.renderer.Render(s.scene)
	}

	layoutRevision := s.renderer.GeometryStore.LayoutRevision()
	// 4 MiB of positions, more than the position stream holds after the renderer's own
	// geometry.
	large := s.renderer.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, make([]glm.Vec3f, 1<<18)),
		},
	})
	t.Cleanup(large.Release)
	if s.renderer.GeometryStore.LayoutRevision() == layoutRevision {
		t.Fatal("creating 4 MiB of positions did not grow the geometry store; the test needs a grow")
	}
	s.renderer.Render(s.scene)

	motionX, motionY := s.motionAt(motionSceneSize/2+4, motionSceneSize/2)
	if math.Abs(motionX) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the square = (%.2f, %.2f) px, want (0, 0)", motionX, motionY)
	}
}

// TestMotionViewShowsAFewPixelsOnALargeFrame moves a box 4 pixels right in a 1024-pixel
// frame: a frame's worth of motion as an application sees it. The view shows motion in
// pixels, so it reads as plainly as on a small frame — measured in frame widths it would
// be under a byte from no motion at all, and the view all one grey.
func TestMotionViewShowsAFewPixelsOnALargeFrame(t *testing.T) {
	const size = 1024
	s := newMotionSceneOfSize(t, size)
	s.renderer.Render(s.scene)

	s.box.SetPosition(glm.Vec3f{4.0 / (size / 2), 0, 0})
	s.renderer.Render(s.scene)

	motionX, motionY := s.motionAt(size/2, size/2)
	if math.Abs(motionX-4) > 0.5 || math.Abs(motionY) > 0.5 {
		t.Errorf("motion at the box = (%.2f, %.2f) px, want (4, 0)", motionX, motionY)
	}
}

// TestMotionViewIsSteadyForSteadyMotion moves the box 3 pixels right and the camera 2
// pixels left every frame, for many frames, with TAA's jitter on and off: the motion the
// view reads must be the same on every frame — the box's 5 pixels, and the background's
// 2. Any frame-to-frame change in it is then the application moving things unevenly.
func TestMotionViewIsSteadyForSteadyMotion(t *testing.T) {
	for _, isTAA := range []bool{false, true} {
		s := newMotionScene(t)
		s.renderer.SetAntiAliasing(render.AntiAliasingTAA)
		s.renderer.EnableAntiAliasing(isTAA)
		for frame := range 40 {
			s.box.SetPosition(glm.Vec3f{float32(3*frame) / pixelsPerUnit, 0, 0})
			s.camera.SetPosition(glm.Vec3f{-float32(2*frame) / pixelsPerUnit, 0, 3})
			s.renderer.Render(s.scene)
			if frame == 0 {
				continue
			}

			// The box's centre is at 32 + 5*frame pixels, so it leaves the frame by
			// frame 6; sample it while it is in view.
			if centre := motionSceneSize/2 + 5*frame; centre < motionSceneSize-2 {
				if x, y := s.motionAt(centre, motionSceneSize/2); math.Abs(x-5) > 0.5 || math.Abs(y) > 0.5 {
					t.Errorf("TAA %v, frame %d: motion at the box = (%.2f, %.2f) px, want (5, 0)", isTAA, frame, x, y)
				}
			}
			if x, y := s.motionAt(1, 1); math.Abs(x-2) > 0.5 || math.Abs(y) > 0.5 {
				t.Errorf("TAA %v, frame %d: motion at the background = (%.2f, %.2f) px, want (2, 0)", isTAA, frame, x, y)
			}
		}
	}
}
