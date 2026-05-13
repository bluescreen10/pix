package pix

import (
	"github.com/bluescreen10/pix/glm"
)

type DirectionalLight struct {
	Object3D  //TODO: Switch Object3D for LookAt3D equal but only contains Position / Target
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

func (l *DirectionalLight) SetPosition(pos glm.Vec3f) {
	l.Object3D.SetPosition(pos)
	if l.shadow != nil {
		// adjust also the position of the camera
		l.shadow.camera.SetPosition(pos)
	}
}

func (l *DirectionalLight) SetTarget(target glm.Vec3f) {
	l.target = target
	if l.shadow != nil {
		// adjust the camera target
		l.shadow.camera.SetTarget(target)
	}
}

func (l *DirectionalLight) SetCastShadow(castShadows bool) {
	l.Object3D.SetCastShadow(castShadows)
	if castShadows {
		l.shadow = NewDirectionalShadow(200, 0.1, 100)
	} else {
		l.shadow = nil
	}
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
	Object3D  //TODO: Switch for pos glm.Vec3f and implement Node interface
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
