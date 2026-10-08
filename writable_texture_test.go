package pix_test

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/gamekit/utils"
	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
	"github.com/bluescreen10/pix/textures"
)

//go:generate go run ./cmd/shadercompile -i testdata/volume_write.comp.glsl -o spv:testdata/build/volume_write.comp.spv -o metallib:testdata/build/volume_write.comp.metalbin
//go:generate go run ./cmd/shadercompile -i testdata/volume_material.frag.glsl -o spv:testdata/build/volume_material.frag.spv -o metallib:testdata/build/volume_material.frag.metalbin
//go:generate go run ./cmd/shadercompile -i testdata/image_write.comp.glsl -o spv:testdata/build/image_write.comp.spv -o metallib:testdata/build/image_write.comp.metalbin

// volumeSize is the side of the volume the tests below write and sample.
const volumeSize = 4

// writeStep fills a writable texture at the start of every frame, with one workgroup of
// a test shader whose root is the texture's index and side: every test shader here
// covers its whole texture in a single workgroup.
type writeStep struct {
	texture  textures.Texture
	side     uint32
	shader   []byte
	backend  gpu.Backend
	pipeline gpu.Pipeline
}

func (s *writeStep) Encode(frame *pix.Frame, cmd gpu.CommandBuffer) {
	if !s.pipeline.IsValid() {
		s.backend = frame.Backend
		s.pipeline = frame.Backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: s.shader, Label: "texture-write"})
	}
	cmd.SetPipeline(s.pipeline)
	data := [4]uint32{s.texture.Index(), s.side}
	cmd.Dispatch(utils.ToBytes(&data), 1, 1, 1)
}

func (s *writeStep) Release() {
	if s.pipeline.IsValid() {
		s.backend.DestroyPipeline(s.pipeline)
		s.pipeline = gpu.Pipeline{}
	}
}

// TestComputeWrittenVolumeIsSampledByMaterial is the whole path a simulation takes to
// the screen: a frame step's compute shader writes a writable 3D texture, and a
// material samples it, by the same index, in the frame's drawing. The material shows
// the depth its record asks for, so the near depth reads red and the far one green.
func TestComputeWrittenVolumeIsSampledByMaterial(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	volume := r.TextureStore.CreateWritable(textures.WritableConfig{
		Kind: gpu.Texture3D, Width: volumeSize, Height: volumeSize, Depth: volumeSize,
		Format: gpu.FormatRGBA8Unorm, Label: "volume",
	})
	defer volume.Release()
	r.AddFrameStep(pix.FrameStageStart, &writeStep{texture: volume.Texture, side: volumeSize, shader: testShader(t, r, "volume_write.comp")})

	material := r.NewRawMaterial(materials.Shader{Fragment: testShader(t, r, "volume_material.frag")}, 16, 1)
	material.SetTexture(0, volume.Texture)
	setVolumeRecord := func(depth float32) {
		record := material.Record()
		binary.LittleEndian.PutUint32(record[0:], volume.Index())
		binary.LittleEndian.PutUint32(record[4:], r.TextureStore.DefaultSampler())
		binary.LittleEndian.PutUint32(record[8:], math.Float32bits(depth))
	}

	scene := scenes.New()
	defer scene.Destroy()
	quad := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.5, -0.5, 0}, {0.5, -0.5, 0}, {0.5, 0.5, 0}, {-0.5, 0.5, 0}}),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	})
	scene.NewMesh(quad, material)
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 2})

	for _, probe := range []struct {
		depth float32
		want  [3]byte
		name  string
	}{
		{0.25, [3]byte{255, 0, 0}, "near half, red"},
		{0.75, [3]byte{0, 255, 0}, "far half, green"},
	} {
		setVolumeRecord(probe.depth)
		r.Render(scene)
		if got := pixelAt(r, postSize/2, postSize/2); got != probe.want {
			t.Errorf("depth %v: center = %v, want %v, the %s", probe.depth, got, probe.want, probe.name)
		}
	}
}

// TestComputeWrittenImageIsSampledByBasicMaterial: a writable 2D texture works wherever a
// texture does. A frame step's compute shader fills one with orange, through the heap's
// 2D storage array, and a stock BasicMaterial shows it as its colour map: 0.5 is stored
// as 128/255, which the display target encodes as 188.
func TestComputeWrittenImageIsSampledByBasicMaterial(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	const side = 8
	image := r.TextureStore.CreateWritable(textures.WritableConfig{
		Kind: gpu.Texture2D, Width: side, Height: side, Format: gpu.FormatRGBA8Unorm, Label: "image",
	})
	defer image.Release()
	r.AddFrameStep(pix.FrameStageStart, &writeStep{texture: image.Texture, side: side, shader: testShader(t, r, "image_write.comp")})

	material := r.NewBasicMaterial()
	material.SetColorMap(image.Texture)
	material.SetColorMapSampler(r.TextureStore.DefaultSampler())
	scene := scenes.New()
	defer scene.Destroy()
	quad := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.5, -0.5, 0}, {0.5, -0.5, 0}, {0.5, 0.5, 0}, {-0.5, 0.5, 0}}),
			geometries.NewAttribute(geometries.AttributeUV, geometries.Float32x2, []glm.Vec2f{{0, 1}, {1, 1}, {1, 0}, {0, 0}}),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	})
	scene.NewMesh(quad, material)
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 2})

	r.Render(scene)

	if got, want := pixelAt(r, postSize/2, postSize/2), [3]byte{255, 188, 0}; got != want {
		t.Errorf("center = %v, want %v, the orange the compute shader wrote", got, want)
	}
}

// testShader returns one of this package's test shaders, compiled for the renderer's
// backend (see the go:generate lines above).
func testShader(t *testing.T, r *pix.Renderer, name string) []byte {
	t.Helper()
	extension := ".spv"
	if backend, ok := r.Backend().(interface{ ShaderFormat() string }); ok && backend.ShaderFormat() == "metal" {
		extension = ".metalbin"
	}
	code, err := os.ReadFile(filepath.Join("testdata", "build", name+extension))
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// TestWritableTextureMips: a writable texture made with Mips holds a storage view of each,
// each with a heap index of its own to be written through, apart from the one the whole
// texture is sampled through; a texture of one mip holds the texture itself.
func TestWritableTextureMips(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	chain := r.TextureStore.CreateWritable(textures.WritableConfig{Kind: gpu.Texture2D, Width: 16, Height: 8, Mips: 5, Format: gpu.FormatRGBA16F})
	defer chain.Release()
	if got := len(chain.Mips); got != 5 {
		t.Errorf("len(Mips) = %d, want 5", got)
	}
	seen := map[uint32]bool{chain.Index(): true}
	for mip, view := range chain.Mips {
		if seen[view.Index] {
			t.Errorf("Mips[%d].Index = %d, an index already taken by the texture or another mip", mip, view.Index)
		}
		seen[view.Index] = true
	}

	single := r.TextureStore.CreateWritable(textures.WritableConfig{Kind: gpu.Texture2D, Width: 4, Height: 4, Format: gpu.FormatRGBA16F})
	defer single.Release()
	if len(single.Mips) != 1 || single.Mips[0].Index != single.Index() {
		t.Errorf("Mips of a texture made without Mips = %v, want the texture itself, at index %d", single.Mips, single.Index())
	}
}
