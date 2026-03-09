package pix

func NewDataTexture(data []byte, width, height int, format TextureFormat) *TextureData {
	return &TextureData{
		width:       width,
		height:      height,
		format:      format,
		pendingData: data,
		sampler:     Sampler{MaxAnisotropy: 1},
	}
}
