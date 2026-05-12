package pix

import "github.com/bluescreen10/pix/glm"

var _ Node = &Object3D{}

// Object3D is the base type for any object with a position, rotation, and scale in 3D space.
// It manages local and world-space transforms and participates in the scene graph hierarchy.
type Object3D struct {
	pos           glm.Vec3f // position relative to the parent node
	scale         glm.Vec3f // scale along each axis
	rot           glm.Quatf // orientation as a unit quaternion
	localModel    glm.Mat4f // transform matrix relative to the parent
	worldModel    glm.Mat4f // transform matrix in world space
	invWorldModel glm.Mat4f // inverse of the world-space transform matrix
	parent        Node      // parent node in the scene graph, or nil for root objects
	children      []Node    // child nodes attached to this object
	dirty         bool      // true when transform matrices need to be recalculated
	castShadow    bool      // whether this object casts shadows onto other objects
	receiveShadow bool      // whether this object displays shadows cast by other objects
}

// SetPosition sets the object's position. Override this method to intercept position changes.
func (o *Object3D) SetPosition(pos glm.Vec3f) {
	o.pos = pos
	o.dirty = true
}

// SetPositionXYZ sets the object's position using individual x, y, z components.
func (o *Object3D) SetPositionXYZ(x, y, z float32) {
	o.SetPosition(glm.Vec3f{x, y, z})
}

// SetPositionX sets the X component of the position, leaving Y and Z unchanged.
func (o *Object3D) SetPositionX(x float32) {
	o.SetPosition(glm.Vec3f{x, o.pos[1], o.pos[2]})
}

// SetPositionY sets the Y component of the position, leaving X and Z unchanged.
func (o *Object3D) SetPositionY(y float32) {
	o.SetPosition(glm.Vec3f{o.pos[0], y, o.pos[2]})
}

// SetPositionZ sets the Z component of the position, leaving X and Y unchanged.
func (o *Object3D) SetPositionZ(z float32) {
	o.SetPosition(glm.Vec3f{o.pos[0], o.pos[1], z})
}

// SetPositionXY sets the X and Y components of the position, leaving Z unchanged.
func (o *Object3D) SetPositionXY(x, y float32) {
	o.SetPosition(glm.Vec3f{x, y, o.pos[2]})
}

// SetPositionXZ sets the X and Z components of the position, leaving Y unchanged.
func (o *Object3D) SetPositionXZ(x, z float32) {
	o.SetPosition(glm.Vec3f{x, o.pos[1], z})
}

// SetPositionYZ sets the Y and Z components of the position, leaving X unchanged.
func (o *Object3D) SetPositionYZ(y, z float32) {
	o.SetPosition(glm.Vec3f{o.pos[0], y, z})
}

// Move translates the object by the given delta vector.
func (o *Object3D) Move(delta glm.Vec3f) {
	o.SetPosition(o.pos.Add(delta))
}

// MoveXYZ translates the object by the given x, y, z delta values.
func (o *Object3D) MoveXYZ(x, y, z float32) {
	o.Move(glm.Vec3f{x, y, z})
}

// MoveX translates the object along the X axis by the given amount.
func (o *Object3D) MoveX(deltaX float32) {
	o.Move(glm.Vec3f{deltaX, 0, 0})
}

// MoveY translates the object along the Y axis by the given amount.
func (o *Object3D) MoveY(deltaY float32) {
	o.Move(glm.Vec3f{0, deltaY, 0})
}

// MoveZ translates the object along the Z axis by the given amount.
func (o *Object3D) MoveZ(deltaZ float32) {
	o.Move(glm.Vec3f{0, 0, deltaZ})
}

// MoveXY translates the object along the X and Y axes by the given amounts.
func (o *Object3D) MoveXY(x, y float32) {
	o.Move(glm.Vec3f{x, y, 0})
}

// MoveXZ translates the object along the X and Z axes by the given amounts.
func (o *Object3D) MoveXZ(x, z float32) {
	o.Move(glm.Vec3f{x, 0, z})
}

// MoveYZ translates the object along the Y and Z axes by the given amounts.
func (o *Object3D) MoveYZ(y, z float32) {
	o.Move(glm.Vec3f{0, y, z})
}

// RotationQuat returns the current rotation as a quaternion.
func (o *Object3D) RotationQuat() glm.Quatf {
	return o.rot
}

