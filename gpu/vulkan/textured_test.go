package vulkan

import (
	"testing"
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/shaders"
)

// texVertex matches Vtx in textured.vert (vec2 pos, vec2 uv).
type texVertex struct{ px, py, u, v float32 }

// texRoot matches Root in textured.{vert,frag}: verts addr + tex/samp heap indices.
type texRoot struct {
	verts uint64
	tex   uint32
	samp  uint32
}

// TestBindlessTexture uploads a 2x2 checkerboard into a heap texture and samples it
// across a fullscreen quad, then checks the four quadrants read the four texels —
// proving in-shader sampling through the bindless descriptor heap.
func TestBindlessTexture(t *testing.T) {
	b := New()
	err := b.Init()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()

	// 2x2 texels: TL red, TR green, BL blue, BstartR white (row-major, top row first).
	texels := []byte{
		255, 0, 0, 255, 0, 255, 0, 255,
		0, 0, 255, 255, 255, 255, 255, 255,
	}
	tex := b.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: 2, Height: 2,
		Format: gpu.FormatRGBA8Unorm, Usage: gpu.TextureSampled | gpu.TextureTransfer, Label: "checker"})
	staging := b.Alloc(uint64(len(texels)), gpu.MemoryHost, "staging")
	copy(unsafe.Slice((*byte)(staging.Ptr), len(texels)), texels)

	samp := b.CreateSampler(gpu.SamplerDescriptor{AddressU: gpu.AddressClamp, AddressV: gpu.AddressClamp})

	const size = 128
	color := b.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: size, Height: size,
		Format: gpu.FormatRGBA8Unorm, Usage: gpu.TextureRenderTarget | gpu.TextureTransfer})

	// Fullscreen quad (two triangles). UV origin top-left to match texel row order;
	// Vulkan clip y is down, so pos.y and uv.y run the same direction.
	verts := []texVertex{
		{-1, -1, 0, 0}, {1, -1, 1, 0}, {1, 1, 1, 1},
		{-1, -1, 0, 0}, {1, 1, 1, 1}, {-1, 1, 0, 1},
	}
	vbuf := b.Alloc(uint64(len(verts))*16, gpu.MemoryHost, "verts")
	copy(unsafe.Slice((*texVertex)(vbuf.Ptr), len(verts)), verts)

	root := b.Alloc(64, gpu.MemoryHost, "root")
	*(*texRoot)(root.Ptr) = texRoot{verts: vbuf.Addr, tex: tex.Index, samp: samp.Index}

	pipe := b.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader: shaders.TexturedVert, FragmentShader: shaders.TexturedFrag,
		Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{gpu.FormatRGBA8Unorm},
		CullMode: gpu.CullNone,
	})

	readback := b.Alloc(size*size*4, gpu.MemoryHost, "readback")
	cl := b.Begin()
	cl.CopyBufferToTexture(tex, 0, 0, staging, 0)
	cl.PrepareSampled(tex, gpu.StageFragment)
	cl.BeginRendering(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: color, Load: gpu.LoadClear, Clear: [4]float32{0, 0, 0, 1}}},
	})
	cl.SetPipeline(pipe)
	cl.Root(root.Addr)
	cl.Viewport(0, 0, size, size, 0, 1)
	cl.Scissor(0, 0, size, size)
	cl.Draw(6, 1, 0, 0)
	cl.EndRendering()
	cl.CopyTextureToBuffer(readback, color, 0, 0)
	f := b.Submit(cl)
	b.Wait(f)

	px := unsafe.Slice((*byte)(readback.Ptr), size*size*4)
	at := func(x, y int) (byte, byte, byte) {
		i := (y*size + x) * 4
		return px[i], px[i+1], px[i+2]
	}
	// Sample near each quadrant center; nearest filtering => exact texel colors.
	type probe struct {
		x, y     int
		r, g, bl byte
		name     string
	}
	q := size / 4
	for _, p := range []probe{
		{q, q, 255, 0, 0, "top-left red"},
		{3 * q, q, 0, 255, 0, "top-right green"},
		{q, 3 * q, 0, 0, 255, "bottom-left blue"},
		{3 * q, 3 * q, 255, 255, 255, "bottom-right white"},
	} {
		r, g, bl := at(p.x, p.y)
		if r != p.r || g != p.g || bl != p.bl {
			t.Fatalf("%s quadrant: got (%d,%d,%d) want (%d,%d,%d)", p.name, r, g, bl, p.r, p.g, p.bl)
		}
	}
	t.Logf("all four texels sampled correctly through the bindless heap")
}
