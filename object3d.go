package pix

import "github.com/bluescreen10/pix/glm"

var _ Node = &Object3D{}

type Object3D struct {
	pos           glm.Vec3f
	scale         glm.Vec3f
	rot           glm.Quatf
	localModel    glm.Mat4f
	worldModel    glm.Mat4f
	invWorldModel glm.Mat4f
	parent        Node
	children      []Node
	dirty         bool
	castShadow    bool
	receiveShadow bool
}

func (n *Object3D) SetPosition(pos glm.Vec3f) {
	n.pos = pos
	n.dirty = true
}

func (n *Object3D) SetPositionXYZ(x, y, z float32) {
	n.pos[0] = x
	n.pos[1] = y
	n.pos[2] = z
	n.dirty = true
}

func (n *Object3D) SetPositionX(x float32) {
	n.pos[0] = x
	n.dirty = true
}

func (n *Object3D) SetPositionY(y float32) {
	n.pos[1] = y
	n.dirty = true
}

func (n *Object3D) SetPositionZ(z float32) {
	n.pos[2] = z
	n.dirty = true
}

func (n *Object3D) Move(delta glm.Vec3f) {
	n.pos = n.pos.Add(delta)
	n.dirty = true
}

func (n *Object3D) MoveXYZ(x, y, z float32) {
	n.pos[0] += x
	n.pos[1] += y
	n.pos[2] += z
	n.dirty = true
}

func (n *Object3D) MoveX(deltaX float32) {
	n.pos[0] += deltaX
	n.dirty = true
}

func (n *Object3D) MoveY(deltaY float32) {
	n.pos[1] += deltaY
	n.dirty = true
}

func (n *Object3D) MoveZ(deltaZ float32) {
	n.pos[2] += deltaZ
	n.dirty = true
}

func (n *Object3D) SetRotation(rot glm.Vec3f) {
	n.rot = glm.QuatFromEuler(rot[0], rot[1], rot[2])
	n.dirty = true
}

func (n *Object3D) SetRotationXYZ(x, y, z float32) {
	n.rot = glm.QuatFromEuler(x, y, z)
	n.dirty = true
}

func (n *Object3D) Rotate(delta glm.Vec3f) {
	n.rot = n.rot.Mul(glm.QuatFromEuler(delta[0], delta[1], delta[2]))
	n.dirty = true
}

func (n *Object3D) RotateXYZ(x, y, z float32) {
	n.rot = n.rot.Mul(glm.QuatFromEuler(x, y, z))
	n.dirty = true
}

func (n *Object3D) RoateX(deltaX float32) {
	n.rot = n.rot.Mul(glm.QuatFromEuler(deltaX, 0, 0))
	n.dirty = true
}

func (n *Object3D) RoateY(deltaY float32) {
	n.rot = n.rot.Mul(glm.QuatFromEuler(0, deltaY, 0))
	n.dirty = true
}
func (n *Object3D) RoateZ(deltaZ float32) {
	n.rot = n.rot.Mul(glm.QuatFromEuler(0, 0, deltaZ))
	n.dirty = true
}

func (n *Object3D) Scale() glm.Vec3f {
	return n.scale
}

func (n *Object3D) SetScale(scale glm.Vec3f) {
	n.scale = scale
	n.dirty = true
}

func (n *Object3D) SetScaleXYZ(x, y, z float32) {
	n.scale[0] = x
	n.scale[1] = y
	n.scale[2] = z
	n.dirty = true
}

func (n *Object3D) SetScaleX(x float32) {
	n.scale[0] = x
	n.dirty = true
}

func (n *Object3D) SetScaleY(y float32) {
	n.scale[1] = y
	n.dirty = true
}

func (n *Object3D) SetScaleZ(z float32) {
	n.scale[2] = z
	n.dirty = true
}

func (n *Object3D) Grow(delta glm.Vec3f) {
	n.scale = n.scale.Add(delta)
	n.dirty = true
}

func (n *Object3D) GrowXYZ(x, y, z float32) {
	n.scale[0] += x
	n.scale[1] += y
	n.scale[2] += z
	n.dirty = true
}

func (n *Object3D) GrowX(deltaX float32) {
	n.scale[0] += deltaX
	n.dirty = true
}

func (n *Object3D) GrowY(deltaY float32) {
	n.scale[1] += deltaY
	n.dirty = true
}

func (n *Object3D) GrowZ(deltaZ float32) {
	n.scale[2] += deltaZ
	n.dirty = true
}

func (n *Object3D) SetParent(parent Node) {
	n.parent = parent
	n.dirty = true
}

func (n *Object3D) Add(child Node) {
	n.children = append(n.children, child)
	child.SetParent(n)
}

func (n *Object3D) Del(child Node) {
	var j int
	for _, c := range n.children {
		if c != child {
			n.children[j] = c
			j++
		}
	}
	n.children = n.children[:j]
}

func (n *Object3D) Model() glm.Mat4f {
	return n.worldModel
}

func (n *Object3D) InvModel() glm.Mat4f {
	return n.worldModel
}

func (n *Object3D) UpdateMatrix(force bool) bool {
	if !n.dirty && !force {
		return false
	}

	n.localModel = glm.Transform(n.scale, n.rot, n.pos)

	if n.parent != nil {
		n.worldModel = n.parent.Model().Mul4x4(n.localModel)
	} else {
		n.worldModel = n.localModel
	}

	n.dirty = false
	return true
}

func (n *Object3D) Children() []Node {
	return n.children
}

func (n *Object3D) CastShadow() bool {
	return n.castShadow
}

func (n *Object3D) SetCastShadow(castShadow bool) {
	n.castShadow = castShadow
}

func (n *Object3D) RecieveShadow() bool {
	return n.receiveShadow
}

func (n *Object3D) SetReceiveShadow(recieveShadow bool) {
	n.receiveShadow = recieveShadow
}
