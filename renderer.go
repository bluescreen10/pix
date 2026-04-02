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
	"github.com/oliverbestmann/webgpu/wgpu"
)

const (
	InitialStorageCapacity = 1024
	MaxDirectionalLights   = 5
)

var instancesPool = sync.Pool{
	New: func() any {
		return make(InstancesUniform, 0, InitialStorageCapacity)
	},
}

var viewableMeshesPool = sync.Pool{
	New: func() any {
		return make([]*Mesh, 0, 4096)
	},
}

var viewableDirectionalLights = sync.Pool{
	New: func() any {
		return make([]*DirectionalLight, 0, MaxDirectionalLights)
	},
}

var drawingsPool = sync.Pool{
	New: func() any {
		return make([]drawing, 0, 4096)
	},
}

type renderContext struct {
	texture *wgpu.Texture
	view    *wgpu.TextureView
	encoder *wgpu.CommandEncoder
}

type Renderer struct {
	resources resourceManager

	runtime       *wgpuRuntime
	width, height uint32
	frameCount    uint32
	logger        *slog.Logger

	pipelineCache *pipelineCache

	//global
	cameraUniformBuffer   *wgpu.Buffer
	lightsUniformBuffer   *wgpu.Buffer
	globalBindGroupLayout *wgpu.BindGroupLayout
	globalBindGroup       *wgpu.BindGroup

	//instance
	instanceStorageBuffer          *wgpu.Buffer
	instanceStorageBindGroupLayout *wgpu.BindGroupLayout
	instanceStorageBindGroup       *wgpu.BindGroup
	instanceStorageCapacity        uint32

	//depth buffer
	depthTexture     *wgpu.Texture
	depthTextureView *wgpu.TextureView

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
	r.createGlobalResources()
	return nil
}

func (r *Renderer) Destroy() {
	r.runtime.Destroy()
	r.runtime = nil

	if r.instanceStorageBindGroup != nil {
		r.instanceStorageBindGroup.Release()
		r.instanceStorageBindGroup = nil
	}

	r.cameraUniformBuffer.Destroy()
	r.cameraUniformBuffer = nil

	r.lightsUniformBuffer.Destroy()
	r.lightsUniformBuffer = nil

	if r.instanceStorageBuffer != nil {
		r.instanceStorageBuffer.Destroy()
		r.instanceStorageBuffer = nil
	}

	r.globalBindGroupLayout.Release()
	r.globalBindGroupLayout = nil

	if r.instanceStorageBindGroupLayout != nil {
		r.instanceStorageBindGroupLayout.Release()
		r.instanceStorageBindGroupLayout = nil
	}

	if r.depthTextureView != nil {
		r.depthTextureView.Release()
		r.depthTexture = nil
	}

	if r.depthTexture != nil {
		r.depthTexture.Destroy()
		r.depthTexture = nil
	}

	r.resources.destroy()
}

func (r *Renderer) ensureInstanceStorageSize(needInstances uint32) {
	if r.instanceStorageBuffer == nil || r.instanceStorageCapacity < needInstances {
		if r.instanceStorageBuffer != nil {
			r.instanceStorageBuffer.Destroy()
		}
		if r.instanceStorageBindGroup != nil {
			r.instanceStorageBindGroup.Release()
		}

		if r.instanceStorageCapacity == 0 {
			r.instanceStorageCapacity = InitialStorageCapacity
		}

		for r.instanceStorageCapacity < needInstances {
			r.instanceStorageCapacity *= 2
		}

		r.instanceStorageBuffer = r.runtime.Device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "Instance storage buffer",
			Size:  uint64(r.instanceStorageCapacity) * uint64(unsafe.Sizeof(InstanceUniform{})),
			Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
		})

		r.instanceStorageBindGroup = r.runtime.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Label:  "Instance bind group",
			Layout: r.instanceStorageBindGroupLayout,
			Entries: []wgpu.BindGroupEntry{
				{
					Binding: 0,
					Buffer:  r.instanceStorageBuffer,
					Offset:  0,
					Size:    wgpu.WholeSize,
				},
			},
		})
	}
}

