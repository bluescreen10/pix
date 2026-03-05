package pix

import (
	"image"
	"os"

	"github.com/cogentcore/webgpu/wgpu"
)

type ResourceManager struct {
	textures              FreeList[Texture]
	pendingUploadTextures []int
	pendingDeleteTextures []int

	samplers map[Sampler]*wgpu.Sampler
}

func (rm *ResourceManager) LoadTexture(path string) (Handle[Texture], error) {
	file, err := os.Open(path)

	if err != nil {
		return Handle[Texture]{id: -1}, err
	}

	img, _, err := image.Decode(file)

	if err != nil {
		return Handle[Texture]{id: -1}, err
	}

	// convert to RGBA
	rgba := image.NewRGBA(img.Bounds())
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}

	texture := NewDataTexture(rgba.Pix, rgba.Bounds().Dx(), rgba.Bounds().Dy(), wgpu.TextureFormatRGBA8Unorm)
	index := rm.textures.Add(texture)
	rm.pendingUploadTextures = append(rm.pendingUploadTextures, index)
	return Handle[Texture]{
		id:       index,
		refCount: new(int32(1)),
		destroy: func() {
			rm.DeleteTexture(index)
		},
	}, nil
}

func (rm *ResourceManager) DeleteTexture(index int) {
	rm.pendingDeleteTextures = append(rm.pendingDeleteTextures, index)
}

func (rm *ResourceManager) GetTexture(handle Handle[Texture]) Texture {
	return rm.textures.Get(handle.id)
}

func (rm *ResourceManager) SetTexture(handle Handle[Texture], texture Texture) {
	rm.textures.Set(handle.id, texture)
	rm.pendingUploadTextures = append(rm.pendingUploadTextures, handle.id)
}

func (rm *ResourceManager) prepareResources(device *wgpu.Device) error {
	for _, index := range rm.pendingUploadTextures {
		texture := rm.textures.Get(index)
		err := rm.uploadTexture(index, texture, device)
		if err != nil {
			return err
		}
	}

	for _, index := range rm.pendingDeleteTextures {
		texture := rm.textures.Get(index)
		if texture.gpuView != nil {
			texture.gpuView.Release()
			texture.gpuView = nil
		}

		if texture.gpuTexture != nil {
			texture.gpuTexture.Destroy()
			texture.gpuTexture = nil
		}
		rm.textures.Delete(index)
	}

	rm.pendingUploadTextures = rm.pendingUploadTextures[:0]
	rm.pendingDeleteTextures = rm.pendingDeleteTextures[:0]
	return nil
}

func (rm *ResourceManager) uploadTexture(index int, texture Texture, device *wgpu.Device) error {
	if _, ok := rm.samplers[texture.Sampler]; !ok {
		sampler, err := device.CreateSampler(&wgpu.SamplerDescriptor{
			Label:         "", //TODO: better label
			AddressModeU:  wgpu.AddressModeRepeat,
			AddressModeV:  wgpu.AddressModeRepeat,
			AddressModeW:  wgpu.AddressModeRepeat,
			MagFilter:     texture.Sampler.MagFilter,
			MinFilter:     texture.Sampler.MinFilter,
			MipmapFilter:  texture.Sampler.MipmapFilter,
			LodMinClamp:   texture.Sampler.LodMinClamp,
			LodMaxClamp:   texture.Sampler.LodMaxClamp,
			Compare:       texture.Sampler.Compare,
			MaxAnisotropy: texture.Sampler.MaxAnisotropy,
		})

		if err != nil {
			return err
		}

		if rm.samplers == nil {
			rm.samplers = make(map[Sampler]*wgpu.Sampler)
		}

		rm.samplers[texture.Sampler] = sampler
	}

	if texture.PendingData == nil {
		return nil
	}

	if texture.gpuTexture != nil {
		texture.gpuTexture.Destroy()
	}

	gpuTexture, err := device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Texture",
		Size:          wgpu.Extent3D{Width: uint32(texture.Width), Height: uint32(texture.Height), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        texture.Format,
		Usage:         wgpu.TextureUsageCopyDst | wgpu.TextureUsageTextureBinding,
	})

	if err != nil {
		return err
	}

	queue := device.GetQueue()
	err = queue.WriteTexture(
		&wgpu.ImageCopyTexture{
			Texture:  gpuTexture,
			MipLevel: 0,
			Origin:   wgpu.Origin3D{X: 0, Y: 0, Z: 0},
		},
		texture.PendingData,
		&wgpu.TextureDataLayout{
			Offset:       0,
			BytesPerRow:  uint32(texture.Width) * 4, // Assuming RGBA8 format
			RowsPerImage: uint32(texture.Height),
		},
		&wgpu.Extent3D{Width: uint32(texture.Width), Height: uint32(texture.Height), DepthOrArrayLayers: 1},
	)

	if err != nil {
		return err
	}

	view, err := gpuTexture.CreateView(nil)
	if err != nil {
		return err
	}

	texture.gpuTexture = gpuTexture
	texture.gpuView = view
	texture.PendingData = nil
	rm.textures.Set(index, texture)
	return nil
}
