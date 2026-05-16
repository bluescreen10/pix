package pix

import (
	"unsafe"

	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/glm"
)

type LightsUniform struct {
	DirectionalLights     [MaxDirectionalLights]DirectionalLightUniform // 560
	DirectionalLightCount uint32                                         //   4
	_                     [3]float32                                     //  12 pad → 576
	AmbientLight          AmbientLightUniform                           //  32 → 608
	SpotLights            [MaxSpotLights]SpotLightUniform               // 640 → 1248
	SpotLightCount        uint32                                         //   4
	_                     [3]float32                                     //  12 pad → 1264
	PointLights           [MaxPointLights]PointLightUniform             // 240 → 1504
	PointLightCount       uint32                                         //   4
	_                     [3]float32                                     //  12 pad → 1520
}

type PointLightUniform struct {
	color       glm.Color4f // 16 — w holds intensity
	position    glm.Vec4f   // 16
	far         float32     //  4
	castsShadow uint32      //  4
	shadowBias  float32     //  4
	_           float32     //  4 pad → 48 total
}

type SpotLightUniform struct {
	color            glm.Color4f // 16 — w holds intensity
	position         glm.Vec4f   // 16
	direction        glm.Vec4f   // 16
	lightSpaceMatrix glm.Mat4f   // 64
	innerCosine      float32     //  4
	outerCosine      float32     //  4
	castsShadow      uint32      //  4
	shadowBias       float32     //  4 → 128 total
}

func (u *LightsUniform) Bytes() []byte {
	return toBytes(u)
}

type DirectionalLightUniform struct {
	color            glm.Color4f // 16 — w holds intensity
	direction        glm.Vec4f   // 16
	lightSpaceMatrix glm.Mat4f   // 64
	castsShadow      uint32      //  4
	shadowBias       float32     //  4
	_                [2]float32  //  8 pad → 112 total (vec4 align)
}

type AmbientLightUniform struct {
	color     glm.Color4f
	intensity float32
	_         [3]float32 // pad to 32 bytes (vec4 alignment)
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
