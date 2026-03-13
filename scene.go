package pix

import "github.com/bluescreen10/pix/glm"

type Node interface {
	Add(child Node)
	Del(child Node)
	SetParent(parent Node)
	Children() []Node
	Model() glm.Mat4f
	UpdateMatrix(force bool) bool
}

var _ Node = &node{}

type node struct {
	pos        glm.Vec3f
	scale      glm.Vec3f
	rot        glm.Quatf
	localModel glm.Mat4f
	worldModel glm.Mat4f
	parent     Node
	children   []Node
	dirty      bool
}

func (n *node) SetPosition(x, y, z float32) {
	n.pos[0] = x
	n.pos[1] = y
	n.pos[2] = z
	n.dirty = true
}

func (n *node) Move(x, y, z float32) {
	n.pos[0] += x
	n.pos[1] += y
	n.pos[2] += z
	n.dirty = true
}

func (n *node) SetRotation(roll, pitch, yaw float32) {
	n.rot = glm.QuatFromEuler(roll, pitch, yaw)
	n.dirty = true
}

func (n *node) Rotate(roll, pitch, yaw float32) {
	n.rot = n.rot.Mul(glm.QuatFromEuler(roll, pitch, yaw))
	n.dirty = true
}

func (n *node) SetParent(parent Node) {
	n.parent = parent
	n.dirty = true
}

func (n *node) Add(child Node) {
	n.children = append(n.children, child)
	child.SetParent(n)
}

func (n *node) Del(child Node) {
	var j int
	for _, c := range n.children {
		if c != child {
			n.children[j] = c
			j++
		}
	}
	n.children = n.children[:j]
}

func (n *node) Model() glm.Mat4f {
	return n.worldModel
}

func (n *node) UpdateMatrix(force bool) bool {
	if !n.dirty && !force {
		return false
	}

	n.localModel = glm.Transform(n.scale, n.rot, n.pos)

	if n.parent != nil {
		n.worldModel = n.parent.Model().Mul4x4(n.localModel)
	} else {
		n.worldModel = n.localModel
	}

	return true
}

func (n *node) Children() []Node {
	return n.children
}

type Scene struct {
	background glm.Color4f
	node
}

func (s *Scene) SetBackground(color glm.Color4f) {
	s.background = color
}

type Group struct {
	node
}

func NewScene() *Scene {
	return &Scene{node: newNode()}
}

func newNode() node {
	return node{
		scale:      glm.Vec3f{1, 1, 1},
		rot:        glm.QuatIdentityf,
		localModel: glm.Mat4fIndentity,
		worldModel: glm.Mat4fIndentity,
	}
}
