package loaders

import (
	"image"
	"os"

	"github.com/bluescreen10/pix"
	"github.com/oliverbestmann/webgpu/wgpu"
)

func LoadTexture(path string) (*pix.TextureData, error) {
	file, err := os.Open(path)

	if err != nil {
		return nil, err
	}

	img, _, err := image.Decode(file)

	if err != nil {
		return nil, err
	}

	// convert to RGBA
	rgba := image.NewRGBA(img.Bounds())
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}

	return pix.NewDataTexture(rgba.Pix, rgba.Bounds().Dx(), rgba.Bounds().Dy(), wgpu.TextureFormatRGBA8Unorm), nil
}
