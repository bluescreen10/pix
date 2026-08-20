package glm

// RGB10A2 packs three components into 32 bits as 10-bit unsigned-normalized
// values (red in the low 10 bits) plus a 2-bit high field. Also known as the
// R10G10B10A2 / 2-10-10-10 format. Typically used to store unit normals via
// PackNormal.
type RGB10A2 uint32

// Vec3f unpacks the three components back to [0,1].
func (v RGB10A2) Vec3f() Vec3f {
	return Vec3f{
		float32(v&0x3FF) / 1023.0,
		float32((v>>10)&0x3FF) / 1023.0,
		float32((v>>20)&0x3FF) / 1023.0,
	}
}
