package pix

import (
	"sort"
	"unsafe"

	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/glm"
)

const (
	dbgGlyphW    = 12
	dbgGlyphH    = 16
	dbgAtlasCols = 16
)

// DebugText is a string rendered at screen-pixel coordinates after the main render pass.
// If Color is the zero value, DebugTextColor on the Renderer is used instead.
type DebugText struct {
	Text  string
	X, Y  float32
	Size  float32     // desired glyph height in pixels; 0 = natural (16 px)
	Color glm.Color4f // per-entry color; zero = use Renderer.DebugTextColor
}

// dbgUniforms holds the viewport dimensions for NDC conversion.
// 16 bytes total (vec2 + pad) to satisfy WebGPU uniform alignment.
type dbgUniforms struct {
	viewport [2]float32
	pad      [2]float32
}

type dbgVertex struct{ x, y, u, v, r, g, b, a float32 }

type debugTextRenderer struct {
	pipeline     *wgpu.RenderPipeline
	bgl          *wgpu.BindGroupLayout
	bindGroup    *wgpu.BindGroup
	uniformBuf   *wgpu.Buffer
	atlasTex     *wgpu.Texture
	atlasView    *wgpu.TextureView
	atlasSampler *wgpu.Sampler
	vertexBuf    *wgpu.Buffer
	vertexCap    int

	glyphIndex     map[int]int
	atlasW, atlasH int
}

func (r *Renderer) initDebugText() {
	dt := &debugTextRenderer{}
	dt.buildAtlas(r.runtime.Device, r.runtime.Queue)
	dt.createPipeline(r)
	r.debugText = dt
}

// buildAtlas rasterises every glyph from the font map into an R8Unorm texture atlas.
func (dt *debugTextRenderer) buildAtlas(device *wgpu.Device, queue *wgpu.Queue) {
	codepoints := make([]int, 0, len(font))
	for cp := range font {
		codepoints = append(codepoints, cp)
	}
	sort.Ints(codepoints)

	dt.glyphIndex = make(map[int]int, len(codepoints))
	for i, cp := range codepoints {
		dt.glyphIndex[cp] = i
	}

	n := len(codepoints)
	rows := (n + dbgAtlasCols - 1) / dbgAtlasCols
	dt.atlasW = dbgAtlasCols * dbgGlyphW
	dt.atlasH = rows * dbgGlyphH

	pixels := make([]byte, dt.atlasW*dt.atlasH)
	for cp, idx := range dt.glyphIndex {
		acol := idx % dbgAtlasCols
		arow := idx / dbgAtlasCols
		for gy, rowBits := range font[cp] {
			for gx := 0; gx < dbgGlyphW; gx++ {
				if (uint16(rowBits)>>uint(gx))&1 != 0 {
					pixels[(arow*dbgGlyphH+gy)*dt.atlasW+acol*dbgGlyphW+gx] = 255
				}
			}
		}
	}

	dt.atlasTex = device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Debug Font Atlas",
		Usage:         wgpu.TextureUsageCopyDst | wgpu.TextureUsageTextureBinding,
		Dimension:     wgpu.TextureDimension2D,
		Size:          wgpu.Extent3D{Width: uint32(dt.atlasW), Height: uint32(dt.atlasH), DepthOrArrayLayers: 1},
		Format:        wgpu.TextureFormatR8Unorm,
		MipLevelCount: 1,
		SampleCount:   1,
	})

	queue.WriteTexture(
		wgpu.TexelCopyTextureInfo{Texture: dt.atlasTex, MipLevel: 0, Origin: wgpu.Origin3D{}, Aspect: wgpu.TextureAspectAll},
		pixels,
		wgpu.TexelCopyBufferLayout{Offset: 0, BytesPerRow: uint32(dt.atlasW), RowsPerImage: uint32(dt.atlasH)},
		wgpu.Extent3D{Width: uint32(dt.atlasW), Height: uint32(dt.atlasH), DepthOrArrayLayers: 1},
	)

	dt.atlasView = dt.atlasTex.CreateView(nil)
	dt.atlasSampler = device.CreateSampler(&wgpu.SamplerDescriptor{
		AddressModeU:  wgpu.AddressModeClampToEdge,
		AddressModeV:  wgpu.AddressModeClampToEdge,
		AddressModeW:  wgpu.AddressModeClampToEdge,
		MagFilter:     wgpu.FilterModeNearest,
		MinFilter:     wgpu.FilterModeNearest,
		MipmapFilter:  wgpu.MipmapFilterModeNearest,
		MaxAnisotropy: 1,
	})
}

