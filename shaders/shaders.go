// Package shaders embeds the SPIR-V used by the renderer.
//
// Layout:
//
//   - src/   the GLSL sources. Every file is .glsl; stage shaders keep the stage in
//     the name (scene_draw.vert.glsl), and files with no stage suffix
//     (material_common.glsl, lighting.glsl, gbuffer.glsl) are include-only
//     headers. Because .glsl implies no stage to glslc, every rule below
//     passes -fshader-stage explicitly.
//   - build/ the compiled .spv, embedded from here. Checked in, so building pix needs
//     no shader toolchain — only editing a shader does.
//
// Regenerate with `go generate ./shaders` after editing a source (requires glslc).
//
// The shaders fall into three groups:
//
//   - Scene pipeline: scene_cull (GPU frustum cull + batch compaction), scene_draw
//     (vertex-pull), scene_shadow (depth-only shadow pass) and fullscreen (the
//     deferred lighting pass's fullscreen triangle).
//   - Overlay: overlay.vert/.frag (the debug HUD).
//
// The per-material fragment shaders are compiled here (the rules below) but embedded
// by the material that owns them — see the //go:embed in basic_material.go,
// blinn_phong_material.go and pbr_material.go, so a material's shaders are visible
// where the material is.
//
// The backend RHI smoke-test shaders (triangle, mesh, instanced, fill_indirect,
// textured) are NOT here — they are test fixtures under gpu/testdata, embedded by
// the backend-agnostic conformance tests. This package holds only shipped shaders.
package shaders

import _ "embed"

// --- scene pipeline ---

//go:generate glslc -fshader-stage=compute --target-env=vulkan1.4 -O src/scene_cull.comp.glsl -o build/scene_cull.comp.spv
//go:generate glslc -fshader-stage=vertex --target-env=vulkan1.4 -O src/scene_draw.vert.glsl -o build/scene_draw.vert.spv
//go:generate glslc -fshader-stage=vertex --target-env=vulkan1.4 -O src/scene_shadow.vert.glsl -o build/scene_shadow.vert.spv
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O src/scene_shadow.frag.glsl -o build/scene_shadow.frag.spv
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O src/scene_basic.frag.glsl -o build/scene_basic.frag.spv
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O src/scene_lit.frag.glsl -o build/scene_lit.frag.spv

// PBRMaterial's three render paths all come from one source, selected by -D.
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O -DPIX_PASS_FORWARD src/scene_pbr.frag.glsl -o build/scene_forward_pbr.frag.spv
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O -DPIX_PASS_DEFERRED src/scene_pbr.frag.glsl -o build/scene_deferred_pbr.frag.spv
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O -DPIX_PASS_LIGHTING src/scene_pbr.frag.glsl -o build/scene_lighting_pbr.frag.spv

//go:generate glslc -fshader-stage=vertex --target-env=vulkan1.4 -O src/fullscreen.vert.glsl -o build/fullscreen.vert.spv

//go:embed build/scene_cull.comp.spv
var SceneCull []byte

//go:embed build/scene_draw.vert.spv
var SceneDraw []byte

//go:embed build/scene_shadow.vert.spv
var SceneShadowVert []byte

//go:embed build/scene_shadow.frag.spv
var SceneShadowFrag []byte

//go:embed build/fullscreen.vert.spv
var FullscreenVert []byte

// --- overlay (debug HUD) ---

//go:generate glslc -fshader-stage=vertex --target-env=vulkan1.4 -O src/overlay.vert.glsl -o build/overlay.vert.spv
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O src/overlay.frag.glsl -o build/overlay.frag.spv

//go:embed build/overlay.vert.spv
var OverlayVert []byte

//go:embed build/overlay.frag.spv
var OverlayFrag []byte
