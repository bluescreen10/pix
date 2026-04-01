package pix

import (
	"github.com/oliverbestmann/webgpu/wgpu"
)

type resourceManager struct {
	// TODO: remove reference to Texturedata
	textures   ResourceList[*TextureData, Texture]
	materials  ResourceList[*MaterialData, Material]
	geometries ResourceList[*GeometryData, Geometry]

	samplers map[Sampler]*wgpu.Sampler

	defaultTexture int
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

func (rm *resourceManager) GetDefaultTexture(device *wgpu.Device) Texture {
	texture := rm.textures.GetResource(rm.defaultTexture)

	// FIXME: horrible hack
	if texture.version == 0 {
		err := rm.uploadTexture(rm.textures.Get(rm.defaultTexture), device)
		if err != nil {
			panic(err)
		}
		texture = rm.textures.GetResource(rm.defaultTexture)
	}

	return texture
}

func (rm *resourceManager) GetGeometry(data *GeometryData) *Geometry {
	if data.slot == 0 {
		data.slot = rm.geometries.Add(data)
	}

	return rm.geometries.GetResourcePtr(data.slot)
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
	rm.materials.Init()
	rm.geometries.Init()
	rm.samplers = make(map[Sampler]*wgpu.Sampler)

	// default Texture
	// FIXME: hacky
	td := NewDataTexture([]byte{255, 255, 255, 255}, 1, 1, wgpu.TextureFormatRGBA8Unorm)
	td.slot = rm.textures.Add(td)
	rm.defaultTexture = td.slot
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

func (rm *resourceManager) processPending(device *wgpu.Device) {
	rm.textures.ProcessDelete(func(tex Texture) error {
		tex.Destroy()
		return nil
	})
}

func (rm *resourceManager) uploadTexture(data *TextureData, device *wgpu.Device) error {
	sampler, ok := rm.samplers[data.sampler]
	tex := rm.textures.GetResource(data.slot)

	if !ok {
		newSampler := device.CreateSampler(&wgpu.SamplerDescriptor{
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

	gpuTexture := device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Texture",
		Size:          wgpu.Extent3D{Width: uint32(data.width), Height: uint32(data.height), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        data.format,
		Usage:         wgpu.TextureUsageCopyDst | wgpu.TextureUsageTextureBinding,
	})

	queue := device.GetQueue()
	queue.WriteTexture(
		&wgpu.TexelCopyTextureInfo{
			Texture:  gpuTexture,
			MipLevel: 0,
			Origin:   wgpu.Origin3D{X: 0, Y: 0, Z: 0},
		},
		data.flush(),
		&wgpu.TexelCopyBufferLayout{
			Offset:       0,
			BytesPerRow:  uint32(data.width) * 4, // Assuming RGBA8 format
			RowsPerImage: uint32(data.height),
		},
		&wgpu.Extent3D{Width: uint32(data.width), Height: uint32(data.height), DepthOrArrayLayers: 1},
	)

	view := gpuTexture.CreateView(nil)

	tex.ref = gpuTexture
	tex.view = view
	rm.textures.SetResource(data.slot, tex)
	return nil
}

func (rm *resourceManager) uploadGeometry(device *wgpu.Device, data *GeometryData) error {
	geometry := rm.geometries.GetResourcePtr(data.slot)

	//TODO: instead of destroying all buffers
	//      track the one that changed
	geometry.Destroy()

	// Allocate index buffer
	if len(data.indices) > 0 {
		buf := device.CreateBufferInit(&wgpu.BufferInitDescriptor{
			Label:    "index buffer",
			Contents: wgpu.ToBytes(data.indices),
			Usage:    wgpu.BufferUsageIndex | wgpu.BufferUsageCopyDst,
		})

		geometry.count = len(data.indices)
		geometry.index = buf
	} else {
		//FIXME: hack
		if len(data.attrs) > 0 {
			geometry.count = data.attrs[0].len
		}
	}

	geometry.bufs = make([]GeometryBuffer, len(data.attrs))

	for i, a := range data.attrs {
		buf := device.CreateBufferInit(&wgpu.BufferInitDescriptor{
			Label:    a.name + " buffer",
			Contents: a.data,
			Usage:    wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
		})

		geometry.bufs[i] = GeometryBuffer{
			loc:     a.loc,
			buf:     buf,
			version: a.version,
		}
	}

	geometry.flags = data.flags
	geometry.version = data.version
	return nil
}
