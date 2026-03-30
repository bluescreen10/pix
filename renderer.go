package pix

import (
	"log/slog"
	"math/bits"
	"os"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/cogentcore/webgpu/wgpu"
)

const (
	InitialStorageCapacity = 1024
)

var modelsPool = sync.Pool{
	New: func() any {
		return make([]glm.Mat4f, 0, InitialStorageCapacity)
	},
}

var viewableMeshesPool = sync.Pool{
	New: func() any {
		return make([]*Mesh, 0, 4096)
	},
}

var renderablesPool = sync.Pool{
	New: func() any {
		return make([]renderable, 0, 4096)
	},
}

type renderContext struct {
	texture    *wgpu.Texture
	view       *wgpu.TextureView
	renderPass *wgpu.RenderPassEncoder
	encoder    *wgpu.CommandEncoder
}

type Renderer struct {
	resources resourceManager

	runtime       *wgpuRuntime
	width, height uint32
	frameCount    uint32
	logger        *slog.Logger

	pipelineCache *pipelineCache

	// temp
	globalUniformBuffer   *wgpu.Buffer
	globalBindGroupLayout *wgpu.BindGroupLayout
	globalBindGroup       *wgpu.BindGroup

	objectStorageBuffer          *wgpu.Buffer
	objectStorageBindGroupLayout *wgpu.BindGroupLayout
	objectStorageBindGroup       *wgpu.BindGroup
	objectStorageCapacity        uint32

	gpuTimingEnabled bool

	Stats *RendererStats
}

func NewRenderer(width, height uint32) *Renderer {

	return &Renderer{
		width:         width,
		height:        height,
		logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		runtime:       &wgpuRuntime{},
		Stats:         NewRendererStats(60),
		pipelineCache: newPipelineCache(),
	}
}

func (r *Renderer) Init(descriptor *wgpu.SurfaceDescriptor) error {
	if err := r.runtime.init(r.width, r.height, descriptor); err != nil {
		slog.Error("error creating runtime", slog.Any("err", err))
		return err
	}

	r.resources.init()

	if err := r.createGlobalResources(); err != nil {
		slog.Error("error creating global resources", slog.Any("err", err))
	}

	return nil
}

func (r *Renderer) Destroy() {
	r.runtime.Destroy()
	r.runtime = nil

	if r.objectStorageBindGroup != nil {
		r.objectStorageBindGroup.Release()
		r.objectStorageBindGroup = nil
	}

	r.globalBindGroup.Release()
	r.globalBindGroup = nil

	r.globalUniformBuffer.Destroy()
	r.globalUniformBuffer = nil

	if r.objectStorageBuffer != nil {
		r.objectStorageBuffer.Destroy()
		r.objectStorageBuffer = nil
	}

	r.globalBindGroupLayout.Release()
	r.globalBindGroupLayout = nil

	if r.objectStorageBindGroupLayout != nil {
		r.objectStorageBindGroupLayout.Release()
		r.objectStorageBindGroupLayout = nil
	}

	r.resources.destroy()
}

