package pix

type Image struct {
	Width  int
	Height int
	Pixels []byte
	Format TextureFormat
}
