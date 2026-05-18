package pix

import "github.com/bluescreen10/dawn-go/wgpu"

// TextureArray manages a fixed-capacity depth texture array.
// Layers are claimed in-order via AllocLayers; use LayerView to get a
// per-layer view for rendering into and ArrayView/Sampler for sampling.
type TextureArray struct {
	gpuTexture *wgpu.Texture
	gpuView    *wgpu.TextureView   // full-array view for sampling
	layerViews []*wgpu.TextureView // per-layer views for rendering
	gpuSampler *wgpu.Sampler
	capacity   uint32
	nextLayer  uint32
}

// newDepthTextureArray creates a depth texture array with the given per-layer
// size and layer capacity. arrayDim controls how the full array is exposed for
// sampling: use TextureViewDimension2DArray for directional/spot shadows and
// TextureViewDimensionCubeArray for point-light cube shadows.
func newDepthTextureArray(device *wgpu.Device, size, capacity uint32, arrayDim wgpu.TextureViewDimension) *TextureArray {
	tex := device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Depth Texture Array",
		Usage:         wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatDepth32Float,
		MipLevelCount: 1,
		SampleCount:   1,
		Size: wgpu.Extent3D{
			Width:              size,
			Height:             size,
			DepthOrArrayLayers: capacity,
		},
	})

	arrayView := tex.CreateView(&wgpu.TextureViewDescriptor{
		Format:          wgpu.TextureFormatDepth32Float,
		Dimension:       arrayDim,
		BaseMipLevel:    0,
		MipLevelCount:   1,
		BaseArrayLayer:  0,
		ArrayLayerCount: capacity,
		Aspect:          wgpu.TextureAspectDepthOnly,
	})

	layerViews := make([]*wgpu.TextureView, capacity)
	for i := range layerViews {
		layerViews[i] = tex.CreateView(&wgpu.TextureViewDescriptor{
			Format:          wgpu.TextureFormatDepth32Float,
			Dimension:       wgpu.TextureViewDimension2D,
			BaseMipLevel:    0,
			MipLevelCount:   1,
			BaseArrayLayer:  uint32(i),
			ArrayLayerCount: 1,
			Aspect:          wgpu.TextureAspectDepthOnly,
		})
	}

	sampler := device.CreateSampler(&wgpu.SamplerDescriptor{
		AddressModeU:  wgpu.AddressModeClampToEdge,
		AddressModeV:  wgpu.AddressModeClampToEdge,
		AddressModeW:  wgpu.AddressModeClampToEdge,
		MagFilter:     wgpu.FilterModeLinear,
		MinFilter:     wgpu.FilterModeLinear,
		MipmapFilter:  wgpu.MipmapFilterModeNearest,
		LodMaxClamp:   32,
		Compare:       wgpu.CompareFunctionLessEqual,
		MaxAnisotropy: 1,
	})

	return &TextureArray{
		gpuTexture: tex,
		gpuView:    arrayView,
		layerViews: layerViews,
		gpuSampler: sampler,
		capacity:   capacity,
	}
}

// AllocLayers claims count consecutive layers and returns the base index.
// Returns (0, false) if the array is full.
func (a *TextureArray) AllocLayers(count uint32) (uint32, bool) {
	if a.nextLayer+count > a.capacity {
		return 0, false
	}
	base := a.nextLayer
	a.nextLayer += count
	return base, true
}

func (a *TextureArray) LayerView(i uint32) *wgpu.TextureView { return a.layerViews[i] }
func (a *TextureArray) ArrayView() *wgpu.TextureView         { return a.gpuView }
func (a *TextureArray) Sampler() *wgpu.Sampler               { return a.gpuSampler }
func (a *TextureArray) Texture() *wgpu.Texture               { return a.gpuTexture }

func (a *TextureArray) Destroy() {
	a.gpuSampler.Release()
	for _, v := range a.layerViews {
		v.Release()
	}
	a.gpuView.Release()
	a.gpuTexture.Destroy()
}
