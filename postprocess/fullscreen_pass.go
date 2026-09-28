package postprocess

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/shaders"
)

// FullscreenPassDescriptor describes a FullscreenPass.
type FullscreenPassDescriptor struct {
	// Fragment is the pass's fragment shader, backend-native bytes. Its root starts
	// with POSTFX_ROOT.
	Fragment []byte
	// Format is the colour format of the images the pass draws into. The zero value
	// is ImageFormat.
	Format gpu.Format
	// Blend is nil to replace what is in the target, or a blend state to combine with it.
	Blend []gpu.BlendState
	Label string
}

// FullscreenPass draws one full-screen triangle through a fragment shader: the building
// block of every post-processing effect.
type FullscreenPass struct {
	backend  gpu.Backend
	pipeline gpu.Pipeline
}

// NewFullscreenPass builds a pass. It owns a pipeline until Release.
func NewFullscreenPass(backend gpu.Backend, desc FullscreenPassDescriptor) *FullscreenPass {
	format := desc.Format
	if format == gpu.FormatUndefined {
		format = ImageFormat
	}
	pipeline := backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader:   shaders.ForBackend(backend, shaders.FullscreenVert),
		FragmentShader: shaders.ForBackend(backend, desc.Fragment),
		Topology:       gpu.TopologyTriangles,
		ColorFormats:   []gpu.Format{format},
		Blend:          desc.Blend,
		CullMode:       gpu.CullNone,
		Label:          desc.Label,
	})
	return &FullscreenPass{backend: backend, pipeline: pipeline}
}

// Draw runs the pass into target, which is width x height, in a render pass of its own.
// load is LoadClear or LoadDontCare to overwrite the target, LoadKeep to blend into it.
func (p *FullscreenPass) Draw(target gpu.Texture, width, height uint32, load gpu.LoadOp, root []byte, cmd gpu.CommandBuffer) {
	cmd.BeginRenderPass(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: target, Load: load, Store: gpu.StoreKeep}},
	})
	cmd.SetViewport(0, 0, float32(width), float32(height), 0, 1)
	cmd.SetScissor(0, 0, int32(width), int32(height))
	cmd.SetPipeline(p.pipeline)
	cmd.Draw(root, 3, 1, 0, 0)
	cmd.EndRenderPass()
}

// Release frees the pass's pipeline.
func (p *FullscreenPass) Release() {
	p.backend.DestroyPipeline(p.pipeline)
}
