package pix

func NewDataTexture(data []byte, width, height int, format TextureFormat) Texture {
	return Texture{
		Width:       width,
		Height:      height,
		Format:      format,
		PendingData: data,
		Sampler:     Sampler{MaxAnisotropy: 1},
	}
}
