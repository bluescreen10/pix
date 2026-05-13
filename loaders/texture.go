package loaders

import (
	"image"
	"os"

	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix"
)

func LoadTexture(r *pix.Renderer, path string) (pix.Texture, error) {
	file, err := os.Open(path)
	if err != nil {
		return pix.Texture{}, err
	}

	img, _, err := image.Decode(file)
	if err != nil {
		return pix.Texture{}, err
	}

	rgba := image.NewRGBA(img.Bounds())
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}

	td := pix.NewDataTexture(rgba.Pix, rgba.Bounds().Dx(), rgba.Bounds().Dy(), wgpu.TextureFormatRGBA8Unorm)
	return r.NewTexture(td), nil
}
