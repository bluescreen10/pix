// Package shaders embeds SPIR-V and native Metal libraries used by the renderer.
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
// Regenerate with `go generate ./shaders` after editing a source. This requires
// glslc, spirv-cross, and Xcode with the Metal Toolchain installed.
//
// The shaders fall into three groups:
//
//   - Scene pipeline: scene_cull (GPU frustum cull + batch compaction), scene_draw
//     (vertex-pull), scene_shadow (depth-only shadow pass) and fullscreen (the
//     deferred lighting pass's fullscreen triangle).
//
//   - Overlay: overlay.vert/.frag (the debug HUD).
//
//   - Per-material fragment shaders: the built-in materials in the materials
//     package (scene_basic, scene_lit, and PBR's forward/deferred/lighting trio).
//     They are embedded here rather than beside the material that owns them because
//     //go:embed cannot reach outside its own package directory.
//
// Low-level backend conformance shaders live in GameKit. This package holds only
// shaders shipped by Pix's renderer.
package shaders

import _ "embed"

// --- scene pipeline ---

//go:generate glslc -fshader-stage=compute --target-env=vulkan1.4 -O src/scene_cull.comp.glsl -o build/scene_cull.comp.spv
//go:generate go run ../cmd/metalshader -in build/scene_cull.comp.spv -out build/scene_cull.comp.metalbin -metallib
//go:generate glslc -fshader-stage=compute --target-env=vulkan1.4 -O src/scene_skin.comp.glsl -o build/scene_skin.comp.spv
//go:generate go run ../cmd/metalshader -in build/scene_skin.comp.spv -out build/scene_skin.comp.metalbin -metallib
//go:generate glslc -fshader-stage=vertex --target-env=vulkan1.4 -O src/scene_draw.vert.glsl -o build/scene_draw.vert.spv
//go:generate go run ../cmd/metalshader -in build/scene_draw.vert.spv -out build/scene_draw.vert.metalbin -metallib
//go:generate glslc -fshader-stage=vertex --target-env=vulkan1.4 -O src/scene_shadow.vert.glsl -o build/scene_shadow.vert.spv
//go:generate go run ../cmd/metalshader -in build/scene_shadow.vert.spv -out build/scene_shadow.vert.metalbin -metallib
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O src/scene_shadow.frag.glsl -o build/scene_shadow.frag.spv
//go:generate go run ../cmd/metalshader -in build/scene_shadow.frag.spv -out build/scene_shadow.frag.metalbin -metallib
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O src/scene_basic.frag.glsl -o build/scene_basic.frag.spv
//go:generate go run ../cmd/metalshader -in build/scene_basic.frag.spv -out build/scene_basic.frag.metalbin -metallib
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O src/scene_lit.frag.glsl -o build/scene_lit.frag.spv
//go:generate go run ../cmd/metalshader -in build/scene_lit.frag.spv -out build/scene_lit.frag.metalbin -metallib

// PBRMaterial's three render paths all come from one source, selected by -D.
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O -DPIX_PASS_FORWARD src/scene_pbr.frag.glsl -o build/scene_forward_pbr.frag.spv
//go:generate go run ../cmd/metalshader -in build/scene_forward_pbr.frag.spv -out build/scene_forward_pbr.frag.metalbin -metallib
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O -DPIX_PASS_DEFERRED src/scene_pbr.frag.glsl -o build/scene_deferred_pbr.frag.spv
//go:generate go run ../cmd/metalshader -in build/scene_deferred_pbr.frag.spv -out build/scene_deferred_pbr.frag.metalbin -metallib
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O -DPIX_PASS_LIGHTING src/scene_pbr.frag.glsl -o build/scene_lighting_pbr.frag.spv
//go:generate go run ../cmd/metalshader -in build/scene_lighting_pbr.frag.spv -out build/scene_lighting_pbr.frag.metalbin -metallib

//go:generate glslc -fshader-stage=vertex --target-env=vulkan1.4 -O src/fullscreen.vert.glsl -o build/fullscreen.vert.spv
//go:generate go run ../cmd/metalshader -in build/fullscreen.vert.spv -out build/fullscreen.vert.metalbin -metallib
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O src/gbuffer_debug.frag.glsl -o build/gbuffer_debug.frag.spv
//go:generate go run ../cmd/metalshader -in build/gbuffer_debug.frag.spv -out build/gbuffer_debug.frag.metalbin -metallib

//go:embed build/scene_cull.comp.spv
var SceneCull []byte

//go:embed build/scene_skin.comp.spv
var SceneSkin []byte

//go:embed build/scene_draw.vert.spv
var SceneDraw []byte

//go:embed build/scene_shadow.vert.spv
var SceneShadowVert []byte

//go:embed build/scene_shadow.frag.spv
var SceneShadowFrag []byte

//go:embed build/fullscreen.vert.spv
var FullscreenVert []byte

// GBufferDebug shows one G-buffer target fullscreen instead of the shaded result
// (see Renderer.SetDebugView). Shares the deferred lighting pass's root struct.
//
//go:embed build/gbuffer_debug.frag.spv
var GBufferDebug []byte

// --- built-in material fragment shaders (see the materials package) ---

//go:embed build/scene_basic.frag.spv
var BasicForward []byte

//go:embed build/scene_lit.frag.spv
var BlinnPhongForward []byte

// PBR's three render paths, all compiled from src/scene_pbr.frag.glsl via -D.

//go:embed build/scene_forward_pbr.frag.spv
var PBRForward []byte

//go:embed build/scene_deferred_pbr.frag.spv
var PBRDeferred []byte

//go:embed build/scene_lighting_pbr.frag.spv
var PBRLighting []byte

// --- overlay (debug HUD) ---

//go:generate glslc -fshader-stage=vertex --target-env=vulkan1.4 -O src/overlay.vert.glsl -o build/overlay.vert.spv
//go:generate go run ../cmd/metalshader -in build/overlay.vert.spv -out build/overlay.vert.metalbin -metallib
//go:generate glslc -fshader-stage=fragment --target-env=vulkan1.4 -O src/overlay.frag.glsl -o build/overlay.frag.spv
//go:generate go run ../cmd/metalshader -in build/overlay.frag.spv -out build/overlay.frag.metalbin -metallib

//go:embed build/overlay.vert.spv
var OverlayVert []byte

//go:embed build/overlay.frag.spv
var OverlayFrag []byte