func (r *Renderer) ensureObjectStorageSize(neededObjects uint32) error {
	var err error

	if r.objectStorageBuffer == nil || r.objectStorageCapacity < neededObjects {
		if r.objectStorageBuffer != nil {
			r.objectStorageBuffer.Destroy()
		}
		if r.objectStorageBindGroup != nil {
			r.objectStorageBindGroup.Release()
		}

		if r.objectStorageCapacity == 0 {
			r.objectStorageCapacity = InitialStorageCapacity
		}

		for r.objectStorageCapacity < neededObjects {
			r.objectStorageCapacity *= 2
		}

		r.objectStorageBuffer, err = r.runtime.Device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "Object storage buffer",
			Size:  uint64(r.objectStorageCapacity) * uint64(unsafe.Sizeof(glm.Mat4f{})),
			Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			return err
		}

		r.objectStorageBindGroup, err = r.runtime.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Label:  "Object bind group",
			Layout: r.objectStorageBindGroupLayout,
			Entries: []wgpu.BindGroupEntry{
				{
					Binding: 0,
					Buffer:  r.objectStorageBuffer,
					Offset:  0,
					Size:    wgpu.WholeSize,
				},
			},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *Renderer) Render(scene *Scene, camera Camera) error {
	r.Stats.NextFrame()
	start := time.Now()

	//Update local/world matrices
	updateMatrix(scene, false)

	// Extract frustum planes
	frustumPlanes := planesFromViewProjection(camera.ViewProjection())

	//Extract objects
	visibleObjects := viewableMeshesPool.Get().([]*Mesh)
	defer viewableMeshesPool.Put(visibleObjects[:0])
	visibleObjects = r.appendViewable(visibleObjects, scene, frustumPlanes)

	renderables := renderablesPool.Get().([]renderable)
	defer renderablesPool.Put(renderables[:0])

	models := modelsPool.Get().([]glm.Mat4f)
	defer modelsPool.Put(models[:0])

	//Prepare Objects
	for _, mesh := range visibleObjects {
		geometryData := mesh.geometry
		geometry := r.resources.GetGeometry(geometryData)

		if geometry.version < geometryData.version {
			if geometry.version == 0 {
				geometry.layout = createVertexLayout(geometryData)
			}
			err := r.resources.uploadGeometry(r.runtime.Device, geometryData)
			if err != nil {
				r.logger.Error("error uploading geometry", slog.Any("err", err))
			}
		}

		materialData := mesh.material
		material := r.resources.GetMaterialByData(materialData)
		material, err := prepareMaterial(r.runtime.Device, materialData, material, &r.resources)

		if err != nil {
			r.logger.Error("error preparing material", slog.Any("err", err))
			continue
		}

		// FIXME: the renderer shouldn't need to know what resources use to store
		r.resources.SetMaterial(materialData.slot, material)

		renderables = append(renderables, renderable{
			geometry: *geometry,
			material: material,
		})

		models = append(models, mesh.Model())

	}

	// Update Global Uniforms
	viewProj := camera.ViewProjection()
	if err := r.runtime.Queue.WriteBuffer(r.globalUniformBuffer, 0, wgpu.ToBytes(viewProj[:])); err != nil {
		r.logger.Error("error updating global uniform", slog.Any("err", err))
	}

	// Batch update object matrices using storage buffer
	if count := len(models); count > 0 {
		r.ensureObjectStorageSize(uint32(count))
		r.runtime.Queue.WriteBuffer(r.objectStorageBuffer, 0, wgpu.ToBytes(models))
	}

	// Begin rendering
	ctx, err := r.beginRendering(scene.background)
	if err != nil {
		return err
	}

	// Draw objects
	for i, renderable := range renderables {
		if err := r.renderObject(renderable, ctx, i); err != nil {
			return err
		}
	}

	// End rendering
	err = r.endRendering(ctx)
	if err != nil {
		return err
	}

	// Process pending resources
	err = r.resources.processPending(r.runtime.Device)
	if err != nil {
		panic(err)
	}

	r.Stats.AddFrameTime(time.Since(start).Seconds())
	//r.runtime.Queue.OnSubmittedWorkDone(func(_ wgpu.QueueWorkDoneStatus) { r.Stats.AddGPUTime(time.Since(start).Seconds()) })
	return nil
}