func (dt *debugTextRenderer) createPipeline(r *Renderer) {
	device := r.runtime.Device

	uniformSize := uint64(unsafe.Sizeof(dbgUniforms{}))
	dt.bgl = device.CreateBindGroupLayout(wgpu.BindGroupLayoutDescriptor{
		Label: "DebugText BGL",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageVertex,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeUniform,
					MinBindingSize: uniformSize,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			{
				Binding:    2,
				Visibility: wgpu.ShaderStageFragment,
				Sampler: wgpu.SamplerBindingLayout{
					Type: wgpu.SamplerBindingTypeNonFiltering,
				},
			},
		},
	})

	dt.uniformBuf = device.CreateBuffer(wgpu.BufferDescriptor{
		Label: "DebugText Uniforms",
		Size:  uniformSize,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})

	dt.bindGroup = device.CreateBindGroup(wgpu.BindGroupDescriptor{
		Label:  "DebugText BindGroup",
		Layout: dt.bgl,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: dt.uniformBuf, Offset: 0, Size: uniformSize},
			{Binding: 1, TextureView: dt.atlasView},
			{Binding: 2, Sampler: dt.atlasSampler},
		},
	})

	layout := device.CreatePipelineLayout(wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{dt.bgl},
	})
	defer layout.Release()

	vert := r.compileShader(device, "debug_text_vertex.wesl", nil)
	defer vert.Release()
	frag := r.compileShader(device, "debug_text_fragment.wesl", nil)
	defer frag.Release()

	dt.pipeline = device.CreateRenderPipeline(wgpu.RenderPipelineDescriptor{
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     vert,
			EntryPoint: "main",
			Buffers: []wgpu.VertexBufferLayout{{
				ArrayStride: uint64(unsafe.Sizeof(dbgVertex{})),
				StepMode:    wgpu.VertexStepModeVertex,
				Attributes: []wgpu.VertexAttribute{
					{ShaderLocation: 0, Offset: 0, Format: wgpu.VertexFormatFloat32x2},
					{ShaderLocation: 1, Offset: 8, Format: wgpu.VertexFormatFloat32x2},
					{ShaderLocation: 2, Offset: 16, Format: wgpu.VertexFormatFloat32x4},
				},
			}},
		},
		Fragment: &wgpu.FragmentState{
			Module:     frag,
			EntryPoint: "main",
			Targets: []wgpu.ColorTargetState{{
				Format: r.runtime.Format,
				Blend: &wgpu.BlendState{
					Color: wgpu.BlendComponent{
						Operation: wgpu.BlendOperationAdd,
						SrcFactor: wgpu.BlendFactorSrcAlpha,
						DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
					},
					Alpha: wgpu.BlendComponent{
						Operation: wgpu.BlendOperationAdd,
						SrcFactor: wgpu.BlendFactorOne,
						DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
					},
				},
				WriteMask: wgpu.ColorWriteMaskAll,
			}},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopologyTriangleList,
			FrontFace: wgpu.FrontFaceCCW,
			CullMode:  wgpu.CullModeNone,
		},
		Multisample: wgpu.MultisampleState{
			Count: 1,
			Mask:  0xFFFFFFFF,
		},
	})
}

func (dt *debugTextRenderer) ensureVertexCap(device *wgpu.Device, need int) {
	if dt.vertexBuf != nil && dt.vertexCap >= need {
		return
	}
	if dt.vertexBuf != nil {
		dt.vertexBuf.Destroy()
	}
	cap := 1024
	for cap < need {
		cap *= 2
	}
	dt.vertexCap = cap
	dt.vertexBuf = device.CreateBuffer(wgpu.BufferDescriptor{
		Label: "DebugText Vertex Buffer",
		Size:  uint64(cap) * uint64(unsafe.Sizeof(dbgVertex{})),
		Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
	})
}

