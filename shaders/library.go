package shaders

import "embed"

// Library holds the public GLSL headers, under lib/. Shaders written outside pix
// include them as <pix/name.glsl>; cmd/shaderc puts them on the include path, so a
// client compiles against the headers of the pix version it links.
//
//go:embed lib
var Library embed.FS
