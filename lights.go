package pix

import "github.com/bluescreen10/pix/glm"

// DirectionalLight is a typed node handle for directional light nodes.
type DirectionalLight struct{ Node }

// DirectionalLightData is the per-light payload stored in Scene.dirLights.
type directionalLightData struct {
	color     glm.Color3f
	intensity float32
	target    glm.Vec3f
	shadow    *DirectionalShadow
	ownerNode uint32
}

func (l DirectionalLight) data() *directionalLightData {
	return &l.scene.dirLights[l.scene.payload[l.slot()]]
}

func (l DirectionalLight) Color() glm.Color3f {
	return l.data().color
}

func (l DirectionalLight) SetColor(c glm.Color3f) {
	l.data().color = c
}

func (l DirectionalLight) Intensity() float32 {
	return l.data().intensity
}

func (l DirectionalLight) SetIntensity(v float32) {
	l.data().intensity = v
}

func (l DirectionalLight) Target() glm.Vec3f {
	return l.data().target
}
func (l DirectionalLight) SetTarget(t glm.Vec3f) {
	l.data().target = t
}

func (l DirectionalLight) Shadow() *DirectionalShadow {
	return l.data().shadow
}

// SetCastShadow creates or destroys the shadow map for this light.
// For directional lights, "cast shadow" means the light has a shadow map;
// it does not use the generic node flagCastShadow.
func (l DirectionalLight) SetCastShadow(castShadows bool) {
	ld := l.data()
	if castShadows {
		ld.shadow = NewDirectionalShadow(5, 0.1, 100)
	} else {
		ld.shadow = nil
	}
}

func (s *Scene) NewDirectionalLight(color glm.Color3f, intensity float32) DirectionalLight {
	id := s.allocNode(KindDirectionalLight)
	payloadIdx := uint32(len(s.dirLights))
	s.dirLights = append(s.dirLights, directionalLightData{
		color:     color,
		intensity: intensity,
		ownerNode: id.index,
	})
	s.payload[id.index] = payloadIdx
	return DirectionalLight{Node{scene: s, id: id}}
}

// AmbientLight is a typed node handle for ambient light nodes.
type AmbientLight struct{ Node }

// AmbientLightData is the per-light payload stored in Scene.ambientLights.
type ambientLightData struct {
	color     glm.Color3f
	intensity float32
	ownerNode uint32
}

func (l AmbientLight) data() *ambientLightData {
	return &l.scene.ambientLights[l.scene.payload[l.slot()]]
}

func (l AmbientLight) Color() glm.Color3f {
	return l.data().color
}

func (l AmbientLight) SetColor(c glm.Color3f) {
	l.data().color = c
}

func (l AmbientLight) Intensity() float32 {
	return l.data().intensity
}

func (l AmbientLight) SetIntensity(v float32) {
	l.data().intensity = v
}

func (s *Scene) NewAmbientLight(intensity float32) AmbientLight {
	id := s.allocNode(KindAmbientLight)
	payloadIdx := uint32(len(s.ambientLights))
	s.ambientLights = append(s.ambientLights, ambientLightData{
		color:     glm.Color3f{1, 1, 1},
		intensity: intensity,
		ownerNode: id.index,
	})
	s.payload[id.index] = payloadIdx
	return AmbientLight{Node{scene: s, id: id}}
}