func (r *Renderer) ensureDepthTextureSize(width, height uint32) {
	if r.depthTexture != nil && r.depthTexture.GetWidth() == width && r.depthTexture.GetHeight() == r.height {
		return
	}

	if r.depthTexture != nil {
		r.depthTextureView.Release()
		r.depthTexture.Destroy()
	}

	r.depthTexture = r.runtime.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Depth Texture",
		Usage:         wgpu.TextureUsageRenderAttachment,
		Dimension:     wgpu.TextureDimension2D,
		MipLevelCount: 1,
		SampleCount:   1,
		Format:        wgpu.TextureFormatDepth24Plus,
		Size: wgpu.Extent3D{
			Width:              width,
			Height:             height,
			DepthOrArrayLayers: 1,
		},
	})

	r.depthTextureView = r.depthTexture.CreateView(nil)
}

type renderList struct {
	meshes            []*Mesh
	directionalLights []*DirectionalLight
}

func (o *renderList) init() {
	o.meshes = viewableMeshesPool.Get().([]*Mesh)
	o.directionalLights = viewableDirectionalLights.Get().([]*DirectionalLight)
}

func (o *renderList) release() {
	viewableMeshesPool.Put(o.meshes[:0])
	viewableDirectionalLights.Put(o.directionalLights[:0])
}

func (r *Renderer) Render(scene *Scene, camera Camera) {
	var ctx renderContext

	//Acquire next texture
	r.acquireNextFrame(&ctx)

	//Update stats
	r.Stats.NextFrame()
	start := time.Now()

	//Update local/world matrices
	updateMatrix(scene, false)

	//Cull Scene
	var list renderList
	list.init()
	defer list.release()

	viewProjection := camera.ViewProjection()
	frustum := NewFrustumFromViewProjection(viewProjection)
	r.cullScene(&list, scene, frustum)

	drawings := drawingsPool.Get().([]drawing)
	defer drawingsPool.Put(drawings[:0])

	instances := instancesPool.Get().(InstancesUniform)
	defer instancesPool.Put(instances[:0])

	var useLights bool

	//Prepare Instances
	for _, mesh := range list.meshes {
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

		if material.isLit {
			useLights = true
		}

		drawings = append(drawings, drawing{
			geometry: *geometry,
			material: material,
		})

		instances = append(instances, InstanceUniform{mesh.Model(), mesh.InvModel()})
	}

	//Batch update object matrices using storage buffer
	if count := len(instances); count > 0 {
		r.ensureInstanceStorageSize(uint32(count))
		r.runtime.Queue.WriteBuffer(r.instanceStorageBuffer, 0, instances.Bytes())
	}

	//Update Global Uniforms
	cameraUniform := CameraUniform{
		viewProj: viewProjection,
		position: camera.Position().Vec4(),
	}
	r.runtime.Queue.WriteBuffer(r.cameraUniformBuffer, 0, cameraUniform.Bytes())

	if useLights {
		var lights LightsUniform

		//Directional lights
		count := min(MaxDirectionalLights, len(list.directionalLights))
		lights.DirectionalLightCount = uint32(count)
		for i, l := range list.directionalLights[:count] {
			lights.DirectionalLights[i] = DirectionalLightUniform{
				color:     l.color.RGBA(),
				direction: l.target.Sub(l.pos).Normalize().Vec4(),
			}
		}

		//Write light buffers
		r.runtime.Queue.WriteBuffer(r.lightsUniformBuffer, 0, lights.Bytes())
	}

	//Begin rendering
	renderPass := r.beginRendering(&ctx, scene.background)

	//Draw instances
	for i, drawing := range drawings {
		r.renderInstance(renderPass, drawing, i)
	}

	//End rendering
	r.endRendering(&ctx, renderPass)
	r.Stats.AddFrameTime(time.Since(start).Seconds())

	//Present Frame
	r.presentFrame(&ctx)

	// Process pending resources
	r.resources.processPending(r.runtime.Device)
}

