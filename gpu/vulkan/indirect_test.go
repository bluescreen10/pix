package vulkan

import (
	"testing"
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/shaders"
)

// drawIndexedIndirect mirrors VkDrawIndexedIndirectCommand (20 bytes).
type drawIndexedIndirect struct {
	indexCount, instanceCount, firstIndex uint32
	vertexOffset                          int32
	firstInstance                         uint32
}

// TestComputeIndirect exercises the exact GPU-driven path that failed on gogpu:
// a compute pass writes the draw args (with atomicAdd on instanceCount) via BDA,
// a barrier orders compute→indirect, then DrawIndexedIndirect consumes them.
func TestComputeIndirect(t *testing.T) {
	b := New()
	err := b.Init()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()

	const size = 64
	target := b.CreateTexture(gpu.TextureDescriptor{
		Kind: gpu.Texture2D, Width: size, Height: size, Format: gpu.FormatRGBA8Unorm,
		Usage: gpu.TextureRenderTarget | gpu.TextureTransfer, Label: "target",
	})

	// Vertices + indices (host-visible, read via BDA / index buffer).
	vb := b.Alloc(256, gpu.MemoryHost, "verts")
	verts := unsafe.Slice((*vertex)(vb.Ptr), 3)
	verts[0] = vertex{-0.9, -0.9, 1, 0, 0}
	verts[1] = vertex{0.9, -0.9, 0, 1, 0}
	verts[2] = vertex{0.0, 0.9, 0, 0, 1}

	ib := b.Alloc(64, gpu.MemoryHost, "indices")
	idx := unsafe.Slice((*uint32)(ib.Ptr), 3)
	idx[0], idx[1], idx[2] = 0, 1, 2

	// Indirect args buffer, zeroed — compute fills it.
	indirect := b.Alloc(64, gpu.MemoryHost, "indirect")
	args := (*drawIndexedIndirect)(indirect.Ptr)
	*args = drawIndexedIndirect{} // instanceCount = 0

	// Compute root: pointer to the indirect args buffer.
	cRoot := b.Alloc(64, gpu.MemoryHost, "comp-root")
	*(*uint64)(cRoot.Ptr) = indirect.Addr

	// Graphics root: white tint + vertex buffer address.
	gRoot := b.Alloc(64, gpu.MemoryHost, "gfx-root")
	root := (*rootData)(gRoot.Ptr)
	root.tint = [4]float32{1, 1, 1, 1}
	root.verts = vb.Addr

	comp := b.CreateComputePipeline(gpu.ComputePipelineDescriptor{
		Shader: shaders.FillIndirect,
		Entry:  "main",
		Label:  "fill-indirect",
	})
	gfx := b.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader: shaders.TriangleVert, FragmentShader: shaders.TriangleFrag,
		Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{gpu.FormatRGBA8Unorm},
		CullMode: gpu.CullNone, Label: "triangle",
	})

	readback := b.Alloc(size*size*4, gpu.MemoryHost, "readback")

	cl := b.Begin()
	cl.SetPipeline(comp)
	cl.Root(cRoot.Addr)
	cl.Dispatch(1, 1, 1)
	// Order the compute writes before the indirect fetch + vertex/index read.
	cl.Barrier(gpu.StageCompute, gpu.StageIndirect|gpu.StageVertex, 0)

	cl.BeginRendering(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: target, Load: gpu.LoadClear, Clear: [4]float32{0, 0, 0, 1}}},
	})
	cl.SetPipeline(gfx)
	cl.Root(gRoot.Addr)
	cl.Viewport(0, 0, size, size, 0, 1)
	cl.Scissor(0, 0, size, size)
	cl.DrawIndexedIndirect(ib, indirect, 0, 1, 20)
	cl.EndRendering()
	cl.CopyTextureToBuffer(readback, target, 0, 0)
	f := b.Submit(cl)
	b.Wait(f)

	// The compute pass must have written the draw args.
	if args.indexCount != 3 || args.instanceCount != 1 {
		t.Fatalf("compute did not fill indirect args: indexCount=%d instanceCount=%d", args.indexCount, args.instanceCount)
	}
	t.Logf("indirect args from compute: indexCount=%d instanceCount=%d", args.indexCount, args.instanceCount)

	// And the indirect draw must have rendered.
	px := unsafe.Slice((*byte)(readback.Ptr), size*size*4)
	i := (size/2*size + size/2) * 4
	if px[i] == 0 && px[i+1] == 0 && px[i+2] == 0 {
		t.Fatal("center pixel black — indirect draw did not render")
	}
	t.Logf("center pixel = (%d,%d,%d) — compute→indirect draw works", px[i], px[i+1], px[i+2])
}
