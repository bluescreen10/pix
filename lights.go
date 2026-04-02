package pix

import (
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
