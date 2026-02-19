package glm

type Color3[T number] [3]T

func (c Color3[T]) R() T {
	return c[0]
}

func (c Color3[T]) G() T {
	return c[1]
}

func (c Color3[T]) B() T {
	return c[2]
}

type Color3f = Color3[float32]
type Color3i = Color3[int]

type Color4[T number] [4]T

func (c Color4[T]) R() T {
	return c[0]
}

func (c Color4[T]) G() T {
	return c[1]
}

func (c Color4[T]) B() T {
	return c[2]
}

func (c Color4[T]) A() T {
	return c[3]
}

type Color4f = Color4[float32]
type Color4i = Color3[int]
