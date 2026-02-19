package pix

import (
	"log/slog"
	"os"
	"sync"
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/cogentcore/webgpu/wgpu"
)

var renderablesPool = sync.Pool{
	New: func() any {
		return make([]*Mesh, 0, 4096)
	},
}

type Renderer struct {
	runtime       *wgpuRuntime
	width, height uint32
	frameCount    uint32
	logger        *slog.Logger

	pipelineCache *pipelineCache

	// temp
	basicShader           *basicShader
	globalUniformBuffer   *wgpu.Buffer
	globalBindGroupLayout *wgpu.BindGroupLayout
	globalBindGroup       *wgpu.BindGroup

	objectUniformBuffer   *wgpu.Buffer
	objectBindGroupLayout *wgpu.BindGroupLayout
	objectBindGroup       *wgpu.BindGroup
}

func NewRenderer(width, height uint32) *Renderer {
	return &Renderer{
		width:   width,
		height:  height,
		logger:  slog.New(slog.NewTextHandler(os.Stderr, nil)),
		runtime: &wgpuRuntime{},

		pipelineCache: newPipelineCache(),

		// temp
		basicShader: &basicShader{},
	}
}

func (r *Renderer) Init(descriptor *wgpu.SurfaceDescriptor) error {
	if err := r.runtime.init(r.width, r.height, descriptor); err != nil {
		slog.Error("error creating runtime", slog.Any("err", err))
		return err
	}

	if err := r.createGlobalResources(); err != nil {
		slog.Error("error creating global resources", slog.Any("err", err))
	}

	return nil
}

func (r *Renderer) Destroy() {
	r.runtime.Destroy()
	r.runtime = nil

	r.objectBindGroup.Release()
	r.objectBindGroup = nil

	r.globalBindGroup.Release()
	r.globalBindGroup = nil

	r.globalUniformBuffer.Destroy()
	r.globalUniformBuffer = nil

	r.objectUniformBuffer.Destroy()
	r.objectUniformBuffer = nil

	r.globalBindGroupLayout.Release()
	r.globalBindGroupLayout = nil

	r.objectBindGroupLayout.Release()
	r.objectBindGroupLayout = nil
}

func (r *Renderer) Render(scene *Scene, camera Camera) error {
	r.frameCount++

	//Extract objects
	//TODO: use sync.Pool to avoid allocations
	meshes := renderablesPool.Get().([]*Mesh)
	defer renderablesPool.Put(meshes)
	meshes = meshes[:0]
	renderables := r.appendRenderables(meshes, scene)

	//Prepare Objects
	for _, mesh := range renderables {
		geometry := mesh.geometry
		if geometry.IsDirty() {
			err := geometry.Upload(r.runtime.Device, r.runtime.Queue)
			if err != nil {
				r.logger.Error("error uploading geometry", slog.Any("err", err))
			}
		}
	}

	// Update Global Uniforms
	viewProj := camera.ViewProjection()
	if err := r.runtime.Queue.WriteBuffer(r.globalUniformBuffer, 0, wgpu.ToBytes(viewProj[:])); err != nil {
		r.logger.Error("error updating global uniform", slog.Any("err", err))
	}

	//Draw
	for _, mesh := range renderables {
		if err := r.renderMesh(mesh, scene.background); err != nil {
			return err
		}
	}

	return nil
}

