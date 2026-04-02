package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/oliverbestmann/webgpu/wgpu"
)

type LightsUniform struct {
	DirectionalLights     [MaxDirectionalLights]DirectionalLightUniform
	DirectionalLightCount uint32

	_ [3]uint32 // 16-bit alignment
}

func (u *LightsUniform) Bytes() []byte {
	return toBytes(u)
}

type DirectionalLightUniform struct {
	color     glm.Color4f
	direction glm.Vec4f
}

type CameraUniform struct {
	viewProj glm.Mat4f
	position glm.Vec4f
}

func (u *CameraUniform) Bytes() []byte {
	return toBytes(u)
}

type InstanceUniform struct {
	Model    glm.Mat4f
	InvModel glm.Mat4f
}

type InstancesUniform []InstanceUniform

func (u InstancesUniform) Bytes() []byte {
	return wgpu.ToBytes(u)
}

func toBytes[T any](v *T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(v)), unsafe.Sizeof(*v))
}
