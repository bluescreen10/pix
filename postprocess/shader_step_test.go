package postprocess_test

import (
	"encoding/binary"
	"slices"
	"testing"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/postprocess"
	"github.com/bluescreen10/pix/shaders"
)

// blurStep is an application effect: bloom's downsampling shader run at full size, which
// makes it a small blur. firstLevel is the shader's own field; 1 turns on its average
// that damps the brightest pixels.
func blurStep(firstLevel uint32) *postprocess.ShaderStep {
	return &postprocess.ShaderStep{
		Fragment: shaders.BloomDownsample,
		Params:   binary.LittleEndian.AppendUint32(nil, firstLevel),
		Label:    "blur",
	}
}

// TestShaderStepRunsTheApplicationsShader: an application's effect is its fragment shader
// and its parameters, laid out after the renderer's own fields. Blurring a bright quad
// must spread its light just past its edge, and the shader's own parameter must reach it.
func TestShaderStepRunsTheApplicationsShader(t *testing.T) {
	r, scene, cam := postScene(t, colors.RGBA32F{50, 50, 50, 1}, 0.2)
	r.EnableHDR(true)
	// The quad's right edge is near x = 59; the blur reaches two texels.
	x, y := 61, postSize/2

	r.Render(scene, cam)
	if dark := pixelAt(r, x, y); dark != [3]byte{} {
		t.Fatalf("without the step the pixel past the quad's edge = %v, want black", dark)
	}

	r.SetPostProcessing([]postprocess.Step{blurStep(0)})
	r.Render(scene, cam)
	blurred := append([]byte(nil), r.Pixels()...)
	if glow := pixelAt(r, x, y); glow == [3]byte{} {
		t.Fatal("with the step the pixel past the quad's edge is still black, want the blur to reach it")
	}

	r.SetPostProcessing([]postprocess.Step{blurStep(1)})
	r.Render(scene, cam)
	if slices.Equal(blurred, r.Pixels()) {
		t.Error("firstLevel 1 drew the same image as firstLevel 0, want the parameter to reach the shader")
	}
}
