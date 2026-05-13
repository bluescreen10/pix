package pix

import "github.com/bluescreen10/pix/glm"

// SceneNode is satisfied by Node and every typed handle that embeds it
// (Mesh, Group, DirectionalLight, …). The promoted ID method is the only
// requirement, so no extra boilerplate is needed on each typed type.
type SceneNode interface {
	ID() NodeID
	Scene() *Scene
}

// Node is a value-type handle into a Scene's parallel arrays.
// Methods are thin wrappers that validate through the generation counter.
type Node struct {
	scene *Scene
	id    NodeID
}

func (n Node) slot() uint32 {
	return n.id.index
}

func (n Node) ID() NodeID {
	return n.id
}

func (n Node) Scene() *Scene {
	return n.scene
}

// IsValid reports whether this handle refers to a live node.
func (n Node) IsValid() bool {
	return n.scene != nil &&
		n.id.isValid() &&
		n.id.index < uint32(len(n.scene.generation)) &&
		n.scene.generation[n.id.index] == n.id.gen &&
		n.scene.flags[n.id.index]&flagAlive != 0
}

// Hierarchy

// Add parents child under this node.
func (n Node) Add(child SceneNode) {
	n.scene.reparent(child.ID(), n.id)
}

// Remove detaches child from this node without destroying it.
func (n Node) Remove(child SceneNode) {
	n.scene.detachFromParent(child.ID())
}

// Parent returns this node's parent, or a zero Node if it has none.
func (n Node) Parent() Node {
	p := n.scene.parents[n.slot()]
	if !p.isValid() {
		return Node{}
	}
	return Node{scene: n.scene, id: p}
}

// Children returns all immediate children (allocates a slice).
func (n Node) Children() []Node {
	var out []Node
	n.ForEachChild(func(c Node) bool {
		out = append(out, c)
		return true
	})
	return out
}

// ForEachChild calls fn for each immediate child. Stops early if fn returns false.
func (n Node) ForEachChild(fn func(Node) bool) {
	child := n.scene.firstChildren[n.slot()]
	for child.isValid() {
		if !fn(Node{scene: n.scene, id: child}) {
			return
		}
		child = n.scene.nextSiblings[child.index]
	}
}

// Transforms

// WorldTransform returns the cached world-space matrix.
func (n Node) WorldTransform() glm.Mat4f {
	return n.scene.world[n.slot()]
}

// Transform returns the local transform matrix.
func (n Node) Transform() glm.Mat4f {
	return n.scene.local[n.slot()]
}

// Model is an alias for WorldTransform kept for renderer compatibility.
func (n Node) Model() glm.Mat4f {
	return n.scene.world[n.slot()]
}

// Flags

func (n Node) Visible() bool {
	return n.scene.flags[n.slot()]&flagVisible != 0
}

func (n Node) SetVisible(b bool) {
	if b {
		n.scene.flags[n.slot()] |= flagVisible
	} else {
		n.scene.flags[n.slot()] &^= flagVisible
	}
}

func (n Node) CastShadow() bool {
	return n.scene.flags[n.slot()]&flagCastShadow != 0
}

func (n Node) SetCastShadow(b bool) {
	if b {
		n.scene.flags[n.slot()] |= flagCastShadow
	} else {
		n.scene.flags[n.slot()] &^= flagCastShadow
	}
}

func (n Node) ReceiveShadow() bool {
	return n.scene.flags[n.slot()]&flagReceiveShadow != 0
}

func (n Node) SetReceiveShadow(b bool) {
	if b {
		n.scene.flags[n.slot()] |= flagReceiveShadow
	} else {
		n.scene.flags[n.slot()] &^= flagReceiveShadow
	}
}

// Destroy removes this node and its entire subtree.
func (n Node) Destroy() {
	n.scene.destroySubtree(n.id)
}

// Position

func (n Node) Position() glm.Vec3f {
	return n.scene.positions[n.slot()]
}

func (n Node) SetPosition(pos glm.Vec3f) {
	n.scene.positions[n.slot()] = pos
	n.scene.flags[n.slot()] |= flagDirty
}

func (n Node) SetPositionXYZ(x, y, z float32) {
	n.SetPosition(glm.Vec3f{x, y, z})
}

func (n Node) SetPositionX(x float32) {
	p := n.scene.positions[n.slot()]
	n.SetPosition(glm.Vec3f{x, p[1], p[2]})
}

func (n Node) SetPositionY(y float32) {
	p := n.scene.positions[n.slot()]
	n.SetPosition(glm.Vec3f{p[0], y, p[2]})
}

func (n Node) SetPositionZ(z float32) {
	p := n.scene.positions[n.slot()]
	n.SetPosition(glm.Vec3f{p[0], p[1], z})
}

func (n Node) SetPositionXY(x, y float32) {
	p := n.scene.positions[n.slot()]
	n.SetPosition(glm.Vec3f{x, y, p[2]})
}

func (n Node) SetPositionXZ(x, z float32) {
	p := n.scene.positions[n.slot()]
	n.SetPosition(glm.Vec3f{x, p[1], z})
}

func (n Node) SetPositionYZ(y, z float32) {
	p := n.scene.positions[n.slot()]
	n.SetPosition(glm.Vec3f{p[0], y, z})
}

func (n Node) Move(delta glm.Vec3f) {
	n.SetPosition(n.scene.positions[n.slot()].Add(delta))
}