func (dt *debugTextRenderer) render(r *Renderer, ctx *renderContext, texts []DebugText, defaultColor glm.Color4f) {
	var verts []dbgVertex
	for _, t := range texts {
		c := t.Color
		if c == (glm.Color4f{}) {
			c = defaultColor
		}
		cr, cg, cb, ca := c[0], c[1], c[2], c[3]

		pixH := t.Size
		if pixH <= 0 {
			pixH = float32(dbgGlyphH)
		}
		scale := pixH / float32(dbgGlyphH)
		gw := float32(dbgGlyphW) * scale
		gh := pixH
		cx := t.X
		for _, ch := range t.Text {
			idx, ok := dt.glyphIndex[int(ch)]
			if !ok {
				cx += gw
				continue
			}
			acol := idx % dbgAtlasCols
			arow := idx / dbgAtlasCols
			u0 := float32(acol*dbgGlyphW) / float32(dt.atlasW)
			u1 := float32(acol*dbgGlyphW+dbgGlyphW) / float32(dt.atlasW)
			v0 := float32(arow*dbgGlyphH) / float32(dt.atlasH)
			v1 := float32(arow*dbgGlyphH+dbgGlyphH) / float32(dt.atlasH)
			x0, y0 := cx, t.Y
			x1, y1 := cx+gw, t.Y+gh
			verts = append(verts,
				dbgVertex{x0, y0, u0, v0, cr, cg, cb, ca},
				dbgVertex{x1, y0, u1, v0, cr, cg, cb, ca},
				dbgVertex{x0, y1, u0, v1, cr, cg, cb, ca},
				dbgVertex{x1, y0, u1, v0, cr, cg, cb, ca},
				dbgVertex{x1, y1, u1, v1, cr, cg, cb, ca},
				dbgVertex{x0, y1, u0, v1, cr, cg, cb, ca},
			)
			cx += gw
		}
	}
	if len(verts) == 0 {
		return
	}

	dt.ensureVertexCap(r.runtime.Device, len(verts))
	queue := r.runtime.Queue

	u := dbgUniforms{viewport: [2]float32{float32(r.width), float32(r.height)}}
	queue.WriteBuffer(dt.uniformBuf, 0, wgpu.ToBytes([]dbgUniforms{u}))
	queue.WriteBuffer(dt.vertexBuf, 0, wgpu.ToBytes(verts))

	encoder := r.runtime.Device.CreateCommandEncoder(nil)
	pass := encoder.BeginRenderPass(wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:    ctx.view,
			LoadOp:  wgpu.LoadOpLoad,
			StoreOp: wgpu.StoreOpStore,
		}},
	})
	pass.SetPipeline(dt.pipeline)
	pass.SetBindGroup(0, dt.bindGroup, nil)
	pass.SetVertexBuffer(0, dt.vertexBuf, 0, wgpu.WholeSize)
	pass.Draw(uint32(len(verts)), 1, 0, 0)
	pass.End()
	pass.Release()

	cmdBuf := encoder.Finish(nil)
	queue.Submit(cmdBuf)
	cmdBuf.Release()
	encoder.Release()
}

func (dt *debugTextRenderer) destroy() {
	if dt.pipeline != nil {
		dt.pipeline.Release()
	}
	if dt.bindGroup != nil {
		dt.bindGroup.Release()
	}
	if dt.bgl != nil {
		dt.bgl.Release()
	}
	if dt.uniformBuf != nil {
		dt.uniformBuf.Destroy()
	}
	if dt.atlasSampler != nil {
		dt.atlasSampler.Release()
	}
	if dt.atlasView != nil {
		dt.atlasView.Release()
	}
	if dt.atlasTex != nil {
		dt.atlasTex.Destroy()
	}
	if dt.vertexBuf != nil {
		dt.vertexBuf.Destroy()
	}
}
