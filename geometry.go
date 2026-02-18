package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/cogentcore/webgpu/wgpu"
)

type Geometry struct {
	version        int
	positions      []glm.Vec3f
	positionBuffer *wgpu.Buffer

	indices       []uint32
	indicesBuffer *wgpu.Buffer

	isDirty bool
}

func (g *Geometry) IsDirty() bool {
	return g.isDirty
}

func (g *Geometry) Upload(device *wgpu.Device, queue *wgpu.Queue) error {
	//TODO: maybe reuse buffer
	if g.positionBuffer != nil {
		g.positionBuffer.Destroy()
	}

	buf, err := device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Position Buffer",
		Contents: wgpu.ToBytes(g.positions),
		Usage:    wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
	})

	if err != nil {
		return err
	}

	g.positionBuffer = buf

	if g.indicesBuffer != nil {
		g.indicesBuffer.Destroy()
	}

	buf, err = device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Index Buffer",
		Contents: wgpu.ToBytes(g.indices),
		Usage:    wgpu.BufferUsageIndex | wgpu.BufferUsageCopyDst,
	})

	if err != nil {
		return err
	}

	g.indicesBuffer = buf

	g.isDirty = false
	return nil
}

func (g *Geometry) VertexLayout() []wgpu.VertexBufferLayout {
	return []wgpu.VertexBufferLayout{
		// Slot 0: Positions (glm.Vec3f)
		{
			ArrayStride: uint64(unsafe.Sizeof(glm.Vec3f{})), // 12 bytes (3 floats)
			StepMode:    wgpu.VertexStepModeVertex,
			Attributes: []wgpu.VertexAttribute{
				{
					Format:         wgpu.VertexFormatFloat32x3, // vec3<f32>
					Offset:         0,
					ShaderLocation: 0, // @location(0) in shader
				},
			},
		},
		// // Slot 1: TexCoords (glm.Vec2f)
		// {
		// 	ArrayStride: uint64(unsafe.Sizeof(glm.Vec2f{})), // 8 bytes (2 floats)
		// 	StepMode:    wgpu.VertexStepModeVertex,
		// 	Attributes: []wgpu.VertexAttribute{
		// 		{
		// 			Format:         wgpu.VertexFormatFloat32x2, // vec2<f32>
		// 			Offset:         0,
		// 			ShaderLocation: 1, // @location(1) in shader
		// 		},
		// 	},
		// },
		// // Slot 2: Normals (glm.Vec3f)
		// {
		// 	ArrayStride: uint64(unsafe.Sizeof(glm.Vec3f{})),
		// 	StepMode:    wgpu.VertexStepModeVertex,
		// 	Attributes: []wgpu.VertexAttribute{
		// 		{
		// 			Format:         wgpu.VertexFormatFloat32x3, // vec3<f32>
		// 			Offset:         0,
		// 			ShaderLocation: 2, // @location(2) in shader
		// 		},
		// 	},
		// },
	}
}
