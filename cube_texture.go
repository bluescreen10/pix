package pix

import "fmt"

func (r *Renderer) NewCubeTexture(imgs []Image) Texture {
	if len(imgs) != 6 {
		panic("incorrect number of images")
	}

	width := imgs[0].Width
	height := imgs[0].Height
	format := imgs[0].Format
	layers := len(imgs)

	texelSize := format.Size()

	pixels := make([]byte, 0, width*height*layers*texelSize)

	for _, img := range imgs {
		if img.Width != width {
			panic(fmt.Sprintf("invalid width %d expected %d", img.Width, width))
		}

		if img.Height != height {
			panic(fmt.Sprintf("invalid width %d expected %d", img.Height, height))
		}

		if img.Format != format {
			panic(fmt.Sprintf("invalid width %s expected %s", img.Format, format))
		}

		pixels = append(pixels, img.Pixels...)
	}

	return r.allocTextureSlot(&TextureData{
		version: 1,
		width:   width,
		height:  height,
		format:  format,
		layers:  layers,
		pixels:  pixels,
	})
}
