package pix

import (
	"github.com/bluescreen10/pix/glm"
)

type DirectionalLight struct {
	Object3D
	intensity float32
	color     glm.Color3f
	target    glm.Vec3f
	shadow    *DirectionalShadow
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

func (l *DirectionalLight) Shadow() *DirectionalShadow     { return l.shadow }
func (l *DirectionalLight) SetShadow(s *DirectionalShadow) { l.shadow = s }

func NewDirectionalLight(color glm.Color3f, intensity float32) *DirectionalLight {
	return &DirectionalLight{
		color:     color,
		intensity: intensity,
	}
}

type AmbientLight struct {
	Object3D
	color     glm.Color3f
	intensity float32
}

func (l *AmbientLight) Color() glm.Color3f {
	return l.color
}

func (l *AmbientLight) SetColor(color glm.Color3f) {
	l.color = color
}

func (l *AmbientLight) Intensity() float32 {
	return l.intensity
}

func (l *AmbientLight) SetIntenstity(intensity float32) {
	l.intensity = intensity
}

func NewAmbientLight(intensity float32) *AmbientLight {
	return &AmbientLight{
		color:     glm.Color3f{1, 1, 1},
		intensity: intensity,
	}
}
