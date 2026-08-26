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

func (c Color3[T]) RGBA() Color4[T] {
	return Color4[T]{c[0], c[1], c[2], 1}
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

type RGBA32F = Color4[float32]
type RGBA32 = Color3[int]

// RGBA8 is a color packed into 32 bits as four 8-bit unsigned-normalized
// channels, red in the low byte (matches the WGSL rgba8unorm / unpack4x8unorm
// byte order).
type RGBA8 = Color4[uint8]

// Vec4f unpacks the color back to floats in [0,1].
func (c Color4[T]) Vec4f() Vec4f {
	return Vec4f{
		float32(c[0]) / 255.0,
		float32(c[1]) / 255.0,
		float32(c[2]) / 255.0,
		float32(c[3]) / 255.0,
	}
}