func (r *Renderer) renderInstance(pass *wgpu.RenderPassEncoder, obj drawing, objIdx int) {
	pipeline := r.getPipelineFor(obj)
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, r.globalBindGroup, []uint32{})
	pass.SetBindGroup(1, obj.material.bindGroup, []uint32{})
	pass.SetBindGroup(2, r.instanceStorageBindGroup, []uint32{})

	for _, b := range obj.geometry.bufs {
		pass.SetVertexBuffer(uint32(b.loc), b.buf, 0, wgpu.WholeSize)
	}

	if obj.geometry.index != nil {
		//TODO support other formats for index buffers
		pass.SetIndexBuffer(obj.geometry.index, wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
		pass.DrawIndexed(uint32(obj.geometry.count), 1, 0, 0, uint32(objIdx))
	} else {
		pass.Draw(uint32(obj.geometry.count), 1, 0, uint32(objIdx))
	}
}

func (r *Renderer) acquireNextFrame(ctx *renderContext) {
	ctx.texture = r.runtime.Surface.GetCurrentTexture()
	ctx.view = ctx.texture.CreateView(nil)
}

func (r *Renderer) presentFrame(ctx *renderContext) {
	r.runtime.Surface.Present()
	ctx.view.Release()
	ctx.view = nil

	ctx.texture.Release()
	ctx.texture = nil
}

func (r *Renderer) beginRendering(ctx *renderContext, bgColor glm.Color4f) *wgpu.RenderPassEncoder {
	ctx.encoder = r.runtime.Device.CreateCommandEncoder(nil)

	//temp code
	r.ensureDepthTextureSize(ctx.texture.GetWidth(), ctx.texture.GetHeight())

	return ctx.encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       ctx.view,
			LoadOp:     wgpu.LoadOpClear,
			StoreOp:    wgpu.StoreOpStore,
			ClearValue: wgpu.Color{R: float64(bgColor.R()), G: (float64(bgColor.G())), B: (float64(bgColor.B())), A: float64(bgColor.A())}, //TODO: make it something the user can define
		}},
		DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{
			View:            r.depthTextureView,
			DepthLoadOp:     wgpu.LoadOpClear,
			DepthStoreOp:    wgpu.StoreOpStore,
			DepthClearValue: 1.0,
		},
	})
}

func (r *Renderer) endRendering(ctx *renderContext, pass *wgpu.RenderPassEncoder) {
	pass.End()
	pass.Release()

	cmdBuf := ctx.encoder.Finish(nil)
	r.runtime.Queue.Submit(cmdBuf)

	// release resources
	cmdBuf.Release()
	ctx.encoder.Release()
	ctx.encoder = nil
}

func (r *Renderer) getPipelineFor(obj drawing) *wgpu.RenderPipeline {
	pipelineKey := renderPipelineKey{
		shaderHash:    obj.material.hash,
		materialFlags: obj.material.flags,
		geometryFlags: obj.geometry.flags,
	}
	pipline := r.pipelineCache.GetRenderPipeline(pipelineKey)
	if pipline != nil {
		return pipline
	}

	pipeline := r.createRenderPipeline(obj)
	r.pipelineCache.SetRenderPipeline(pipelineKey, pipeline)
	return pipeline
}

func (r *Renderer) createRenderPipeline(obj drawing) *wgpu.RenderPipeline {

	layout := r.runtime.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label: "", // TODO: add a descriptive name for debugging
		BindGroupLayouts: []*wgpu.BindGroupLayout{
			r.globalBindGroupLayout,
			obj.material.bindGroupLayout,
			r.instanceStorageBindGroupLayout,
		},
	})

	defines := createDefines(obj.material.flags, obj.geometry.flags)
	vsModule := r.compileShader(r.runtime.Device, obj.material.vertexShader, defines, wgpu.ShaderStageVertex)
	fsModule := r.compileShader(r.runtime.Device, obj.material.fragmentShader, defines, wgpu.ShaderStageFragment)

	pipeline := r.runtime.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
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
		DepthStencil: &wgpu.DepthStencilState{
			Format:            wgpu.TextureFormatDepth24Plus,
			DepthWriteEnabled: wgpu.OptionalBoolTrue,
			DepthCompare:      wgpu.CompareFunctionLess,
			StencilFront: wgpu.StencilFaceState{
				Compare:     wgpu.CompareFunctionAlways,
				FailOp:      wgpu.StencilOperationKeep,
				DepthFailOp: wgpu.StencilOperationKeep,
				PassOp:      wgpu.StencilOperationKeep,
			},
			StencilBack: wgpu.StencilFaceState{
				Compare:     wgpu.CompareFunctionAlways,
				FailOp:      wgpu.StencilOperationKeep,
				DepthFailOp: wgpu.StencilOperationKeep,
				PassOp:      wgpu.StencilOperationKeep,
			},
			StencilReadMask:  0xFFFFFFFF,
			StencilWriteMask: 0xFFFFFFFF,
		},
		Multisample: wgpu.MultisampleState{
			Count:                  1,
			Mask:                   0xFFFFFFFF,
			AlphaToCoverageEnabled: false,
		},
	})

	return pipeline
}

