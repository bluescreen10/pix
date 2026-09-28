package postprocess

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/materials"
)

// ShaderStep is the simplest effect: one full-screen pass of a fragment shader, from the
// image so far into the next. An effect that needs more than one pass implements
// Step itself, the way Bloom does.
//
// Fragment is backend-native bytes; its root starts with POSTFX_ROOT, and Params follows
// it, laid out as the shader declares its own fields. Params may change between frames,
// and a replaced Fragment is picked up on the next one.
type ShaderStep struct {
	Fragment []byte
	Params   []byte
	Label    string

	pass     *FullscreenPass
	fragment []byte // what pass was built from, so a replaced Fragment is noticed
}

func (s *ShaderStep) Encode(frame *Frame, cmd gpu.CommandBuffer) {
	if s.pass == nil || !materials.SameSPIRV(s.fragment, s.Fragment) {
		s.Release()
		label := s.Label
		if label == "" {
			label = "post-shader-step"
		}
		s.pass = NewFullscreenPass(frame.Backend, FullscreenPassDescriptor{Fragment: s.Fragment, Label: label})
		s.fragment = s.Fragment
	}
	root := frame.Root(frame.Source, frame.Width, frame.Height)
	s.pass.Draw(frame.Target, frame.Width, frame.Height, gpu.LoadClear, root.With(s.Params), cmd)
}

func (s *ShaderStep) Release() {
	if s.pass != nil {
		s.pass.Release()
		s.pass = nil
	}
}
