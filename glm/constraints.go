package glm

import "golang.org/x/exp/constraints"

type number interface {
	constraints.Float | constraints.Signed | constraints.Unsigned
}
