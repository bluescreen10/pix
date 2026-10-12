package render_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/render"
	"github.com/bluescreen10/pix/render/geometries"
	"github.com/bluescreen10/pix/render/materials"
	"github.com/bluescreen10/pix/render/scenes"
)

// screenPosition is where viewProj puts point on a size x size image, in pixels.
func screenPosition(viewProj glm.Mat4f, point glm.Vec3f, size int) (x, y float64) {
	clip := viewProj.Mul4x1(glm.Vec4f{point[0], point[1], point[2], 1})
	return (float64(clip[0]/clip[3]) + 1) / 2 * float64(size), (float64(clip[1]/clip[3]) + 1) / 2 * float64(size)
}

// TestTAAJittersTheProjection: TAA draws each frame from a slightly different spot within
// the pixel, so that over frames it samples the whole pixel. With the camera still, a
// point drawn by the frames' projections moves from frame to frame, by less than a pixel;
// without TAA it stays put.
func TestTAAJittersTheProjection(t *testing.T) {
	for _, isTAA := range []bool{true, false} {
		r, scene := stepScene(t)
		r.SetAntiAliasing(render.AntiAliasingTAA)
		r.EnableAntiAliasing(isTAA)
		var log []string
		step := &recordingStep{name: "depth", log: &log}
		r.AddFrameStep(render.FrameStageAfterDepth, step)
		for range 8 {
			r.Render(scene)
		}

		point := glm.Vec3f{0.3, 0.2, 0}
		firstX, firstY := screenPosition(step.frames[0].ViewProj, point, postSize)
		moved := 0
		for i, frame := range step.frames[1:] {
			x, y := screenPosition(frame.ViewProj, point, postSize)
			if math.Abs(x-firstX) >= 1 || math.Abs(y-firstY) >= 1 {
				t.Errorf("TAA %v, frame %d: point drawn at (%.3f, %.3f), %0.3f px from frame 0's (%.3f, %.3f), want under a pixel", isTAA, i+1, x, y, math.Hypot(x-firstX, y-firstY), firstX, firstY)
			}
			if x != firstX || y != firstY {
				moved++
			}
		}
		if isTAA && moved != len(step.frames)-1 {
			t.Errorf("TAA on: the point moved in %d of %d frames, want every frame", moved, len(step.frames)-1)
		}
		if !isTAA && moved != 0 {
			t.Errorf("TAA off: the point moved in %d of %d frames, want none", moved, len(step.frames)-1)
		}
	}
}

// TestTAAMeasuresMotionWithoutTheJitter: the jitter moves what is drawn by a fraction of a
// pixel each frame, but nothing moved; motion that included it would have TAA follow the
// jitter back and blur what it means to sharpen. So with everything still, the motion
// view reads exactly what it reads without TAA, frame after frame — on the box and on
// the background behind it.
func TestTAAMeasuresMotionWithoutTheJitter(t *testing.T) {
	s := newMotionScene(t)
	s.renderer.Render(s.scene)
	s.renderer.Render(s.scene)
	box, background := s.pixel(motionSceneSize/2, motionSceneSize/2), s.pixel(2, 2)

	s.renderer.SetAntiAliasing(render.AntiAliasingTAA)
	s.renderer.EnableAntiAliasing(true)
	for frame := range jitterFrames {
		s.renderer.Render(s.scene)
		if got := s.pixel(motionSceneSize/2, motionSceneSize/2); got != box {
			t.Errorf("frame %d: motion view at the box = %v, want %v as without TAA", frame, got, box)
		}
		if got := s.pixel(2, 2); got != background {
			t.Errorf("frame %d: motion view at the background = %v, want %v as without TAA", frame, got, background)
		}
	}
}

// jitterFrames is enough frames for the jitter to visit every spot it draws from.
const jitterFrames = 8

// stripeBars is ten white bars, 2 pixels wide and 4 apart in the motion scene, from x = 0
// rightward, a unit tall and centred on y = 0.
func stripeBars() geometries.GeometryConfig {
	const barWidth, barSpacing = 2.0 / pixelsPerUnit, 4.0 / pixelsPerUnit
	var positions []glm.Vec3f
	var indices []uint32
	for bar := range 10 {
		left := float32(bar) * barSpacing
		first := uint32(len(positions))
		positions = append(positions, glm.Vec3f{left, -0.5, 0}, glm.Vec3f{left + barWidth, -0.5, 0}, glm.Vec3f{left + barWidth, 0.5, 0}, glm.Vec3f{left, 0.5, 0})
		indices = append(indices, first, first+1, first+2, first, first+2, first+3)
	}
	return geometries.GeometryConfig{
		Attributes: []geometries.Attribute{geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, positions)},
		Indices:    indices,
	}
}

