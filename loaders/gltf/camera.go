package gltf

import (
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// omittedFarPerNear places the far plane of a perspective camera whose glTF zfar is
// omitted — an infinite far plane in glTF — at this multiple of its near plane. Pix
// has no infinite projection, and its light clusters slice depth logarithmically from
// near to far with near clamped to at least a millionth of far, so a fixed distance
// such as three.js's 2e6 would push the clusters' near out to 2 units behind a 0.01
// near plane. A ratio keeps every camera within that range.
const omittedFarPerNear = 1e5

// addCamera creates the scene camera for a node's glTF camera, as a child of parent
// with no transform of its own, so it looks down the node's -Z axis with +Y up as
// glTF's camera does. The camera starts hidden: the renderer draws the scene's first
// visible camera, and an asset's camera must not take that over from the caller's
// unasked. To look through it, find it with Scene.CameraByName or Scene.Cameras, hide
// the camera rendering now, and show it. A perspective camera without an aspect ratio gets the renderer's. It takes
// the glTF camera's name, or the node's if the camera has none.
func (l *loader) addCamera(parent scenes.Node, nodeIdx int) {
	gn := l.doc.Nodes[nodeIdx]
	if *gn.Camera < 0 || *gn.Camera >= len(l.doc.Cameras) {
		return
	}
	gc := l.doc.Cameras[*gn.Camera]

	var cam scenes.Camera
	switch {
	case gc.Type == "orthographic" && gc.Orthographic != nil:
		o := gc.Orthographic
		cam = l.scene.NewOrthographicCamera(-o.XMag, o.XMag, -o.YMag, o.YMag, o.Znear, o.Zfar)
	case gc.Type == "perspective" && gc.Perspective != nil:
		p := gc.Perspective
		aspect := l.renderer.Aspect()
		if p.AspectRatio != nil && *p.AspectRatio > 0 {
			aspect = *p.AspectRatio
		}
		far := p.Znear * omittedFarPerNear
		if p.Zfar != nil {
			far = *p.Zfar
		}
		cam = l.scene.NewPerspectiveCamera(glm.ToDegrees(p.YFov), aspect, p.Znear, far)
	default:
		return
	}

	name := gc.Name
	if name == "" {
		name = gn.Name
	}
	cam.SetName(name)
	cam.SetVisible(false)
	parent.Add(cam)
}
