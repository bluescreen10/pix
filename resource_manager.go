package pix

import (
	"github.com/cogentcore/webgpu/wgpu"
)

type resourceManager struct {
	textures  ResourceList[*TextureData, Texture]
	materials ResourceList[*MaterialData, Material]
	//geometries ResourceList[GeometryData, Geometry]

	samplers map[Sampler]*wgpu.Sampler
}

func (rm *resourceManager) GetTextureByData(device *wgpu.Device, td *TextureData) Texture {
	if td.slot == 0 {
		td.slot = rm.textures.Add(td)
		rm.uploadTexture(td, device)
	}

	texture := rm.textures.GetResource(td.slot)
	if texture.version != td.version {
		rm.uploadTexture(td, device)
		texture = rm.textures.GetResource(td.slot)
	}

	return texture
}

func (rm *resourceManager) GetMaterialByData(m *MaterialData) Material {
	if m.slot == 0 {
		m.slot = rm.materials.Add(m)
	}
	return rm.materials.GetResource(m.slot)
}

func (rm *resourceManager) SetMaterial(id int, resource Material) {
	rm.materials.SetResource(id, resource)
}

func (rm *resourceManager) init() {
	rm.textures.Init()
	rm.samplers = make(map[Sampler]*wgpu.Sampler)
}

func (rm *resourceManager) destroy() {
	for _, sampler := range rm.samplers {
		sampler.Release()
	}

	for _, tex := range rm.textures.AllResources() {
		tex.Destroy()
	}

	rm.textures.Clear()
}

func (rm *resourceManager) processPending(device *wgpu.Device) error {
	rm.textures.ProcessDelete(func(tex Texture) error {
		tex.Destroy()
		return nil
	})

	return nil
}

func (rm *resourceManager) uploadTexture(data *TextureData, device *wgpu.Device) error {
	sampler, ok := rm.samplers[data.sampler]
	tex := rm.textures.GetResource(data.slot)

	if !ok {
		newSampler, err := device.CreateSampler(&wgpu.SamplerDescriptor{
			Label:         "", //TODO: better label
			AddressModeU:  data.sampler.AddressModeU,
			AddressModeV:  data.sampler.AddressModeV,
			AddressModeW:  data.sampler.AddressModeW,
			MagFilter:     data.sampler.MagFilter,
			MinFilter:     data.sampler.MinFilter,
			MipmapFilter:  data.sampler.MipmapFilter,
			LodMinClamp:   data.sampler.LodMinClamp,
			LodMaxClamp:   data.sampler.LodMaxClamp,
			Compare:       data.sampler.Compare,
			MaxAnisotropy: data.sampler.MaxAnisotropy,
		})

		if err != nil {
			return err
		}
		sampler = newSampler
		rm.samplers[data.sampler] = sampler
	}

	tex.sampler = sampler
	tex.version = data.version

	if !data.hasPendingData() {
		rm.textures.SetResource(data.slot, tex)
		return nil
	}

	if tex.ref != nil {
		tex.ref.Destroy()
	}

	gpuTexture, err := device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Texture",
		Size:          wgpu.Extent3D{Width: uint32(data.width), Height: uint32(data.height), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        data.format,
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
		data.flush(),
		&wgpu.TextureDataLayout{
			Offset:       0,
			BytesPerRow:  uint32(data.width) * 4, // Assuming RGBA8 format
			RowsPerImage: uint32(data.height),
		},
		&wgpu.Extent3D{Width: uint32(data.width), Height: uint32(data.height), DepthOrArrayLayers: 1},
	)

	if err != nil {
		return err
	}

	view, err := gpuTexture.CreateView(nil)
	if err != nil {
		return err
	}

	tex.ref = gpuTexture
	tex.view = view
	rm.textures.SetResource(data.slot, tex)
	return nil
}