// TestTAAFollowsWhatMoves moves white bars 2 pixels right every frame, half their
// spacing: last frame's image under each bar is the gap between bars. Read where each
// pixel's surface was, the history matches the frame and the bars stay crisp; read from
// the same pixel, it is the bars inverted, which blends them toward grey.
//
// The bars are as narrow as they are so that every pixel has both colours around it:
// around a pixel with one colour all about it, TAA clips any history to that colour,
// which would hide history read from the wrong place.
func TestTAAFollowsWhatMoves(t *testing.T) {
	for _, isHDR := range []bool{false, true} {
		s := newMotionScene(t)
		s.renderer.SetDebugView(render.DebugOff)
		s.renderer.EnableHDR(isHDR)
		s.renderer.SetToneMapping(render.ToneMapNone)
		s.renderer.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
		s.renderer.SetAntiAliasing(render.AntiAliasingTAA)
		s.renderer.EnableAntiAliasing(true)
		s.box.SetPosition(glm.Vec3f{0, 50, 0})
		geo := s.renderer.GeometryStore.Create(stripeBars())
		t.Cleanup(geo.Release)
		white := s.renderer.NewBasicMaterial()
		white.SetColor(colors.RGBA32F{1, 1, 1, 1})
		bars := s.scene.NewMesh(geo, white)

		// The bars start at the left of the frame, and move 2 pixels a frame.
		const steps = 8
		for step := range steps {
			bars.SetPosition(glm.Vec3f{-1 + float32(2*step)/pixelsPerUnit, 0, 0})
			s.renderer.Render(s.scene)
		}

		// The fourth bar's left edge: a pixel of the bar, and of the gap after it.
		left := 2*(steps-1) + 12
		bar, gap := left, left+2
		pixels := s.renderer.Pixels()
		at := func(x int) byte {
			return pixels[(motionSceneSize/2*motionSceneSize+x)*4]
		}
		if got := at(bar); got < 240 {
			t.Errorf("HDR %v: inside a bar = %d, want white (at least 240)", isHDR, got)
		}
		if got := at(gap); got > 15 {
			t.Errorf("HDR %v: inside a gap = %d, want black (at most 15)", isHDR, got)
		}
	}
}

// TestTAALeavesNoTrail moves a white box 8 pixels right every frame over black. Where it
// was two frames ago, the history still holds white, but nothing around that pixel is
// white any more: TAA must trust the frame there, not trail the box behind it.
func TestTAALeavesNoTrail(t *testing.T) {
	s := newMotionScene(t)
	s.renderer.SetDebugView(render.DebugOff)
	s.renderer.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
	s.renderer.SetAntiAliasing(render.AntiAliasingTAA)
	s.renderer.EnableAntiAliasing(true)
	white := s.renderer.NewBasicMaterial()
	white.SetColor(colors.RGBA32F{1, 1, 1, 1})
	s.box.SetMaterial(white)

	// The box is 32 pixels wide; from 16 pixels left of centre it moves 8 a frame.
	const steps = 4
	for step := range steps {
		s.box.SetPosition(glm.Vec3f{float32(-16+8*step) / pixelsPerUnit, 0, 0})
		s.renderer.Render(s.scene)
	}

	// The box's left edge is now at 8*(steps-1) pixels; two frames ago it was 16 pixels
	// further left.
	left := 8 * (steps - 1)
	pixels := s.renderer.Pixels()
	for x := left - 16; x < left-1; x++ {
		if got := pixels[(motionSceneSize/2*motionSceneSize+x)*4]; got > 15 {
			t.Errorf("pixel %d, behind the box = %d, want black (at most 15)", x, got)
		}
	}
}

// TestTAAContinuesAcrossAnHDRChange: TAA's history is in the format of the image it
// resolves, which turning HDR on or off changes. Its images are made again, and it goes
// on smoothing edges — and with HDR on keeps light brighter than white, which a history
// left in the display's format would cut down to white. The quad is four times white,
// and Reinhard tone mapping shows four and one differently.
func TestTAAContinuesAcrossAnHDRChange(t *testing.T) {
	r, err := render.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
	r.SetToneMapping(render.ToneMapReinhard)
	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	quad := r.GeometryStore.Create(render.PlaneGeometry(1, 1, 1, 1))
	t.Cleanup(quad.Release)
	bright := r.NewBasicMaterial()
	bright.SetColor(colors.RGBA32F{4, 4, 4, 1})
	bright.SetCull(materials.CullNone)
	mesh := scene.NewMesh(quad, bright)
	mesh.SetRotationXYZ(math.Pi/2, 0, 0.35)
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 2})

	want := map[bool][3]byte{}
	for _, isHDR := range []bool{false, true} {
		r.EnableHDR(isHDR)
		r.Render(scene)
		want[isHDR] = pixelAt(r, postSize/2, postSize/2)
	}

	r.SetAntiAliasing(render.AntiAliasingTAA)
	r.EnableAntiAliasing(true)
	for _, isHDR := range []bool{false, true, false} {
		r.EnableHDR(isHDR)
		for range jitterFrames {
			r.Render(scene)
		}
		if blended := pixelsBetween(r, 0, want[isHDR][1]); blended == 0 {
			t.Errorf("HDR %v: pixels between the background and the quad = 0, want the quad's edges blended", isHDR)
		}
		if got := pixelAt(r, postSize/2, postSize/2); got != want[isHDR] {
			t.Errorf("HDR %v: centre = %v, want %v as without TAA", isHDR, got, want[isHDR])
		}
	}
}

// pixelsBetween counts the pixels whose green lies strictly between low and high: in a
// scene of two flat colours, the pixels an edge crosses.
func pixelsBetween(r *render.Renderer, low, high byte) int {
	pixels := r.Pixels()
	count := 0
	for i := 0; i < len(pixels); i += 4 {
		if green := pixels[i+1]; green > low && green < high {
			count++
		}
	}
	return count
}