func (r *Renderer) renderMesh(mesh *Mesh, bgColor glm.Color4f) error {

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
			ClearValue: wgpu.Color{R: float64(bgColor.R()), G: (float64(bgColor.G())), B: (float64(bgColor.B())), A: float64(bgColor.A())}, //TODO: make it something the user can define
		}},
	})

	pipeline, err := r.getPipelineFor(mesh)
	if err != nil {
		r.logger.Error("error getting pipeline", slog.Any("err", err))
		return err
	}

	renderPass.SetPipeline(pipeline)
	renderPass.SetBindGroup(0, r.globalBindGroup, []uint32{})
	renderPass.SetBindGroup(1, r.objectBindGroup, []uint32{})

	//TODO: use attributes instead
	renderPass.SetVertexBuffer(0, mesh.geometry.positionBuffer, 0, wgpu.WholeSize)
	renderPass.SetIndexBuffer(mesh.geometry.indicesBuffer, wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
	renderPass.DrawIndexed(uint32(len(mesh.geometry.indices)), 1, 0, 0, 0)

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

func (r *Renderer) getPipelineFor(mesh *Mesh) (*wgpu.RenderPipeline, error) {
	pipelineKey := renderPipelineKey{r.basicShader.Name()}
	pipline := r.pipelineCache.GetRenderPipeline(pipelineKey)

	if pipline != nil {
		return pipline, nil
	}

	vertexLayout := mesh.geometry.VertexLayout()
	pipeline, err := r.createRenderPipeline(r.basicShader, vertexLayout)
	if err != nil {
		return nil, err
	}

	r.pipelineCache.SetRenderPipeline(pipelineKey, pipeline)
	return pipeline, nil
}

func (r *Renderer) createRenderPipeline(shader Shader, vertexLayout []wgpu.VertexBufferLayout) (*wgpu.RenderPipeline, error) {
	// shaderBindGroupLayout, err := shader.BindGroupLayout(r.runtime.Device)
	// if err != nil {
	// 	return nil, err
	// }

	layout, err := r.runtime.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label: "", // TODO: add a descriptive name for debugging
		BindGroupLayouts: []*wgpu.BindGroupLayout{
			r.globalBindGroupLayout,
			//shaderBindGroupLayout,
			r.objectBindGroupLayout,
		},
	})

	if err != nil {
		return nil, err
	}

	vsModule, vsMain, fsModule, fsMain, err := r.compileShader(r.runtime.Device, shader)
	if err != nil {
		return nil, err
	}

	pipeline, err := r.runtime.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "", //TODO: provide a meaningful name
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     vsModule,
			EntryPoint: vsMain,
			Buffers:    vertexLayout,
		},
		Fragment: &wgpu.FragmentState{
			Module:     fsModule,
			EntryPoint: fsMain,
			Targets: []wgpu.ColorTargetState{
				{
					Format:    r.runtime.Format,
					Blend:     nil, //TODO: Shader should provide this
					WriteMask: wgpu.ColorWriteMaskAll,
				},
			},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopologyTriangleList, //TODO: Shader should provide this
			FrontFace: wgpu.FrontFaceCCW,                  //TODO: Shader should provide this
			CullMode:  wgpu.CullModeBack,                  //TODO:Shader should provide this
		},
		// DepthStencil: &wgpu.DepthStencilState{
		// 	Format:            wgpu.TextureFormatDepth24Plus,
		// 	DepthWriteEnabled: true,
		// 	DepthCompare:      wgpu.CompareFunctionLess,
		// 	StencilFront: wgpu.StencilFaceState{
		// 		Compare:     wgpu.CompareFunctionAlways,
		// 		FailOp:      wgpu.StencilOperationKeep,
		// 		DepthFailOp: wgpu.StencilOperationKeep,
		// 		PassOp:      wgpu.StencilOperationKeep,
		// 	},
		// 	StencilBack: wgpu.StencilFaceState{
		// 		Compare:     wgpu.CompareFunctionAlways,
		// 		FailOp:      wgpu.StencilOperationKeep,
		// 		DepthFailOp: wgpu.StencilOperationKeep,
		// 		PassOp:      wgpu.StencilOperationKeep,
		// 	},
		// 	StencilReadMask:  0xFFFFFFFF,
		// 	StencilWriteMask: 0xFFFFFFFF,
		// },
		Multisample: wgpu.MultisampleState{
			Count:                  1,
			Mask:                   0xFFFFFFFF,
			AlphaToCoverageEnabled: false,
		},
	})

	return pipeline, err
}

