// Package colors holds the engine's colour value types: a generic RGB/RGBA pair plus
// aliases for the channel formats actually used on the GPU.
//
// Colours live here rather than in glm because a colour is not a vector. They happen
// to share a memory layout, but a direction and a tint are not interchangeable, and
// keeping them as distinct named types is what lets the compiler catch a swapped
// argument — Scene.AddDirectionalLight(dir, color) being the obvious case.
//
// Naming follows the graphics-API convention: channel set, then bit width, then a type
// suffix (none = unsigned normalized, F = float, I = signed int). RGBA32F is the same
// thing Vulkan calls VK_FORMAT_R32G32B32A32_SFLOAT.
//
// Values are LINEAR unless a name says otherwise. Nothing in the type system enforces
// that — it is a convention the shading path relies on, encoding to sRGB only at the
// very end of a fragment shader.
package colors

import "github.com/bluescreen10/pix/glm"

// number is the set of channel types a colour can be stored in. It mirrors glm's own
// constraint; colours are declared over the same scalar set as vectors.
type number interface {
	~float32 | ~float64 |
		~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// RGB is a colour with no alpha: three channels of T.
type RGB[T number] [3]T

func (c RGB[T]) R() T {
	return c[0]
}

func (c RGB[T]) G() T {
	return c[1]
}

func (c RGB[T]) B() T {
	return c[2]
}

// RGBA returns the colour with an opaque alpha added.
func (c RGB[T]) RGBA() RGBA[T] {
	return RGBA[T]{c[0], c[1], c[2], 1}
}

// RGB aliases. Names follow the per-channel convention described on the package.
type (
	RGB8   = RGB[uint8]   // 8-bit unsigned-normalized per channel
	RGB32F = RGB[float32] // 32-bit float per channel
	RGB32I = RGB[int32]   // 32-bit signed int per channel
)

// RGBA is a colour plus an alpha channel. Strictly, alpha is coverage rather than a
// colour component — Porter & Duff call it the matte — but every graphics API from CSS
// to Skia treats the quadruple as one colour value, and so does this.
type RGBA[T number] [4]T

func (c RGBA[T]) R() T {
	return c[0]
}

func (c RGBA[T]) G() T {
	return c[1]
}

func (c RGBA[T]) B() T {
	return c[2]
}

func (c RGBA[T]) A() T {
	return c[3]
}

func (c RGBA[T]) RGBA8() RGBA8 {
	return RGBA8{
		uint8(float32(Clamp(c[0], 0, 1)) * 255.0),
		uint8(float32(Clamp(c[1], 0, 1)) * 255.0),
		uint8(float32(Clamp(c[2], 0, 1)) * 255.0),
		uint8(float32(Clamp(c[3], 0, 1)) * 255.0),
	}
}

// RGB drops the alpha channel.
func (c RGBA[T]) RGB() RGB[T] {
	return RGB[T]{c[0], c[1], c[2]}
}

// RGBA aliases (same per-channel convention as the RGB set above).
type (
	RGBA32F = RGBA[float32] // 32-bit float per channel
	RGBA32I = RGBA[int32]   // 32-bit signed int per channel

	// RGBA8 is a colour packed into 32 bits as four 8-bit unsigned-normalized
	// channels, red in the low byte (matches the WGSL rgba8unorm / unpack4x8unorm
	// byte order).
	RGBA8 = RGBA[uint8]
)

// Vec4f unpacks an 8-bit-per-channel colour back to floats in [0,1].
func (c RGBA[T]) Vec4f() glm.Vec4f {
	return glm.Vec4f{
		float32(c[0]) / 255.0,
		float32(c[1]) / 255.0,
		float32(c[2]) / 255.0,
		float32(c[3]) / 255.0,
	}
}

func Clamp[T number](x, min, max T) T {
	if x < min {
		return min
	} else if x > max {
		return max
	}
	return x
}
