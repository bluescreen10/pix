package pix

import (
	"math"

	"github.com/bluescreen10/pix/glm"
)

func nodeWorldPos(scene *Scene, ownerNode uint32) glm.Vec3f {
	w := scene.world[ownerNode]
	return glm.Vec3f{w[12], w[13], w[14]}
}

// shadowVP syncs the directional light's shadow camera to the current world
// position and returns its view-projection matrix.
func (ld *directionalLightData) shadowVP(scene *Scene) (glm.Mat4f, bool) {
	if ld.shadow == nil {
		return glm.Mat4f{}, false
	}
	pos := nodeWorldPos(scene, ld.ownerNode)
	ld.shadow.camera.SetPosition(pos)
	ld.shadow.camera.SetTarget(ld.target)
	return ld.shadow.camera.ViewProjection(), true
}

func (ld *directionalLightData) toUniform(scene *Scene, lightSpaceMat glm.Mat4f) DirectionalLightUniform {
	colorRGBA := ld.color.RGBA()
	colorRGBA[3] = ld.intensity
	worldPos := nodeWorldPos(scene, ld.ownerNode)
	var castsShadow uint32
	var shadowBias float32
	if ld.shadow != nil {
		castsShadow = 1
		shadowBias = ld.shadow.bias
	}
	return DirectionalLightUniform{
		color:            colorRGBA,
		direction:        ld.target.Sub(worldPos).Normalize().Vec4(),
		lightSpaceMatrix: lightSpaceMat,
		castsShadow:      castsShadow,
		shadowBias:       shadowBias,
	}
}

// shadowVP syncs the spot light's shadow camera to the current world position
// and returns its view-projection matrix.
func (ld *spotLightData) shadowVP(scene *Scene) (glm.Mat4f, bool) {
	if ld.shadow == nil {
		return glm.Mat4f{}, false
	}
	worldPos := nodeWorldPos(scene, ld.ownerNode)
	fwd := ld.target.Sub(worldPos).Normalize()
	up := glm.Vec3f{0, 1, 0}
	if fwd[1] > 0.999 || fwd[1] < -0.999 {
		up = glm.Vec3f{0, 0, 1}
	}
	ld.shadow.camera.SetPosition(worldPos)
	ld.shadow.camera.SetFwd(fwd)
	ld.shadow.camera.SetUp(up)
	return ld.shadow.camera.ViewProjection(), true
}

func (ld *spotLightData) toUniform(scene *Scene, lightSpaceMat glm.Mat4f) SpotLightUniform {
	colorRGBA := ld.color.RGBA()
	colorRGBA[3] = ld.intensity
	worldPos := nodeWorldPos(scene, ld.ownerNode)
	spotDir := ld.target.Sub(worldPos).Normalize()
	innerCosine := float32(math.Cos(float64(ld.innerAngle) * math.Pi / 180.0))
	outerCosine := float32(math.Cos(float64(ld.outerAngle) * math.Pi / 180.0))
	var castsShadow uint32
	var shadowBias float32
	if ld.shadow != nil {
		castsShadow = 1
		shadowBias = ld.shadow.bias
	}
	return SpotLightUniform{
		color:            colorRGBA,
		position:         glm.Vec4f{worldPos[0], worldPos[1], worldPos[2], 1},
		direction:        glm.Vec4f{spotDir[0], spotDir[1], spotDir[2], 0},
		lightSpaceMatrix: lightSpaceMat,
		innerCosine:      innerCosine,
		outerCosine:      outerCosine,
		castsShadow:      castsShadow,
		shadowBias:       shadowBias,
	}
}

func (ld *pointLightData) toUniform(scene *Scene) PointLightUniform {
	colorRGBA := ld.color.RGBA()
	colorRGBA[3] = ld.intensity
	worldPos := nodeWorldPos(scene, ld.ownerNode)
	far := float32(100)
	var castsShadow uint32
	var shadowBias float32
	if ld.shadow != nil {
		castsShadow = 1
		shadowBias = ld.shadow.bias
		far = ld.shadow.far
	}
	return PointLightUniform{
		color:       colorRGBA,
		position:    glm.Vec4f{worldPos[0], worldPos[1], worldPos[2], 1},
		far:         far,
		castsShadow: castsShadow,
		shadowBias:  shadowBias,
	}
}
