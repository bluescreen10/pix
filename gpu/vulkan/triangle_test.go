package vulkan

import (
	"testing"
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/shaders"
)

// vertex matches Vertex in shaders/common.glsl (scalar layout, 20 bytes).
type vertex struct {
	px, py     float32
	cr, cg, cb float32
}

// rootData matches Root in shaders/common.glsl (scalar: vec4 @0, ptr @16).
type rootData struct {
	tint  [4]float32
	verts uint64
}

func TestTriangle(t *testing.T) {
	b := New()
	err := b.Init()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()

	const size = 64

	// Offscreen color target.
	target := b.CreateTexture(gpu.TextureDescriptor{
		Kind: gpu.Texture2D, Width: size, Height: size, Format: gpu.FormatRGBA8Unorm,
		Usage: gpu.TextureRenderTarget | gpu.TextureTransfer, Label: "target",
	})

	// Vertex buffer (host-visible; GPU reads via BDA).
	vb := b.Alloc(256, gpu.MemoryHost, "verts")
	verts := unsafe.Slice((*vertex)(vb.Ptr), 3)
	verts[0] = vertex{-0.9, -0.9, 1, 0, 0} // red
	verts[1] = vertex{0.9, -0.9, 0, 1, 0}  // green
	verts[2] = vertex{0.0, 0.9, 0, 0, 1}   // blue

	// Root struct: white tint + address of the vertex buffer.
	rb := b.Alloc(64, gpu.MemoryHost, "root")
	root := (*rootData)(rb.Ptr)
	root.tint = [4]float32{1, 1, 1, 1}
	root.verts = vb.Addr

	pipe := b.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader: shaders.TriangleVert, FragmentShader: shaders.TriangleFrag,
		Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{gpu.FormatRGBA8Unorm},
		CullMode: gpu.CullNone, Label: "triangle",
	})

	readback := b.Alloc(size*size*4, gpu.MemoryHost, "readback")

	cl := b.Begin()
	cl.BeginRendering(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: target, Load: gpu.LoadClear, Clear: [4]float32{0, 0, 0, 1}}},
	})
	cl.SetPipeline(pipe)
	cl.Root(rb.Addr)
	cl.Viewport(0, 0, size, size, 0, 1)
	cl.Scissor(0, 0, size, size)
	cl.Draw(3, 1, 0, 0)
	cl.EndRendering()
	cl.CopyTextureToBuffer(readback, target, 0, 0)
	f := b.Submit(cl)
	b.Wait(f)

	px := unsafe.Slice((*byte)(readback.Ptr), size*size*4)
	at := func(x, y int) (r, g, bl, a byte) {
		i := (y*size + x) * 4
		return px[i], px[i+1], px[i+2], px[i+3]
	}
	cr, cg, cb, _ := at(size/2, size/2)
	t.Logf("center pixel = (%d,%d,%d)", cr, cg, cb)
	if cr == 0 && cg == 0 && cb == 0 {
		t.Fatal("center pixel is black — triangle did not draw")
	}
	tr, tg, tb, _ := at(1, 1)
	if tr != 0 || tg != 0 || tb != 0 {
		t.Fatalf("top-left corner not the clear color: (%d,%d,%d)", tr, tg, tb)
	}
	t.Log("triangle rendered: center is colored, corner is clear")
}