func (r *Renderer) compileShader(device *wgpu.Device, shader Shader) (*wgpu.ShaderModule, string, *wgpu.ShaderModule, string, error) {
	vsFile := shader.VertexShader()
	fsFile := shader.FragmentShader()

	if vsFile == fsFile {
		code, err := os.ReadFile(vsFile)
		if err != nil {
			return nil, "", nil, "", err
		}

		module, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
			WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: string(code)},
		})

		return module, "vs_main", module, "fs_main", err

	} else {
		vsCode, err := os.ReadFile(vsFile)
		if err != nil {
			return nil, "", nil, "", err
		}

		fsCode, err := os.ReadFile(fsFile)
		if err != nil {
			return nil, "", nil, "", err
		}

		vsModule, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
			WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: string(vsCode)},
		})

		if err != nil {
			return nil, "", nil, "", err
		}

		fsModule, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
			WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: string(fsCode)},
		})

		return vsModule, "main", fsModule, "main", err
	}
}

func (r *Renderer) createGlobalResources() error {
	if err := r.createGlobalBindGroupLayouts(); err != nil {
		return err
	}

	if err := r.createGlobalBuffers(); err != nil {
		return err
	}

	if err := r.createGlobalBindGroups(); err != nil {
		return err
	}

	return nil
}

func (r *Renderer) createGlobalBindGroupLayouts() error {
	layout, err := r.runtime.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Global Bind Group Layout",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0, //TODO: Make it a constant
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,

				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					HasDynamicOffset: false,
					MinBindingSize:   uint64(unsafe.Sizeof(glm.Mat4f{})), //TODO: replace this with a uniform
				},
			},
		},
	})

	if err != nil {
		return err
	}

	r.globalBindGroupLayout = layout

	layout, err = r.runtime.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Object/Model Bind Group Layout",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0, //TODO: Make it a constant
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,

				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					HasDynamicOffset: false,
					MinBindingSize:   uint64(unsafe.Sizeof(glm.Mat4f{})), //TODO: replace this with a uniform (also implement a ring-buffer)
				},
			},
		},
	})

	r.objectBindGroupLayout = layout

	return err
}

func (r *Renderer) createGlobalBuffers() error {
	buf, err := r.runtime.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "Global uniform buffer",
		Size:  uint64(unsafe.Sizeof(glm.Mat4f{})), //TODO: use an actual uniform
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})

	if err != nil {
		return err
	}

	r.globalUniformBuffer = buf

	buf, err = r.runtime.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "Object uniform buffer",
		Size:  uint64(unsafe.Sizeof(glm.Mat4f{})), //TODO: use an actual uniform
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})

	if err != nil {
		return err
	}

	r.objectUniformBuffer = buf

	return nil
}

func (r *Renderer) createGlobalBindGroups() error {
	bindingGroup, err := r.runtime.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "Global bind group",
		Layout: r.globalBindGroupLayout,
		Entries: []wgpu.BindGroupEntry{
			{
				Binding: 0, //TODO: use a constant
				Buffer:  r.globalUniformBuffer,
				Offset:  0,
				Size:    wgpu.WholeSize,
			},
		},
	})

	if err != nil {
		return err
	}

	r.globalBindGroup = bindingGroup

	bindingGroup, err = r.runtime.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "Object bind group",
		Layout: r.globalBindGroupLayout,
		Entries: []wgpu.BindGroupEntry{
			{
				Binding: 0, //TODO: use a constant
				Buffer:  r.objectUniformBuffer,
				Offset:  0,
				Size:    wgpu.WholeSize,
			},
		},
	})

	if err != nil {
		return err
	}

	r.objectBindGroup = bindingGroup

	return nil
}

func (r *Renderer) appendRenderables(meshes []*Mesh, node Node) []*Mesh {
	for _, child := range node.Children() {
		switch object := any(child).(type) {

		case *Mesh:
			meshes = append(meshes, object)
		}

		meshes = r.appendRenderables(meshes, child)
	}

	return meshes
}