// SetRotationQuat sets the rotation from a quaternion. Override this method to intercept rotation changes.
func (o *Object3D) SetRotationQuat(rot glm.Quatf) {
	o.rot = rot
	o.dirty = true
}

// SetRotation sets the rotation from Euler angles (roll, pitch, yaw) packed as a Vec3f.
func (o *Object3D) SetRotation(rot glm.Vec3f) {
	o.SetRotationQuat(glm.QuatFromEuler(rot[0], rot[1], rot[2]))
}

// SetRotationXYZ sets the rotation from individual roll, pitch, and yaw angles.
func (o *Object3D) SetRotationXYZ(x, y, z float32) {
	o.SetRotation(glm.Vec3f{x, y, z})
}

// SetRotationXY sets roll and pitch, leaving yaw at zero.
func (o *Object3D) SetRotationXY(x, y float32) {
	o.SetRotation(glm.Vec3f{x, y, 0})
}

// SetRotationXZ sets roll and yaw, leaving pitch at zero.
func (o *Object3D) SetRotationXZ(x, z float32) {
	o.SetRotation(glm.Vec3f{x, 0, z})
}

// SetRotationYZ sets pitch and yaw, leaving roll at zero.
func (o *Object3D) SetRotationYZ(y, z float32) {
	o.SetRotation(glm.Vec3f{0, y, z})
}

// RotateQuat applies an additional rotation by the given quaternion delta.
func (o *Object3D) RotateQuat(delta glm.Quatf) {
	o.SetRotationQuat(o.rot.Mul(delta))
}

// Rotate applies an additional rotation by Euler angle deltas (roll, pitch, yaw) packed as a Vec3f.
func (o *Object3D) Rotate(delta glm.Vec3f) {
	o.RotateQuat(glm.QuatFromEuler(delta[0], delta[1], delta[2]))
}

// RotateXYZ applies an additional rotation by individual roll, pitch, and yaw angle deltas.
func (o *Object3D) RotateXYZ(x, y, z float32) {
	o.RotateQuat(glm.QuatFromEuler(x, y, z))
}

// RotateX rotates the object around the X axis (roll) by the given angle.
func (o *Object3D) RotateX(deltaX float32) {
	o.RotateQuat(glm.QuatFromEuler(deltaX, 0, 0))
}

// RotateY rotates the object around the Y axis (pitch) by the given angle.
func (o *Object3D) RotateY(deltaY float32) {
	o.RotateQuat(glm.QuatFromEuler(0, deltaY, 0))
}

// RotateZ rotates the object around the Z axis (yaw) by the given angle.
func (o *Object3D) RotateZ(deltaZ float32) {
	o.RotateQuat(glm.QuatFromEuler(0, 0, deltaZ))
}

// RotateXY applies an additional rotation around the X and Y axes by the given angle deltas.
func (o *Object3D) RotateXY(x, y float32) {
	o.RotateQuat(glm.QuatFromEuler(x, y, 0))
}

// RotateXZ applies an additional rotation around the X and Z axes by the given angle deltas.
func (o *Object3D) RotateXZ(x, z float32) {
	o.RotateQuat(glm.QuatFromEuler(x, 0, z))
}

// RotateYZ applies an additional rotation around the Y and Z axes by the given angle deltas.
func (o *Object3D) RotateYZ(y, z float32) {
	o.RotateQuat(glm.QuatFromEuler(0, y, z))
}

// Scale returns the current scale along each axis.
func (o *Object3D) Scale() glm.Vec3f {
	return o.scale
}

// SetScale sets the scale along all three axes. Override this method to intercept scale changes.
func (o *Object3D) SetScale(scale glm.Vec3f) {
	o.scale = scale
	o.dirty = true
}

// SetScaleXYZ sets the scale using individual x, y, z values.
func (o *Object3D) SetScaleXYZ(x, y, z float32) {
	o.SetScale(glm.Vec3f{x, y, z})
}

// SetScaleX sets the X scale component, leaving Y and Z unchanged.
func (o *Object3D) SetScaleX(x float32) {
	o.SetScale(glm.Vec3f{x, o.scale[1], o.scale[2]})
}

// SetScaleY sets the Y scale component, leaving X and Z unchanged.
func (o *Object3D) SetScaleY(y float32) {
	o.SetScale(glm.Vec3f{o.scale[0], y, o.scale[2]})
}

// SetScaleZ sets the Z scale component, leaving X and Y unchanged.
func (o *Object3D) SetScaleZ(z float32) {
	o.SetScale(glm.Vec3f{o.scale[0], o.scale[1], z})
}

