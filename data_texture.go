package pix

func NewDataTexture(data []byte, width, height int, format TextureFormat) *TextureData {
	return &TextureData{
		id:      textureID.Next(),
		version: 1, //Force upload
		width:   width,
		height:  height,
		format:  format,
		layers:  1,
		pixels:  data,
		sampler: Sampler{MaxAnisotropy: 1},
	}
}
