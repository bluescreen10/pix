package pix

import (
	"embed"
	"io/fs"
)

//go:embed shaderlib
var shaderlibEmbed embed.FS

var shaderlib fs.FS = func() fs.FS {
	sub, err := fs.Sub(shaderlibEmbed, "shaderlib")
	if err != nil {
		panic(err)
	}
	return sub
}()
