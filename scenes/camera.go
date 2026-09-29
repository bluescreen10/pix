package scenes

import (
	"slices"

	"github.com/bluescreen10/pix/glm"
	"github.com/chewxy/math32"
)

// Camera is a typed node handle for a viewpoint. It is an ordinary scene node: it looks
// down its local -Z axis with +Y up, so parenting it under another node carries it along
// with that node, and it renders from wherever the hierarchy puts it.
//
// Every attached, visible camera becomes a view of the frame (see FramePacket.Views).
// SetVisible(false) takes a camera out of the frame without destroying it.
type Camera struct{ Node }

// cameraData is the per-camera payload stored in Scene.cameras.
type cameraData struct {
	id           ViewID
	orthographic bool
	// Perspective: vertical field of view in degrees, and width / height.
	fov, aspect float32
	// Orthographic: the view volume's extents across the view, in view space.
	left, right, bottom, top float32
	near, far                float32
	ownerNode                uint32
}

// projection is the camera's projection matrix in Pix's canonical convention:
// right-handed, reversed Z.
func (c *cameraData) projection() glm.Mat4f {
	if c.orthographic {
		return glm.OrthoFullRevZRH(c.left, c.right, c.bottom, c.top, c.near, c.far)
	}
	return glm.PerspectiveRevZRH(glm.ToRadians(c.fov), c.aspect, c.near, c.far)
}

// NewPerspectiveCamera creates a perspective camera node looking down -Z. fov is the
// vertical field of view in degrees and aspect is width / height. Like every node, it
// renders only once it is added to the scene.
func (s *Scene) NewPerspectiveCamera(fov, aspect, near, far float32) Camera {
	return s.newCamera(cameraData{fov: fov, aspect: aspect, near: near, far: far})
}

// NewOrthographicCamera creates an orthographic camera node looking down -Z, seeing the
// box [left, right] x [bottom, top] across the view and [near, far] along it.
func (s *Scene) NewOrthographicCamera(left, right, bottom, top, near, far float32) Camera {
	return s.newCamera(cameraData{
		orthographic: true,
		left:         left,
		right:        right,
		bottom:       bottom,
		top:          top,
		near:         near,
		far:          far,
	})
}

func (s *Scene) newCamera(data cameraData) Camera {
	id := s.allocNode(kindCamera)
	data.id = s.newViewID()
	data.ownerNode = id.index
	s.payload[id.index] = uint32(len(s.cameras))
	s.cameras = append(s.cameras, data)
	return Camera{Node{scene: s, id: id}}
}

// newViewID mints the next stable view identity. Never reused: a renderer keys per-view
// history on it, and a recycled id would hand a new camera a dead one's history.
func (s *Scene) newViewID() ViewID {
	s.nextViewID++
	return s.nextViewID
}

// removeCamera deletes a camera's payload. Order is kept rather than swapped, because
// payload order is view order and a renderer draws the first view: destroying one
// camera must not promote an unrelated one to the front.
func (s *Scene) removeCamera(payloadIdx uint32) {
	s.cameras = slices.Delete(s.cameras, int(payloadIdx), int(payloadIdx)+1)
	for i := int(payloadIdx); i < len(s.cameras); i++ {
		s.payload[s.cameras[i].ownerNode] = uint32(i)
	}
}

func (c Camera) data() *cameraData {
	return &c.scene.cameras[c.scene.payload[c.slot()]]
}

// Forward is the direction the camera looks, in its parent's space.
func (c Camera) Forward() glm.Vec3f {
	return c.RotationQuat().Rotate(glm.Vec3f{0, 0, -1})
}

// SetForward turns the camera to look along direction, in its parent's space, keeping
// its up vector as close to the current one as the new direction allows. A zero
// direction names no facing and is ignored.
func (c Camera) SetForward(direction glm.Vec3f) {
	if direction.Length() == 0 {
		return
	}
	c.SetRotationQuat(lookRotation(direction, c.Up()))
}

// Up is the camera's up vector, in its parent's space.
func (c Camera) Up() glm.Vec3f {
	return c.RotationQuat().Rotate(glm.Vec3f{0, 1, 0})
}

// SetUp rolls the camera about its forward direction so its up vector is as close to up
// as that direction allows.
func (c Camera) SetUp(up glm.Vec3f) {
	c.SetRotationQuat(lookRotation(c.Forward(), up))
}

// LookAt turns the camera toward target, a point in its parent's space. Set the
// camera's position first: the direction is taken from where it is now.
func (c Camera) LookAt(target glm.Vec3f) {
	c.SetForward(target.Sub(c.Position()))
}

// ViewProjection returns the camera's world-to-clip matrix, from its world transform as
// of the last Sync.
func (c Camera) ViewProjection() glm.Mat4f {
	return c.data().projection().Mul4x4(c.WorldTransform().Inv())
}

// SetFOV sets a perspective camera's vertical field of view, in degrees. It has no
// effect on an orthographic camera.
func (c Camera) SetFOV(fov float32) {
	c.data().fov = fov
}

// SetAspect sets a perspective camera's width / height, which has to follow the target
// it renders into when that is resized. It has no effect on an orthographic camera.
func (c Camera) SetAspect(aspect float32) {
	c.data().aspect = aspect
}

// SetNear sets the distance to the near clipping plane.
func (c Camera) SetNear(near float32) {
	c.data().near = near
}

// SetFar sets the distance to the far clipping plane.
func (c Camera) SetFar(far float32) {
	c.data().far = far
}

// lookRotation is the rotation that turns -Z to forward and +Y as close to up as
// forward allows.
func lookRotation(forward, up glm.Vec3f) glm.Quatf {
	back := forward.Normalize().Scale(-1)
	right := up.Cross(back)
	if right.Length() < 1e-6 {
		// up is parallel to forward, so it says nothing about roll; any axis across
		// forward will do.
		across := glm.Vec3f{1, 0, 0}
		if math32.Abs(back[0]) > 0.9 {
			across = glm.Vec3f{0, 1, 0}
		}
		right = across.Cross(back)
	}
	right = right.Normalize()
	trueUp := back.Cross(right)

	basis := glm.Mat4f{
		right[0], right[1], right[2], 0,
		trueUp[0], trueUp[1], trueUp[2], 0,
		back[0], back[1], back[2], 0,
		0, 0, 0, 1,
	}
	_, rotation, _ := glm.DecomposeMat4f(basis)
	return rotation
}