// SetScaleXY sets the X and Y scale components, leaving Z unchanged.
func (o *Object3D) SetScaleXY(x, y float32) {
	o.SetScale(glm.Vec3f{x, y, o.scale[2]})
}

// SetScaleXZ sets the X and Z scale components, leaving Y unchanged.
func (o *Object3D) SetScaleXZ(x, z float32) {
	o.SetScale(glm.Vec3f{x, o.scale[1], z})
}

// SetScaleYZ sets the Y and Z scale components, leaving X unchanged.
func (o *Object3D) SetScaleYZ(y, z float32) {
	o.SetScale(glm.Vec3f{o.scale[0], y, z})
}

// Grow increases the scale by the given delta vector.
func (o *Object3D) Grow(delta glm.Vec3f) {
	o.SetScale(o.scale.Add(delta))
}

// GrowXYZ increases the scale by individual x, y, z delta values.
func (o *Object3D) GrowXYZ(x, y, z float32) {
	o.Grow(glm.Vec3f{x, y, z})
}

// GrowX increases the X scale component by the given amount.
func (o *Object3D) GrowX(deltaX float32) {
	o.Grow(glm.Vec3f{deltaX, 0, 0})
}

// GrowY increases the Y scale component by the given amount.
func (o *Object3D) GrowY(deltaY float32) {
	o.Grow(glm.Vec3f{0, deltaY, 0})
}

// GrowZ increases the Z scale component by the given amount.
func (o *Object3D) GrowZ(deltaZ float32) {
	o.Grow(glm.Vec3f{0, 0, deltaZ})
}

// GrowXY increases the X and Y scale components by the given amounts.
func (o *Object3D) GrowXY(x, y float32) {
	o.Grow(glm.Vec3f{x, y, 0})
}

// GrowXZ increases the X and Z scale components by the given amounts.
func (o *Object3D) GrowXZ(x, z float32) {
	o.Grow(glm.Vec3f{x, 0, z})
}

// GrowYZ increases the Y and Z scale components by the given amounts.
func (o *Object3D) GrowYZ(y, z float32) {
	o.Grow(glm.Vec3f{0, y, z})
}

// SetParent sets this object's parent in the scene graph.
func (o *Object3D) SetParent(parent Node) {
	o.parent = parent
	o.dirty = true
}

// Add attaches a child node to this object and sets its parent accordingly.
func (o *Object3D) Add(child Node) {
	o.children = append(o.children, child)
	child.SetParent(o)
}

// Del removes a child node from this object. Has no effect if child is not present.
func (o *Object3D) Del(child Node) {
	var j int
	for _, c := range o.children {
		if c != child {
			o.children[j] = c
			j++
		}
	}
	o.children = o.children[:j]
}

// Model returns the world-space transform matrix.
func (o *Object3D) Model() glm.Mat4f {
	return o.worldModel
}

// InvModel returns the inverse of the world-space transform matrix.
func (o *Object3D) InvModel() glm.Mat4f {
	return o.invWorldModel
}

// UpdateMatrix recalculates the world-space transform matrices if the object's transform has changed.
// Passing force as true recalculates even when no changes are pending. Returns true if matrices were updated.
func (o *Object3D) UpdateMatrix(force bool) bool {
	if !o.dirty && !force {
		return false
	}

	o.localModel = glm.Transform(o.scale, o.rot, o.pos)

	if o.parent != nil {
		o.worldModel = o.parent.Model().Mul4x4(o.localModel)
	} else {
		o.worldModel = o.localModel
	}

	o.dirty = false
	return true
}

// Children returns the list of nodes attached to this object.
func (o *Object3D) Children() []Node {
	return o.children
}

// CastShadow reports whether this object casts shadows onto other objects.
func (o *Object3D) CastShadow() bool {
	return o.castShadow
}

// SetCastShadow controls whether this object casts shadows onto other objects.
func (o *Object3D) SetCastShadow(castShadow bool) {
	o.castShadow = castShadow
}

// ReceiveShadow reports whether this object displays shadows cast by other objects.
func (o *Object3D) ReceiveShadow() bool {
	return o.receiveShadow
}

// SetReceiveShadow controls whether this object displays shadows cast by other objects.
func (o *Object3D) SetReceiveShadow(recieveShadow bool) {
	o.receiveShadow = recieveShadow
}
