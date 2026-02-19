package pix

import "github.com/bluescreen10/pix/glm"

type Node interface {
	Add(child Node)
	Del(child Node)
	Children() []Node
}

var _ Node = &node{}

type node struct {
	children []Node
}

func (n *node) Add(child Node) {
	n.children = append(n.children, child)
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
