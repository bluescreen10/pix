package shaders_test

import (
	"bytes"
	"testing"

	"github.com/bluescreen10/pix/shaders"
)

// shaderFormatBackend is a backend that reports the shader format it runs.
type shaderFormatBackend string

func (b shaderFormatBackend) ShaderFormat() string {
	return string(b)
}

// TestSelectPicksTheBackendsVariant: a Metal backend gets the metallib, any other the
// SPIR-V — including one that does not say what it runs.
func TestSelectPicksTheBackendsVariant(t *testing.T) {
	spirv, metallib := []byte("spirv"), []byte("metallib")
	cases := []struct {
		name    string
		backend any
		want    []byte
	}{
		{"metal", shaderFormatBackend("metal"), metallib},
		{"vulkan", shaderFormatBackend("spirv"), spirv},
		{"unknown", struct{}{}, spirv},
	}
	for _, c := range cases {
		if got := shaders.Select(c.backend, spirv, metallib); !bytes.Equal(got, c.want) {
			t.Errorf("Select(%s backend) = %q, want %q", c.name, got, c.want)
		}
	}
}
