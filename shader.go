package pix

import (
	"github.com/cogentcore/webgpu/wgpu"
)

// Basically there are the following possibilities:
// The prepared material exists but is out of date -> we need to update buffers
// The preapred material exists but the binding group + data is invalid -> we need to update buffers and rebuild the bg
// The prepared material has never been initialized -> we need to build layout + buffers + bg
func prepareMaterial(device *wgpu.Device, data *MaterialData, material Material, resources *resourceManager) (Material, error) {
	// if material is up-to-date don't don anything
	if data.version == material.version {
		return material, nil
	}

	// full rebuild
	if len(data.uniforms) != len(material.uniformBuffers) {
		material.Destroy()

		// Allocate buffers
		buffers, err := createMaterialBuffers(device, data)
		if err != nil {
			return material, err
		}
		material.uniformBuffers = buffers

		// Create BindGroup Layout
		layout, err := createMaterialBindGroupLayout(device, data)
		if err != nil {
			return material, err
		}
		material.bindGroupLayout = layout

	}

	// only data has changed
	queue := device.GetQueue()
	for i, u := range data.uniforms {
		queue.WriteBuffer(material.uniformBuffers[i], 0, u.Bytes())
	}

	// recreate the bind group
	bindGroup, err := createMaterialBindGroup(device, data, material, resources)

	if err != nil {
		return material, err
	}

	material.vertexShader = data.vertexShader
	material.fragmentShader = data.fragmentShader
	material.bindGroup = bindGroup
	material.version = data.version
	return material, nil
}

func createMaterialBindGroupLayout(device *wgpu.Device, data *MaterialData) (*wgpu.BindGroupLayout, error) {
	var bgLayoutEntries []wgpu.BindGroupLayoutEntry
	var binding uint32

	for _, u := range data.uniforms {
		bgLayoutEntries = append(bgLayoutEntries,
			wgpu.BindGroupLayoutEntry{
				Binding:    binding,
				Visibility: wgpu.ShaderStageFragment | wgpu.ShaderStageVertex, //FIXME: allow uniform to pass the stage
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
				Visibility: wgpu.ShaderStageFragment | wgpu.ShaderStageVertex, //FIXME: allow user to specify visibility of texture
				Texture: wgpu.TextureBindingLayout{
					Multisampled:  false,
					ViewDimension: wgpu.TextureViewDimension2D,
					SampleType:    wgpu.TextureSampleTypeFloat,
				},
			},
			wgpu.BindGroupLayoutEntry{
				Binding:    binding + 1,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment, //FIXME: allow user to specify visibility
				Sampler: wgpu.SamplerBindingLayout{
					Type: wgpu.SamplerBindingTypeFiltering,
				},
			})
		binding += 2
	}

	return device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "", //TODO
		Entries: bgLayoutEntries,
	})
}

func createMaterialBuffers(device *wgpu.Device, data *MaterialData) ([]*wgpu.Buffer, error) {
	var buffers []*wgpu.Buffer

	for _, u := range data.uniforms {
		buffer, err := device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "", //TODO
			Size:  uint64(u.Size()),
			Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		})

		if err != nil {
			return buffers, err
		}

		buffers = append(buffers, buffer)
	}

	return buffers, nil
}

func createMaterialBindGroup(device *wgpu.Device, data *MaterialData, material Material, resources *resourceManager) (*wgpu.BindGroup, error) {
	var bgEntries []wgpu.BindGroupEntry
	var binding uint32

	for i, _ := range data.uniforms {
		bgEntries = append(bgEntries,
			wgpu.BindGroupEntry{
				Binding: binding,
				Buffer:  material.uniformBuffers[i],
				Offset:  0,
				Size:    wgpu.WholeSize,
			})
		binding++
	}

	for _, td := range data.textures {
		t := resources.GetTextureByData(device, td)
		bgEntries = append(bgEntries,
			wgpu.BindGroupEntry{
				Binding:     binding,
				TextureView: t.view,
			},
			wgpu.BindGroupEntry{
				Binding: binding + 1,
				Sampler: t.sampler,
			},
		)
		binding += 2
	}

	return device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:   "", //TODO
		Layout:  material.bindGroupLayout,
		Entries: bgEntries,
	})
}
