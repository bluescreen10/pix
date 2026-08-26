// Package shaders embeds the SPIR-V compiled from the GLSL sources in this
// directory. Regenerate with `go generate ./shaders` after editing a source.
package shaders

import _ "embed"

//go:generate glslc --target-env=vulkan1.4 -O triangle.vert -o triangle.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O triangle.frag -o triangle.frag.spv
//go:generate glslc --target-env=vulkan1.4 -O mesh.vert -o mesh.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O mesh.frag -o mesh.frag.spv
//go:generate glslc --target-env=vulkan1.4 -O instanced.comp -o instanced.comp.spv
//go:generate glslc --target-env=vulkan1.4 -O instanced.vert -o instanced.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O fill_indirect.comp -o fill_indirect.comp.spv
//go:generate glslc --target-env=vulkan1.4 -O geo.vert -o geo.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O geo.frag -o geo.frag.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_cull.comp -o scene_cull.comp.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_draw.vert -o scene_draw.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_material.frag -o scene_material.frag.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_lit.frag -o scene_lit.frag.spv
//go:generate glslc --target-env=vulkan1.4 -O textured.vert -o textured.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O textured.frag -o textured.frag.spv
//go:generate glslc --target-env=vulkan1.4 -O overlay.vert -o overlay.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O overlay.frag -o overlay.frag.spv

//go:embed triangle.vert.spv
var TriangleVert []byte

//go:embed triangle.frag.spv
var TriangleFrag []byte

//go:embed mesh.vert.spv
var MeshVert []byte

//go:embed mesh.frag.spv
var MeshFrag []byte

//go:embed instanced.comp.spv
var InstancedComp []byte

//go:embed instanced.vert.spv
var InstancedVert []byte

//go:embed fill_indirect.comp.spv
var FillIndirect []byte

//go:embed geo.vert.spv
var GeoVert []byte

//go:embed geo.frag.spv
var GeoFrag []byte

//go:embed scene_cull.comp.spv
var SceneCull []byte

//go:embed scene_draw.vert.spv
var SceneDraw []byte

//go:embed scene_material.frag.spv
var SceneMaterial []byte

//go:embed scene_lit.frag.spv
var SceneLit []byte

//go:embed textured.vert.spv
var TexturedVert []byte

//go:embed textured.frag.spv
var TexturedFrag []byte

//go:embed overlay.vert.spv
var OverlayVert []byte

//go:embed overlay.frag.spv
var OverlayFrag []byte
