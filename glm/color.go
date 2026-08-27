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

// Color3 aliases. Names follow the per-channel convention (channel bit width + type
// suffix: none = unsigned normalized, F = float, I = signed int, UI = unsigned int).
type (
	RGB8   = Color3[uint8]   // 8-bit unsigned-normalized per channel
	RGB32F = Color3[float32] // 32-bit float per channel
	RGB32I = Color3[int32]   // 32-bit signed int per channel
)

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

// Color4 aliases (same per-channel convention as the Color3 set above).
type (
	RGBA32F = Color4[float32] // 32-bit float per channel
	RGBA32I = Color4[int32]   // 32-bit signed int per channel

	// RGBA8 is a color packed into 32 bits as four 8-bit unsigned-normalized
	// channels, red in the low byte (matches the WGSL rgba8unorm / unpack4x8unorm
	// byte order).
	RGBA8 = Color4[uint8]
)

// Vec4f unpacks the color back to floats in [0,1].
func (c Color4[T]) Vec4f() Vec4f {
	return Vec4f{
		float32(c[0]) / 255.0,
		float32(c[1]) / 255.0,
		float32(c[2]) / 255.0,
		float32(c[3]) / 255.0,
	}
}