func (r *Renderer) renderObject(obj renderable, ctx *renderContext, objIdx int) error {
	pipeline, err := r.getPipelineFor(obj)
	if err != nil {
		r.logger.Error("error getting pipeline", slog.Any("err", err))
		return err
	}

	ctx.renderPass.SetPipeline(pipeline)
	ctx.renderPass.SetBindGroup(0, r.globalBindGroup, []uint32{})
	ctx.renderPass.SetBindGroup(1, obj.material.bindGroup, []uint32{})
	ctx.renderPass.SetBindGroup(2, r.objectStorageBindGroup, []uint32{})

	for _, b := range obj.geometry.bufs {
		ctx.renderPass.SetVertexBuffer(uint32(b.loc), b.buf, 0, wgpu.WholeSize)
	}

	if obj.geometry.index != nil {
		//TODO support other formats for index buffers
		ctx.renderPass.SetIndexBuffer(obj.geometry.index, wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
		ctx.renderPass.DrawIndexed(uint32(obj.geometry.count), 1, 0, 0, uint32(objIdx))
	} else {
		ctx.renderPass.Draw(uint32(obj.geometry.count), 1, 0, uint32(objIdx))
	}

	return nil
}

func (r *Renderer) beginRendering(bgColor glm.Color4f) (*renderContext, error) {
	var err error

	ctx := &renderContext{}

	ctx.texture, err = r.runtime.Surface.GetCurrentTexture()
	if err != nil {
		r.logger.Error("error obtaining next frame texture", slog.Any("err", err))
		return nil, err
	}

	ctx.view, err = ctx.texture.CreateView(nil)
	if err != nil {
		r.logger.Error("error creating view", slog.Any("err", err))
		return nil, err
	}

	ctx.encoder, err = r.runtime.Device.CreateCommandEncoder(nil)
	if err != nil {
		r.logger.Error("error creating command encoder", slog.Any("err", err))
		return nil, err
	}

	//temp code
	ctx.renderPass = ctx.encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       ctx.view,
			LoadOp:     wgpu.LoadOpClear,
			StoreOp:    wgpu.StoreOpStore,
			ClearValue: wgpu.Color{R: float64(bgColor.R()), G: (float64(bgColor.G())), B: (float64(bgColor.B())), A: float64(bgColor.A())}, //TODO: make it something the user can define
		}},
	})

	return ctx, nil
}

func (r *Renderer) endRendering(ctx *renderContext) error {
	err := ctx.renderPass.End()
	if err != nil {
		r.logger.Error("error ending render pass", slog.Any("err", err))
		return err
	}

	cmdBuf, err := ctx.encoder.Finish(nil)
	if err != nil {
		r.logger.Error("error creating command buffer", slog.Any("err", err))
		return err
	}

	r.runtime.Queue.Submit(cmdBuf)
	r.runtime.Surface.Present()

	// release resources
	cmdBuf.Release()
	ctx.encoder.Release()
	ctx.view.Release()
	ctx.texture.Release()
	return nil
}

func (r *Renderer) getPipelineFor(obj renderable) (*wgpu.RenderPipeline, error) {
	pipelineKey := renderPipelineKey{
		shaderHash:    obj.material.hash,
		materialFlags: obj.material.flags,
		geometryFlags: obj.geometry.flags,
	}
	pipline := r.pipelineCache.GetRenderPipeline(pipelineKey)

	if pipline != nil {
		return pipline, nil
	}

	pipeline, err := r.createRenderPipeline(obj)
	if err != nil {
		return nil, err
	}

	r.pipelineCache.SetRenderPipeline(pipelineKey, pipeline)
	return pipeline, nil
}

func (r *Renderer) createRenderPipeline(obj renderable) (*wgpu.RenderPipeline, error) {

	layout, err := r.runtime.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label: "", // TODO: add a descriptive name for debugging
		BindGroupLayouts: []*wgpu.BindGroupLayout{
			r.globalBindGroupLayout,
			obj.material.bindGroupLayout,
			r.objectStorageBindGroupLayout,
		},
	})

	if err != nil {
		return nil, err
	}

	defines := createDefines(obj.material.flags, obj.geometry.flags)

	vsModule, err := r.compileShader(r.runtime.Device, obj.material.vertexShader, defines, wgpu.ShaderStageVertex)
	if err != nil {
		return nil, err
	}

	fsModule, err := r.compileShader(r.runtime.Device, obj.material.fragmentShader, defines, wgpu.ShaderStageFragment)
	if err != nil {
		return nil, err
	}

	pipeline, err := r.runtime.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "", //TODO: provide a meaningful name
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     vsModule,
			EntryPoint: "main",
			Buffers:    obj.geometry.layout,
		},
		Fragment: &wgpu.FragmentState{
			Module:     fsModule,
			EntryPoint: "main",
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

func (r *Renderer) compileShader(device *wgpu.Device, code string, defines map[string]string, stage wgpu.ShaderStage) (*wgpu.ShaderModule, error) {
	module, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		GLSLDescriptor: &wgpu.ShaderModuleGLSLDescriptor{Code: code, Defines: defines, ShaderStage: stage},
	})

	return module, err

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
					Type:             wgpu.BufferBindingTypeReadOnlyStorage,
					HasDynamicOffset: false,
					MinBindingSize:   uint64(unsafe.Sizeof(glm.Mat4f{})),
				},
			},
		},
	})

	r.objectStorageBindGroupLayout = layout

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

	r.ensureObjectStorageSize(InitialStorageCapacity)

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
	return nil
}

