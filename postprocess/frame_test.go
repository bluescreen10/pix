package postprocess_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/postprocess"
	"github.com/bluescreen10/pix/shaders"
)

//go:generate go run ../cmd/shadercompile -i testdata/scene_depth.frag.glsl -o spv:testdata/build/scene_depth.frag.spv -o metallib:testdata/build/scene_depth.frag.metalbin

// depthStep keeps the frames it is given and draws the scene's depth into the chain's
// image, where it reads as colour: bloom's downsampling shader filters whatever image
// its root points at, and this step points it at depth.
type depthStep struct {
	pass   *postprocess.FullscreenPass
	frames []postprocess.Frame
}

func (s *depthStep) Encode(frame *postprocess.Frame, cmd gpu.CommandBuffer) {
	s.frames = append(s.frames, *frame)
	if s.pass == nil {
		s.pass = postprocess.NewFullscreenPass(frame.Backend, postprocess.FullscreenPassDescriptor{Fragment: shaders.BloomDownsample, Label: "depth"})
	}
	root := frame.Root(frame.SceneDepth, frame.Width, frame.Height)
	s.pass.Draw(frame.Target, frame.Width, frame.Height, gpu.LoadClear, root.With(make([]byte, 4)), cmd)
}

func (s *depthStep) Release() {
	if s.pass != nil {
		s.pass.Release()
		s.pass = nil
	}
}

// TestStepReadsSceneDepth: a step can sample the depth the scene was drawn with. The
// quad is black, so nothing of the scene's colour can pass for depth: the centre shows
// the quad's depth, and the empty corner the cleared far plane, which is 0.
func TestStepReadsSceneDepth(t *testing.T) {
	r, scene := postScene(t, colors.RGBA32F{0, 0, 0, 1}, 0.2)
	r.EnableHDR(true)
	r.AddPostProcessingStep(&depthStep{})

	r.Render(scene)

	if center := pixelAt(r, postSize/2, postSize/2); center[0] == 0 {
		t.Errorf("center = %v, want the quad's depth in red", center)
	}
	if corner := pixelAt(r, 0, 0); corner != [3]byte{} {
		t.Errorf("corner = %v, want black, the far plane's depth of 0", corner)
	}
}

// TestShaderStepReadsSceneDepth: the root every post-processing pass is pushed carries
// the scene's depth, so a ShaderStep — whose Params are written before any frame says
// where depth is — can still read it. The test shader draws depth as grey.
func TestShaderStepReadsSceneDepth(t *testing.T) {
	r, scene := postScene(t, colors.RGBA32F{0, 0, 0, 1}, 0.2)
	r.EnableHDR(true)
	r.AddPostProcessingStep(&postprocess.ShaderStep{Fragment: testShader(t, r, "scene_depth.frag"), Label: "scene depth"})

	r.Render(scene)

	if center := pixelAt(r, postSize/2, postSize/2); center[0] == 0 {
		t.Errorf("center = %v, want the quad's depth as grey", center)
	}
	if corner := pixelAt(r, 0, 0); corner != [3]byte{} {
		t.Errorf("corner = %v, want black, the far plane's depth of 0", corner)
	}
}

// testShader returns one of this package's test shaders, compiled for the renderer's
// backend (see the go:generate line above).
func testShader(t *testing.T, r *pix.Renderer, name string) []byte {
	t.Helper()
	extension := ".spv"
	if backend, ok := r.Backend().(interface{ ShaderFormat() string }); ok && backend.ShaderFormat() == "metal" {
		extension = ".metalbin"
	}
	code, err := os.ReadFile(filepath.Join("testdata", "build", name+extension))
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// TestStepIsGivenTheCamera: a step is told where the camera is, and the matrices the
// scene was drawn with — ViewProj takes the point the camera looks at to the centre of
// clip space, and InverseViewProj takes it back.
func TestStepIsGivenTheCamera(t *testing.T) {
	r, scene := postScene(t, colors.RGBA32F{1, 1, 1, 1}, 0.2)
	r.EnableHDR(true)
	step := &depthStep{}
	r.AddPostProcessingStep(step)

	r.Render(scene)

	frame := step.frames[0]
	if want := (glm.Vec3f{0, 0, 2}); frame.Eye != want {
		t.Errorf("Eye = %v, want %v", frame.Eye, want)
	}

	clip := frame.ViewProj.Mul4x1(glm.Vec4f{0, 0, 0, 1})
	if clip[3] == 0 || !isNear(clip[0]/clip[3], 0) || !isNear(clip[1]/clip[3], 0) {
		t.Errorf("ViewProj takes the origin to clip %v, want the centre of the view", clip)
	}
	world := frame.InverseViewProj.Mul4x1(clip)
	if want := (glm.Vec4f{0, 0, 0, 1}); !isNear(world[0]/world[3], want[0]) || !isNear(world[1]/world[3], want[1]) || !isNear(world[2]/world[3], want[2]) {
		t.Errorf("InverseViewProj takes clip %v to %v, want the origin back", clip, world)
	}
}

// isNear reports whether got is within float32 rounding of want.
func isNear(got, want float32) bool {
	return math.Abs(float64(got-want)) < 1e-5
}
