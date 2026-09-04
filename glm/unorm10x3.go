package glm

// Unorm10x3 packs three unsigned-normalized values into 32 bits, 10 bits each (the
// first in the low bits), leaving the top 2 bits unused. Each channel stores a float
// in [0,1] quantized to 1/1023. Pack with Vec3.Unorm10x3, unpack with Vec3f.
//
// The name describes the encoding, not a use: this is not a colour, and the type it
// replaced (RGB10A2, after the GPU format R10G10B10A2) was misnamed. Nothing here
// binds to that hardware format — both halves of the codec are hand-written — so the
// channels have no colour meaning. gpu's FormatRGB10A2Unorm keeps the hardware name
// because it genuinely is that format.
type Unorm10x3 uint32

// Vec3f unpacks the three channels back to [0,1].
func (v Unorm10x3) Vec3f() Vec3f {
	return Vec3f{
		float32(v&0x3FF) / 1023.0,
		float32((v>>10)&0x3FF) / 1023.0,
		float32((v>>20)&0x3FF) / 1023.0,
	}
}
