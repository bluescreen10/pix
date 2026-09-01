// Package shaders embeds the SPIR-V compiled from the GLSL sources in this
// directory. Regenerate with `go generate ./shaders` after editing a source.
//
// The shaders fall into three groups:
//
//   - Scene pipeline (used by the renderer): scene_cull.comp (GPU frustum cull +
//     batch compaction), scene_draw.vert (vertex-pull), and the material fragment
//     shaders scene_unlit / scene_lit / scene_pbr (they share material_common.glsl).
//   - Overlay: overlay.vert/.frag (the debug HUD).
//
// The backend RHI smoke-test shaders (triangle, mesh, instanced, fill_indirect,
// textured) are NOT here — they are test fixtures under gpu/testdata, embedded by
// the backend-agnostic conformance tests. This package holds only shipped shaders.
package shaders

import _ "embed"

// --- scene pipeline ---

//go:generate glslc --target-env=vulkan1.4 -O scene_cull.comp -o scene_cull.comp.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_draw.vert -o scene_draw.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_shadow.vert -o scene_shadow.vert.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_shadow.frag -o scene_shadow.frag.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_basic.frag -o scene_basic.frag.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_lit.frag -o scene_lit.frag.spv
//go:generate glslc --target-env=vulkan1.4 -O scene_pbr.frag -o scene_pbr.frag.spv

//go:embed scene_cull.comp.spv
var SceneCull []byte

//go:embed scene_draw.vert.spv
var SceneDraw []byte

//go:embed scene_shadow.vert.spv
var SceneShadowVert []byte

//go:embed scene_shadow.frag.spv
var SceneShadowFrag []byte

//go:embed scene_basic.frag.spv
var SceneBasic []byte

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