func (n Node) MoveXYZ(x, y, z float32) {
	n.Move(glm.Vec3f{x, y, z})
}

func (n Node) MoveX(dx float32) {
	n.Move(glm.Vec3f{dx, 0, 0})
}

func (n Node) MoveY(dy float32) {
	n.Move(glm.Vec3f{0, dy, 0})
}

func (n Node) MoveZ(dz float32) {
	n.Move(glm.Vec3f{0, 0, dz})
}

func (n Node) MoveXY(x, y float32) {
	n.Move(glm.Vec3f{x, y, 0})
}

func (n Node) MoveXZ(x, z float32) {
	n.Move(glm.Vec3f{x, 0, z})
}

func (n Node) MoveYZ(y, z float32) {
	n.Move(glm.Vec3f{0, y, z})
}

// Rotation

func (n Node) RotationQuat() glm.Quatf {
	return n.scene.rotations[n.slot()]
}

func (n Node) SetRotationQuat(rot glm.Quatf) {
	n.scene.rotations[n.slot()] = rot
	n.scene.flags[n.slot()] |= flagDirty
}

func (n Node) SetRotation(rot glm.Vec3f) {
	n.SetRotationQuat(glm.QuatFromEuler(rot[0], rot[1], rot[2]))
}

func (n Node) SetRotationXYZ(x, y, z float32) {
	n.SetRotation(glm.Vec3f{x, y, z})
}

func (n Node) SetRotationXY(x, y float32) {
	n.SetRotation(glm.Vec3f{x, y, 0})
}

func (n Node) SetRotationXZ(x, z float32) {
	n.SetRotation(glm.Vec3f{x, 0, z})
}

func (n Node) SetRotationYZ(y, z float32) {
	n.SetRotation(glm.Vec3f{0, y, z})
}

func (n Node) RotateQuat(delta glm.Quatf) {
	n.SetRotationQuat(n.scene.rotations[n.slot()].Mul(delta))
}

func (n Node) Rotate(delta glm.Vec3f) {
	n.RotateQuat(glm.QuatFromEuler(delta[0], delta[1], delta[2]))
}

func (n Node) RotateXYZ(x, y, z float32) {
	n.RotateQuat(glm.QuatFromEuler(x, y, z))
}

func (n Node) RotateX(dx float32) {
	n.RotateQuat(glm.QuatFromEuler(dx, 0, 0))
}

func (n Node) RotateY(dy float32) {
	n.RotateQuat(glm.QuatFromEuler(0, dy, 0))
}

func (n Node) RotateZ(dz float32) {
	n.RotateQuat(glm.QuatFromEuler(0, 0, dz))
}

func (n Node) RotateXY(x, y float32) {
	n.RotateQuat(glm.QuatFromEuler(x, y, 0))
}

func (n Node) RotateXZ(x, z float32) {
	n.RotateQuat(glm.QuatFromEuler(x, 0, z))
}

func (n Node) RotateYZ(y, z float32) {
	n.RotateQuat(glm.QuatFromEuler(0, y, z))
}

// Scale

func (n Node) Scale() glm.Vec3f {
	return n.scene.scales[n.slot()]
}

func (n Node) SetScale(scale glm.Vec3f) {
	n.scene.scales[n.slot()] = scale
	n.scene.flags[n.slot()] |= flagDirty
}

func (n Node) SetScaleXYZ(x, y, z float32) {
	n.SetScale(glm.Vec3f{x, y, z})
}

func (n Node) SetScaleX(x float32) {
	s := n.scene.scales[n.slot()]
	n.SetScale(glm.Vec3f{x, s[1], s[2]})
}

func (n Node) SetScaleY(y float32) {
	s := n.scene.scales[n.slot()]
	n.SetScale(glm.Vec3f{s[0], y, s[2]})
}

func (n Node) SetScaleZ(z float32) {
	s := n.scene.scales[n.slot()]
	n.SetScale(glm.Vec3f{s[0], s[1], z})
}

func (n Node) SetScaleXY(x, y float32) {
	s := n.scene.scales[n.slot()]
	n.SetScale(glm.Vec3f{x, y, s[2]})
}

func (n Node) SetScaleXZ(x, z float32) {
	s := n.scene.scales[n.slot()]
	n.SetScale(glm.Vec3f{x, s[1], z})
}

func (n Node) SetScaleYZ(y, z float32) {
	s := n.scene.scales[n.slot()]
	n.SetScale(glm.Vec3f{s[0], y, z})
}

func (n Node) Grow(delta glm.Vec3f) {
	n.SetScale(n.scene.scales[n.slot()].Add(delta))
}

func (n Node) GrowXYZ(x, y, z float32) {
	n.Grow(glm.Vec3f{x, y, z})
}

func (n Node) GrowX(dx float32) {
	n.Grow(glm.Vec3f{dx, 0, 0})
}

func (n Node) GrowY(dy float32) {
	n.Grow(glm.Vec3f{0, dy, 0})
}

func (n Node) GrowZ(dz float32) {
	n.Grow(glm.Vec3f{0, 0, dz})
}

func (n Node) GrowXY(x, y float32) {
	n.Grow(glm.Vec3f{x, y, 0})
}

func (n Node) GrowXZ(x, z float32) {
	n.Grow(glm.Vec3f{x, 0, z})
}

func (n Node) GrowYZ(y, z float32) {
	n.Grow(glm.Vec3f{0, y, z})
}

// Group is a typed node handle with no payload — used purely for hierarchy.
type Group struct{ Node }