func (r *Renderer) appendViewable(meshes []*Mesh, node Node, frustumPlanes [6]glm.Vec4f) []*Mesh {
	for _, child := range node.Children() {

		switch object := any(child).(type) {

		case *Mesh:
			if sphereInFrustum(frustumPlanes, object.BoundingSphere()) {
				meshes = append(meshes, object)
			}
		}

		meshes = r.appendViewable(meshes, child, frustumPlanes)
	}

	return meshes
}

func createDefines(matFlags MaterialFlags, geoFlags GeometryFlags) map[string]string {
	defines := make(map[string]string)

	for flags := matFlags; flags != 0; {
		bit := bits.TrailingZeros64(uint64(flags))
		flags &= flags - 1

		name, ok := materialFlagNames[bit]
		if ok {
			defines[name] = "1"
		}
		defines["USE_FLAG"+strconv.Itoa(bit)] = "1"
	}

	for flags := geoFlags; flags != 0; {
		bit := bits.TrailingZeros64(uint64(flags))
		flags &= flags - 1
		name, ok := geometryFlagNames[bit]
		if ok {
			defines[name] = "1"

		}
		defines["USE_GEOMETRY_FLAG"+strconv.Itoa(bit)] = "1"
	}

	return defines
}

func updateMatrix(n Node, force bool) {
	force = n.UpdateMatrix(force)
	for _, child := range n.Children() {
		updateMatrix(child, force)
	}
}

func planesFromViewProjection(viewProj glm.Mat4f) [6]glm.Vec4f {

	return [6]glm.Vec4f{
		//left plane
		glm.Vec4f{
			viewProj[3] + viewProj[0],
			viewProj[7] + viewProj[4],
			viewProj[11] + viewProj[8],
			viewProj[15] + viewProj[12],
		}.Normalize(),

		//right plane
		glm.Vec4f{
			viewProj[3] - viewProj[0],
			viewProj[7] - viewProj[4],
			viewProj[11] - viewProj[8],
			viewProj[15] - viewProj[12],
		}.Normalize(),

		//top  plane
		glm.Vec4f{
			viewProj[3] - viewProj[1],
			viewProj[7] - viewProj[5],
			viewProj[11] - viewProj[9],
			viewProj[15] - viewProj[13],
		}.Normalize(),

		//bottom plane
		glm.Vec4f{
			viewProj[3] + viewProj[1],
			viewProj[7] + viewProj[5],
			viewProj[11] + viewProj[9],
			viewProj[15] + viewProj[13],
		}.Normalize(),

		//near plane
		glm.Vec4f{
			viewProj[3] + viewProj[2],
			viewProj[7] + viewProj[6],
			viewProj[11] + viewProj[10],
			viewProj[15] + viewProj[14],
		}.Normalize(),

		//far plane
		glm.Vec4f{
			viewProj[3] - viewProj[2],
			viewProj[7] - viewProj[6],
			viewProj[11] - viewProj[10],
			viewProj[15] - viewProj[14],
		}.Normalize(),
	}
}

func sphereInFrustum(planes [6]glm.Vec4f, sphere Sphere) bool {
	for _, p := range planes {
		distance :=
			p[0]*sphere.Center[0] +
				p[1]*sphere.Center[1] +
				p[2]*sphere.Center[2] +
				p[3]

		if distance < -sphere.Radius {
			return false // outside
		}
	}
	return true // inside or intersecting
}

func transformSphere(sphere Sphere, model glm.Mat4f) Sphere {
	// transform center
	worldCenter := model.Mul4x1(glm.Vec4f{sphere.Center[0], sphere.Center[1], sphere.Center[2], 1.0})

	// extract max scale (important!)
	sx := glm.Vec3f{model[0], model[1], model[2]}.Length()
	sy := glm.Vec3f{model[4], model[5], model[6]}.Length()
	sz := glm.Vec3f{model[8], model[9], model[10]}.Length()

	maxScale := max(sx, max(sy, sz))

	return Sphere{
		Center: glm.Vec3f{worldCenter[0], worldCenter[1], worldCenter[2]},
		Radius: sphere.Radius * maxScale,
	}
}
