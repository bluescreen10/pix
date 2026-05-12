package pix

import "github.com/bluescreen10/pix/glm"

type Node interface {
	Add(child Node)
	Del(child Node)
	SetParent(parent Node)
	Children() []Node
	Model() glm.Mat4f
	InvModel() glm.Mat4f
	UpdateMatrix(force bool) bool
	CastShadow() bool
	ReceiveShadow() bool
}
