package pix

import "github.com/bluescreen10/pix/glm"

// Camera is a viewpoint the renderer computes for itself — the shadow cameras a fit
// aims each frame, exposed through ShadowView. The views a frame is rendered from are
// not Cameras: they come from the packet (see scenes.Camera and FramePacket.Views).
type Camera interface {
	ViewProjection() glm.Mat4f
	Position() glm.Vec3f
}
