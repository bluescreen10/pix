package pix

import (
	"github.com/bluescreen10/dawn-go/wgpu"
)

func prepareMaterial(device *wgpu.Device, data *MaterialData, r *Renderer) error {
	if data.version == data.gpuVersion {
		return nil
	}

	if len(data.uniforms) != len(data.gpuUniformBuffers) {
		data.Destroy()

		buffers, err := createMaterialBuffers(device, data)
		if err != nil {
			return err
		}
		data.gpuUniformBuffers = buffers

		layout, err := createMaterialBindGroupLayout(device, data)
		if err != nil {
			return err
		}
		data.gpuBindGroupLayout = layout
	}

	queue := device.GetQueue()
	for i, u := range data.uniforms {
		queue.WriteBuffer(data.gpuUniformBuffers[i], 0, u.Bytes())
	}

	bindGroup, err := createMaterialBindGroup(device, data, r)
	if err != nil {
		return err
	}

	data.gpuBindGroup = bindGroup
	data.gpuVersion = data.version
	return nil
}

func createMaterialBindGroupLayout(device *wgpu.Device, data *MaterialData) (*wgpu.BindGroupLayout, error) {
	var bgLayoutEntries []wgpu.BindGroupLayoutEntry
	var binding uint32

	for _, u := range data.uniforms {
		bgLayoutEntries = append(bgLayoutEntries,
			wgpu.BindGroupLayoutEntry{
				Binding:    binding,
				Visibility: wgpu.ShaderStageFragment | wgpu.ShaderStageVertex,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					HasDynamicOffset: false,
					MinBindingSize:   uint64(u.Size()),
				},
			})
		binding++
	}

	for range data.textures {
		bgLayoutEntries = append(bgLayoutEntries,
			wgpu.BindGroupLayoutEntry{
				Binding:    binding,
				Visibility: wgpu.ShaderStageFragment | wgpu.ShaderStageVertex,
				Texture: wgpu.TextureBindingLayout{
					Multisampled:  false,
					ViewDimension: wgpu.TextureViewDimension2D,
					SampleType:    wgpu.TextureSampleTypeFloat,
				},
			},
			wgpu.BindGroupLayoutEntry{
				Binding:    binding + 1,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
				Sampler: wgpu.SamplerBindingLayout{
					Type: wgpu.SamplerBindingTypeFiltering,
				},
			})
		binding += 2
	}

	bgl := device.CreateBindGroupLayout(wgpu.BindGroupLayoutDescriptor{
		Entries: bgLayoutEntries,
	})

	return bgl, nil
}

func createMaterialBuffers(device *wgpu.Device, data *MaterialData) ([]*wgpu.Buffer, error) {
	var buffers []*wgpu.Buffer

	for _, u := range data.uniforms {
		buffer := device.CreateBuffer(wgpu.BufferDescriptor{
			Size:  uint64(u.Size()),
			Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		})
		buffers = append(buffers, buffer)
	}

	return buffers, nil
}

func createMaterialBindGroup(device *wgpu.Device, data *MaterialData, r *Renderer) (*wgpu.BindGroup, error) {
	var bgEntries []wgpu.BindGroupEntry
	var binding uint32

	for i := range data.uniforms {
		bgEntries = append(bgEntries,
			wgpu.BindGroupEntry{
				Binding: binding,
				Buffer:  data.gpuUniformBuffers[i],
				Offset:  0,
				Size:    wgpu.WholeSize,
			})
		binding++
	}

	for _, texRef := range data.textures {
		var td *TextureData
		if texRef.Valid() {
			id := texRef.ID()
			td = r.textures.get(id)
			if td.gpuVersion < td.version {
				r.uploadTexture(id)
			}
		} else {
			td = r.defaultTexture()
		}

		bgEntries = append(bgEntries,
			wgpu.BindGroupEntry{
				Binding:     binding,
				TextureView: td.gpuView,
			},
			wgpu.BindGroupEntry{
				Binding: binding + 1,
				Sampler: td.gpuSampler,
			},
		)
		binding += 2
	}

	bg := device.CreateBindGroup(wgpu.BindGroupDescriptor{
		Layout:  data.gpuBindGroupLayout,
		Entries: bgEntries,
	})

	return bg, nil
}
