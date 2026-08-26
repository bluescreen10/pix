package vulkan

import (
	"testing"
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/shaders"
)

// vertex3D matches Vertex3D in shaders/mesh.vert (scalar, 24 bytes).
type vertex3D struct {
	px, py, pz float32
	cr, cg, cb float32
}

// meshRoot matches MeshRoot in shaders/mesh.vert (mat4 @0, ptr @64).
type meshRoot struct {
	mvp   [16]float32
	verts uint64
}

// TestCube renders a transformed, depth-tested indexed cube through the gpu —
// the core of pix's mesh rendering (camera+model transform, index buffer, depth).
func TestCube(t *testing.T) {
	b := New()
	err := b.Init()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()

	const size = 128
	color := b.CreateTexture(gpu.TextureDescriptor{
		Kind: gpu.Texture2D, Width: size, Height: size, Format: gpu.FormatRGBA8Unorm,
		Usage: gpu.TextureRenderTarget | gpu.TextureTransfer, Label: "color",
	})
	depth := b.CreateTexture(gpu.TextureDescriptor{
		Kind: gpu.Texture2D, Width: size, Height: size, Format: gpu.FormatDepth32F,
		Usage: gpu.TextureDepth, Label: "depth",
	})

	// Unit cube centred at origin, 8 corners, colours from position.
	corners := [8][3]float32{
		{-0.5, -0.5, -0.5}, {0.5, -0.5, -0.5}, {0.5, 0.5, -0.5}, {-0.5, 0.5, -0.5},
		{-0.5, -0.5, 0.5}, {0.5, -0.5, 0.5}, {0.5, 0.5, 0.5}, {-0.5, 0.5, 0.5},
	}
	vb := b.Alloc(uint64(len(corners))*24, gpu.MemoryHost, "verts")
	vs := unsafe.Slice((*vertex3D)(vb.Ptr), len(corners))
	for i, c := range corners {
		vs[i] = vertex3D{c[0], c[1], c[2], c[0] + 0.5, c[1] + 0.5, c[2] + 0.5}
	}
	indices := []uint32{
		0, 1, 2, 0, 2, 3, // back
		4, 6, 5, 4, 7, 6, // front
		0, 4, 5, 0, 5, 1, // bottom
		3, 2, 6, 3, 6, 7, // top
		0, 3, 7, 0, 7, 4, // left
		1, 5, 6, 1, 6, 2, // right
	}
	ib := b.Alloc(uint64(len(indices))*4, gpu.MemoryHost, "indices")
	copy(unsafe.Slice((*uint32)(ib.Ptr), len(indices)), indices)

	// MVP = perspective * lookAt * model(identity).
	proj := glm.PerspectiveRH[float32](glm.ToRadians(float32(45)), 1, 0.1, 100)
	view := glm.LookAtRH(glm.Vec3f{2, 2, 3}, glm.Vec3f{0, 0, 0}, glm.Vec3f{0, 1, 0})
	mvp := proj.Mul4x4(view)

	rb := b.Alloc(128, gpu.MemoryHost, "root")
	root := (*meshRoot)(rb.Ptr)
	root.mvp = mvp
	root.verts = vb.Addr

	pipe := b.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader: shaders.MeshVert, FragmentShader: shaders.MeshFrag,
		Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{gpu.FormatRGBA8Unorm},
		DepthFormat: gpu.FormatDepth32F, DepthTest: true, DepthWrite: true, DepthCompare: gpu.CompareLess,
		CullMode: gpu.CullNone, Label: "mesh",
	})

	readback := b.Alloc(size*size*4, gpu.MemoryHost, "readback")

	cl := b.Begin()
	cl.BeginRendering(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: color, Load: gpu.LoadClear, Clear: [4]float32{0, 0, 0, 1}}},
		Depth: &gpu.DepthAttachment{Texture: depth, Load: gpu.LoadClear, Clear: 1.0},
	})
	cl.SetPipeline(pipe)
	cl.Root(rb.Addr)
	cl.Viewport(0, 0, size, size, 0, 1)
	cl.Scissor(0, 0, size, size)
	cl.DrawIndexed(ib, uint32(len(indices)), 1, 0, 0, 0)
	cl.EndRendering()
	cl.CopyTextureToBuffer(readback, color, 0, 0)
	f := b.Submit(cl)
	b.Wait(f)

	px := unsafe.Slice((*byte)(readback.Ptr), size*size*4)
	lit := 0
	for i := 0; i < len(px); i += 4 {
		if px[i] != 0 || px[i+1] != 0 || px[i+2] != 0 {
			lit++
		}
	}
	frac := float64(lit) / float64(size*size)
	t.Logf("cube coverage: %.1f%% of pixels lit", frac*100)
	if frac < 0.1 || frac > 0.95 {
		t.Fatalf("unexpected cube coverage %.1f%% — transform/depth likely wrong", frac*100)
	}
	i := (size/2*size + size/2) * 4
	t.Logf("center pixel = (%d,%d,%d)", px[i], px[i+1], px[i+2])
}
