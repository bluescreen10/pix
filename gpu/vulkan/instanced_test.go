package vulkan

import (
	"math"
	"testing"
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/shaders"
)

// gpuDrawable matches Drawable in shaders/instanced.{comp,vert} (scalar, 20 bytes).
type gpuDrawable struct {
	bounds      [4]float32 // xyz = local center, w = radius
	transformID uint32
}

// cullRoot matches CullRoot in shaders/instanced.comp (scalar; pointers first,
// count, then planes at offset 36 — scalar packs vec4 at 4-byte alignment).
type cullRoot struct {
	drawables uint64
	models    uint64
	indirect  uint64
	visible   uint64
	count     uint32
	planes    [6][4]float32
}

// drawRoot matches DrawRoot in shaders/instanced.vert.
type drawRoot struct {
	viewProj  [16]float32
	verts     uint64
	models    uint64
	drawables uint64
	visible   uint64
}

func translate(x, y, z float32) glm.Mat4f {
	var m glm.Mat4f // column-major
	m[0], m[5], m[10], m[15] = 1, 1, 1, 1
	m[12], m[13], m[14] = x, y, z
	return m
}

// extractPlanes derives 6 normalized inward frustum planes from a column-major
// view-projection (Gribb-Hartmann; near uses row2 for 0..1 clip depth).
func extractPlanes(vp glm.Mat4f) [6][4]float32 {
	row := func(i int) [4]float32 { return [4]float32{vp[i], vp[4+i], vp[8+i], vp[12+i]} }
	r0, r1, r2, r3 := row(0), row(1), row(2), row(3)
	comb := func(a, b [4]float32, s float32) [4]float32 {
		return [4]float32{a[0] + s*b[0], a[1] + s*b[1], a[2] + s*b[2], a[3] + s*b[3]}
	}
	planes := [6][4]float32{
		comb(r3, r0, 1), comb(r3, r0, -1),
		comb(r3, r1, 1), comb(r3, r1, -1),
		r2, comb(r3, r2, -1),
	}
	for i := range planes {
		p := planes[i]
		l := float32(math.Sqrt(float64(p[0]*p[0] + p[1]*p[1] + p[2]*p[2])))
		planes[i] = [4]float32{p[0] / l, p[1] / l, p[2] / l, p[3] / l}
	}
	return planes
}

func cubeGeometry(b *Backend) (verts, idx gpu.Buffer, indexCount int) {
	corners := [8][3]float32{{-0.3, -0.3, -0.3}, {0.3, -0.3, -0.3}, {0.3, 0.3, -0.3}, {-0.3, 0.3, -0.3},
		{-0.3, -0.3, 0.3}, {0.3, -0.3, 0.3}, {0.3, 0.3, 0.3}, {-0.3, 0.3, 0.3}}
	verts = b.Alloc(uint64(len(corners))*24, gpu.MemoryHost, "verts")
	vs := unsafe.Slice((*vertex3D)(verts.Ptr), len(corners))
	for i, c := range corners {
		vs[i] = vertex3D{c[0], c[1], c[2], c[0] + 0.5, c[1] + 0.5, c[2] + 0.5}
	}
	indices := []uint32{0, 1, 2, 0, 2, 3, 4, 6, 5, 4, 7, 6, 0, 4, 5, 0, 5, 1, 3, 2, 6, 3, 6, 7, 0, 3, 7, 0, 7, 4, 1, 5, 6, 1, 6, 2}
	idx = b.Alloc(uint64(len(indices))*4, gpu.MemoryHost, "indices")
	copy(unsafe.Slice((*uint32)(idx.Ptr), len(indices)), indices)
	return verts, idx, len(indices)
}

