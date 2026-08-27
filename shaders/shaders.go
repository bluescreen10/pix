// Package shaders embeds the SPIR-V compiled from the GLSL sources in this
// directory. Regenerate with `go generate ./shaders` after editing a source.
//
// The shaders fall into three groups:
//
//   - Scene pipeline (used by the renderer): scene_cull.comp (GPU frustum cull +
//     batch compaction), scene_draw.vert (vertex-pull), and the material fragment
//     shaders scene_unlit / scene_lit / scene_pbr (they share material_common.glsl).
//   - Overlay: overlay.vert/.frag (the debug HUD).
//   - Dev/spike programs used only by the gpu/vulkan tests and the vktriangle
//     example, kept as minimal RHI smoke tests: triangle, mesh, instanced,
//     fill_indirect, textured.
package shaders

import _ "embed"

// --- scene pipeline ---

//go:generate glslc --target-env=vulkan1.4 -O scene_cull.comp -o scene_cull.comp.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_draw.vert -o scene_draw.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_unlit.frag -o scene_unlit.frag.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_lit.frag -o scene_lit.frag.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_pbr.frag -o scene_pbr.frag.spv

//go:embed scene_cull.comp.spv
var SceneCull []byte

//go:embed scene_draw.vert.spv
var SceneDraw []byte

//go:embed scene_unlit.frag.spv
var SceneUnlit []byte

//go:embed scene_lit.frag.spv
var SceneLit []byte

//go:embed scene_pbr.frag.spv
var ScenePBR []byte

// --- overlay (debug HUD) ---

//go:generate glslc --target-env=vulkan1.4 -O overlay.vert -o overlay.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O overlay.frag -o overlay.frag.spv

//go:embed overlay.vert.spv
var OverlayVert []byte

//go:embed overlay.frag.spv
var OverlayFrag []byte

// --- dev/spike programs (gpu/vulkan tests + vktriangle example only) ---

//go:generate glslc --target-env=vulkan1.4 -O triangle.vert -o triangle.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O triangle.frag -o triangle.frag.spv
//go:generate glslc --target-env=vulkan1.4 -O mesh.vert -o mesh.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O mesh.frag -o mesh.frag.spv
//go:generate glslc --target-env=vulkan1.4 -O instanced.comp -o instanced.comp.spv
//go:generate glslc --target-env=vulkan1.4 -O instanced.vert -o instanced.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O fill_indirect.comp -o fill_indirect.comp.spv
//go:generate glslc --target-env=vulkan1.4 -O textured.vert -o textured.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O textured.frag -o textured.frag.spv

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

//go:embed textured.vert.spv
var TexturedVert []byte

//go:embed textured.frag.spv
var TexturedFrag []byte
