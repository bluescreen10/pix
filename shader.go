package pix

import (
	"github.com/cogentcore/webgpu/wgpu"
)

// Basically there are the following possibilities:
// The prepared material exists but is out of date -> we need to update buffers
// The preapred material exists but the binding group + data is invalid -> we need to update buffers and rebuild the bg
// The prepared material has never been initialized -> we need to build layout + buffers + bg
func (r *Renderer) prepareMaterial(data *Material, material PreparedMaterial, device *wgpu.Device, resources *resourceManager) (PreparedMaterial, error) {
	var bgLayoutEntries []wgpu.BindGroupLayoutEntry
	var bgEntries []wgpu.BindGroupEntry

	var binding uint32

	queue := device.GetQueue()

	if material.uniformBuffers == nil {
		material.uniformBuffers = make([]*wgpu.Buffer, len(data.uniforms))
	}

	for i, u := range data.uniforms {
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

		if material.uniformBuffers[i] == nil {
			buffer, err := device.CreateBuffer(&wgpu.BufferDescriptor{
				Label: "", //TODO
				Size:  uint64(u.Size()),
				Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
			})

			if err != nil {
				return material, err
			}

			material.uniformBuffers[i] = buffer
		}

		bgEntries = append(bgEntries,
			wgpu.BindGroupEntry{
				Binding: binding,
				Buffer:  material.uniformBuffers[i],
				Offset:  0,
				Size:    wgpu.WholeSize,
			})

		queue.WriteBuffer(material.uniformBuffers[i], 0, u.Bytes())
		binding++
	}

	for _, td := range data.textures {
		t := resources.GetTextureByData(td)
		if t.version != td.version {
			err := r.resources.uploadTexture(td, device)
			if err != nil {
				return material, err
			}
			t = resources.GetTextureByData(td)
		}
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

	layout, err := device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "", //TODO
		Entries: bgLayoutEntries,
	})

	if err != nil {
		return material, err
	}

	bg, err := device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:   "", //TODO
		Layout:  layout,
		Entries: bgEntries,
	})

	if err != nil {
		return material, err
	}

	material.bindGroupLayout = layout
	material.bindGroup = bg
	material.version = data.version
	material.vertexShader = data.vertexShader
	material.fragmentShader = data.fragmentShader
	return material, nil
}