func renderInstances(b *Backend, size int, offsets [][3]float32, eye glm.Vec3f) (px []byte, survivors uint32) {
	color := b.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: uint32(size), Height: uint32(size),
		Format: gpu.FormatRGBA8Unorm, Usage: gpu.TextureRenderTarget | gpu.TextureTransfer})
	depth := b.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: uint32(size), Height: uint32(size),
		Format: gpu.FormatDepth32F, Usage: gpu.TextureDepth})
	verts, idx, indexCount := cubeGeometry(b)

	n := len(offsets)
	models := b.Alloc(uint64(n)*64, gpu.MemoryHost, "models")
	ms := unsafe.Slice((*glm.Mat4f)(models.Ptr), n)
	drawables := b.Alloc(uint64(n)*20, gpu.MemoryHost, "drawables")
	ds := unsafe.Slice((*gpuDrawable)(drawables.Ptr), n)
	for i, o := range offsets {
		ms[i] = translate(o[0], o[1], o[2])
		ds[i] = gpuDrawable{bounds: [4]float32{0, 0, 0, 0.55}, transformID: uint32(i)}
	}
	visible := b.Alloc(uint64(n)*4, gpu.MemoryHost, "visible")

	indirect := b.Alloc(64, gpu.MemoryHost, "indirect")
	args := (*drawIndexedIndirect)(indirect.Ptr)
	*args = drawIndexedIndirect{indexCount: uint32(indexCount)}

	proj := glm.PerspectiveRH[float32](glm.ToRadians(float32(50)), 1, 0.1, 100)
	view := glm.LookAtRH(eye, glm.Vec3f{0, 0, 0}, glm.Vec3f{0, 1, 0})
	vp := proj.Mul4x4(view)

	cr := b.Alloc(256, gpu.MemoryHost, "cull-root")
	*(*cullRoot)(cr.Ptr) = cullRoot{drawables: drawables.Addr, models: models.Addr, indirect: indirect.Addr,
		visible: visible.Addr, count: uint32(n), planes: extractPlanes(vp)}
	dr := b.Alloc(128, gpu.MemoryHost, "draw-root")
	*(*drawRoot)(dr.Ptr) = drawRoot{viewProj: vp, verts: verts.Addr, models: models.Addr,
		drawables: drawables.Addr, visible: visible.Addr}

	cull := b.CreateComputePipeline(gpu.ComputePipelineDescriptor{
		Shader: shaders.InstancedComp,
		Entry:  "main",
		Label:  "cull",
	})
	draw := b.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader: shaders.InstancedVert, FragmentShader: shaders.MeshFrag,
		Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{gpu.FormatRGBA8Unorm},
		DepthFormat: gpu.FormatDepth32F, DepthTest: true, DepthWrite: true, DepthCompare: gpu.CompareLess,
		CullMode: gpu.CullNone,
	})

	readback := b.Alloc(uint64(size*size*4), gpu.MemoryHost, "readback")
	cl := b.Begin()
	cl.SetPipeline(cull)
	cl.Root(cr.Addr)
	cl.Dispatch(uint32((n+63)/64), 1, 1)
	cl.Barrier(gpu.StageCompute, gpu.StageIndirect|gpu.StageVertex, 0)
	cl.BeginRendering(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: color, Load: gpu.LoadClear, Clear: [4]float32{0, 0, 0, 1}}},
		Depth: &gpu.DepthAttachment{Texture: depth, Load: gpu.LoadClear, Clear: 1.0},
	})
	cl.SetPipeline(draw)
	cl.Root(dr.Addr)
	cl.Viewport(0, 0, float32(size), float32(size), 0, 1)
	cl.Scissor(0, 0, int32(size), int32(size))
	cl.DrawIndexedIndirect(idx, indirect, 0, 1, 20)
	cl.EndRendering()
	cl.CopyTextureToBuffer(readback, color, 0, 0)
	f := b.Submit(cl)
	b.Wait(f)

	out := make([]byte, size*size*4)
	copy(out, unsafe.Slice((*byte)(readback.Ptr), size*size*4))
	return out, args.instanceCount
}

func quadCoverage(px []byte, size int) [4]int {
	var quad [4]int
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			i := (y*size + x) * 4
			if px[i] != 0 || px[i+1] != 0 || px[i+2] != 0 {
				q := 0
				if x >= size/2 {
					q |= 1
				}
				if y >= size/2 {
					q |= 2
				}
				quad[q]++
			}
		}
	}
	return quad
}

func TestInstancedGPUDriven(t *testing.T) {
	b := New()
	err := b.Init()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()
	const size = 160
	px, survivors := renderInstances(b, size, [][3]float32{{-0.8, -0.8, 0}, {0.8, -0.8, 0}, {-0.8, 0.8, 0}, {0.8, 0.8, 0}}, glm.Vec3f{0, 0, 4})
	if survivors != 4 {
		t.Fatalf("cull produced %d survivors, want 4", survivors)
	}
	quad := quadCoverage(px, size)
	t.Logf("survivors=%d per-quadrant lit=%v", survivors, quad)
	for i, c := range quad {
		if c < 50 {
			t.Fatalf("quadrant %d nearly empty (%d px) — an instance is missing", i, c)
		}
	}
}

func TestFrustumCull(t *testing.T) {
	b := New()
	err := b.Init()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()
	const size = 160
	// Two instances in front (z=0), two behind the camera (z=+10). Camera at z=4
	// looking -Z, so the far pair is outside the frustum and must be culled.
	offsets := [][3]float32{{-0.8, 0, 0}, {0.8, 0, 0}, {-0.8, 0, 10}, {0.8, 0, 10}}
	px, survivors := renderInstances(b, size, offsets, glm.Vec3f{0, 0, 4})
	t.Logf("survivors=%d (want 2 — two instances are behind the camera)", survivors)
	if survivors != 2 {
		t.Fatalf("frustum cull kept %d instances, want 2", survivors)
	}
	lit := 0
	for i := 0; i < len(px); i += 4 {
		if px[i] != 0 || px[i+1] != 0 || px[i+2] != 0 {
			lit++
		}
	}
	if lit < 100 {
		t.Fatalf("almost nothing drawn (%d px) — in-view instances missing", lit)
	}
	t.Logf("2 of 4 instances survived the frustum cull and rendered (%d px lit)", lit)
}
