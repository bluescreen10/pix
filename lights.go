package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
)

type DirectionalLight struct {
	node
	intensity float32
	color     glm.Color3f
	target    glm.Vec3f
}

func (l *DirectionalLight) Color() glm.Color3f {
	return l.color
}

func (l *DirectionalLight) SetColor(color glm.Color3f) {
	l.color = color
}

func (l *DirectionalLight) Intensity() float32 {
	return l.intensity
}

func (l *DirectionalLight) SetIntenstity(intensity float32) {
	l.intensity = intensity
}

func (l *DirectionalLight) Target() glm.Vec3f {
	return l.target
}

func (l *DirectionalLight) SetTarget(target glm.Vec3f) {
	l.target = target
}

func NewDirectionalLight(color glm.Color3f, intensity float32) *DirectionalLight {
	return &DirectionalLight{
		color:     color,
		intensity: intensity,
	}
}

// TODO: find a better way to handle uniforms
type lightsUniform struct {
	directionalLights     [MaxDirectionalLights]directionalLightUniform
	directionalLightCount uint32

	_ [3]uint32 // 16-bit alignment
}

type directionalLightUniform struct {
	color     glm.Color4f
	direction glm.Vec4f
}

func toBytes[T any](v *T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(v)), unsafe.Sizeof(*v))
}

type cameraUniform struct {
	viewProj glm.Mat4f
	position glm.Vec4f
}

type objectUniform struct {
	model    glm.Mat4f
	invModel glm.Mat4f
}
