package pix

import (
	"log"
	"log/slog"
	"os"

	"github.com/cogentcore/webgpu/wgpu"
)

type Renderer struct {
	runtime       *wgpuRuntime
	width, height uint32
	frameCount    uint32
	logger        *slog.Logger
}

func NewRenderer(width, height uint32) *Renderer {
	return &Renderer{
		width:   width,
		height:  height,
		logger:  slog.New(slog.NewTextHandler(os.Stderr, nil)),
		runtime: &wgpuRuntime{},
	}
}

func (r *Renderer) Init(descriptor *wgpu.SurfaceDescriptor) error {
	if err := r.runtime.init(r.width, r.height, descriptor); err != nil {
		log.Fatalf("error creating runtime: %v", slog.Any("err", err))
		return err
	}

	return nil
}

func (r *Renderer) Render(camera Camera) error {
	r.frameCount++
	texture, err := r.runtime.Surface.GetCurrentTexture()
	if err != nil {
		r.logger.Error("error obtaining next frame texture", slog.Any("err", err))
		return err
	}
	defer texture.Release()

	view, err := texture.CreateView(nil)
	if err != nil {
		r.logger.Error("error creating view", slog.Any("err", err))
		return err
	}
	defer view.Release()

	encoder, err := r.runtime.Device.CreateCommandEncoder(nil)
	if err != nil {
		r.logger.Error("error creating command encoder", slog.Any("err", err))
		return err
	}
	defer encoder.Release()

	//temp code
	renderPass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       view,
			LoadOp:     wgpu.LoadOpClear,
			StoreOp:    wgpu.StoreOpStore,
			ClearValue: wgpu.Color{R: 0, G: 0, B: 0, A: 1},
		}},
	})

	err = renderPass.End()
	if err != nil {
		r.logger.Error("error ending render pass", slog.Any("err", err))
		return err
	}

	cmdBuf, err := encoder.Finish(nil)
	if err != nil {
		r.logger.Error("error creating command buffer", slog.Any("err", err))
		return err
	}
	defer cmdBuf.Release()

	r.runtime.Queue.Submit(cmdBuf)
	r.runtime.Surface.Present()
	return nil
}
