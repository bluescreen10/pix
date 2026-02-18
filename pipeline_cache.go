package pix

import "github.com/cogentcore/webgpu/wgpu"

type renderPipelineKey struct {
	shader string
}

type pipelineCache struct {
	renderPipelines map[renderPipelineKey]*wgpu.RenderPipeline
}

func newPipelineCache() *pipelineCache {
	return &pipelineCache{
		renderPipelines: make(map[renderPipelineKey]*wgpu.RenderPipeline),
	}
}

func (c *pipelineCache) GetRenderPipeline(key renderPipelineKey) *wgpu.RenderPipeline {
	pipeline, _ := c.renderPipelines[key]
	return pipeline
}

func (c *pipelineCache) SetRenderPipeline(key renderPipelineKey, pipeline *wgpu.RenderPipeline) {
	c.renderPipelines[key] = pipeline
}

func (c *pipelineCache) Destroy() {
	for _, pipeline := range c.renderPipelines {
		pipeline.Release()
	}

	c.renderPipelines = make(map[renderPipelineKey]*wgpu.RenderPipeline)
}