func (r *Renderer) compileShader(device *wgpu.Device, code string, defines map[string]string, stage wgpu.ShaderStage) *wgpu.ShaderModule {
	module := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		GLSLSource: &wgpu.ShaderSourceGLSL{Code: code, Defines: defines, ShaderStage: stage},
	})

	return module
}

func (r *Renderer) createGlobalResources() {
	r.createGlobalBindGroupLayouts()
	r.createGlobalBuffers()
	r.createGlobalBindGroups()
}

func (r *Renderer) createGlobalBindGroupLayouts() {
	r.globalBindGroupLayout = r.runtime.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Global Lit Bind Group Layout",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0, //TODO: Make it a constant
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,

				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					HasDynamicOffset: false,
					MinBindingSize:   uint64(unsafe.Sizeof(CameraUniform{})),
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					HasDynamicOffset: false,
					MinBindingSize:   uint64(unsafe.Sizeof(LightsUniform{})),
				},
			},
		},
	})

	r.instanceStorageBindGroupLayout = r.runtime.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Instance/Model Bind Group Layout",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0, //TODO: Make it a constant
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,

				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeReadOnlyStorage,
					HasDynamicOffset: false,
					MinBindingSize:   uint64(unsafe.Sizeof(InstanceUniform{})),
				},
			},
		},
	})
}

func (r *Renderer) createGlobalBuffers() {
	r.cameraUniformBuffer = r.runtime.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "Camera uniform buffer",
		Size:  uint64(unsafe.Sizeof(CameraUniform{})), //TODO: use an actual uniform
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})

	r.lightsUniformBuffer = r.runtime.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "Lights uniform buffer",
		Size:  uint64(unsafe.Sizeof(LightsUniform{})), //TODO: use an actual uniform
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})

	r.ensureInstanceStorageSize(InitialStorageCapacity)
}

func (r *Renderer) createGlobalBindGroups() {
	r.globalBindGroup = r.runtime.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "Global Lit bind group",
		Layout: r.globalBindGroupLayout,
		Entries: []wgpu.BindGroupEntry{
			{
				Binding: 0, //TODO: use a constant
				Buffer:  r.cameraUniformBuffer,
				Offset:  0,
				Size:    wgpu.WholeSize,
			},
			{
				Binding: 1,
				Buffer:  r.lightsUniformBuffer,
				Offset:  0,
				Size:    wgpu.WholeSize,
			},
		},
	})
}

func (r *Renderer) cullScene(list *renderList, node Node, frustum Frustum) {
	for _, child := range node.Children() {

		switch object := any(child).(type) {

		case *Mesh:
			if frustum.ContainsSphere(object.BoundingSphere()) {
				list.meshes = append(list.meshes, object)
			}
		case *DirectionalLight:
			list.directionalLights = append(list.directionalLights, object)
		}

		r.cullScene(list, child, frustum)
	}
}

func createDefines(matFlags MaterialFlags, geoFlags GeometryFlags) map[string]string {
	defines := map[string]string{
		//sets
		"GLOBAL_SET":             "0",
		"MATERIAL_SET":           "1",
		"INSTANCE_SET":           "2",
		"MAX_DIRECTIONAL_LIGHTS": strconv.Itoa(MaxDirectionalLights),
	}

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
