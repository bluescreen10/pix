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

//go:generate go run ../cmd/shadercompile -i src/scene_cull.comp.glsl -o spv:build/scene_cull.comp.spv -o metallib:build/scene_cull.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_skin.comp.glsl -o spv:build/scene_skin.comp.spv -o metallib:build/scene_skin.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_draw.vert.glsl -o spv:build/scene_draw.vert.spv -o metallib:build/scene_draw.vert.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_shadow.vert.glsl -o spv:build/scene_shadow.vert.spv -o metallib:build/scene_shadow.vert.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_shadow.frag.glsl -o spv:build/scene_shadow.frag.spv -o metallib:build/scene_shadow.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_basic.frag.glsl -o spv:build/scene_basic.frag.spv -o metallib:build/scene_basic.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_lit.frag.glsl -o spv:build/scene_lit.frag.spv -o metallib:build/scene_lit.frag.metalbin

// PBRMaterial's three render paths all come from one source, selected by -D.
//go:generate go run ../cmd/shadercompile -i src/scene_pbr.frag.glsl -D PIX_PASS_FORWARD -o spv:build/scene_forward_pbr.frag.spv -o metallib:build/scene_forward_pbr.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_pbr.frag.glsl -D PIX_PASS_DEFERRED -o spv:build/scene_deferred_pbr.frag.spv -o metallib:build/scene_deferred_pbr.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_pbr.frag.glsl -D PIX_PASS_LIGHTING -o spv:build/scene_lighting_pbr.frag.spv -o metallib:build/scene_lighting_pbr.frag.metalbin

//go:generate go run ../cmd/shadercompile -i src/fullscreen.vert.glsl -o spv:build/fullscreen.vert.spv -o metallib:build/fullscreen.vert.metalbin
//go:generate go run ../cmd/shadercompile -i src/gbuffer_debug.frag.glsl -o spv:build/gbuffer_debug.frag.spv -o metallib:build/gbuffer_debug.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_debug_id.vert.glsl -o spv:build/scene_debug_id.vert.spv -o metallib:build/scene_debug_id.vert.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_debug_id.frag.glsl -o spv:build/scene_debug_id.frag.spv -o metallib:build/scene_debug_id.frag.metalbin

// --- particles ---

//go:generate go run ../cmd/shadercompile -i src/particle_update.comp.glsl -o spv:build/particle_update.comp.spv -o metallib:build/particle_update.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/particle_draw.vert.glsl -o spv:build/particle_draw.vert.spv -o metallib:build/particle_draw.vert.metalbin
//go:generate go run ../cmd/shadercompile -i src/particle_basic.frag.glsl -o spv:build/particle_basic.frag.spv -o metallib:build/particle_basic.frag.metalbin

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

//go:embed build/scene_debug_id.vert.spv
var SceneDebugIDVert []byte

//go:embed build/scene_debug_id.frag.spv
var SceneDebugIDFrag []byte

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

//go:generate go run ../cmd/shadercompile -i src/overlay.vert.glsl -o spv:build/overlay.vert.spv -o metallib:build/overlay.vert.metalbin
//go:generate go run ../cmd/shadercompile -i src/overlay.frag.glsl -o spv:build/overlay.frag.spv -o metallib:build/overlay.frag.metalbin

//go:embed build/overlay.vert.spv
var OverlayVert []byte

//go:embed build/overlay.frag.spv
var OverlayFrag []byte

// --- particles ---

//go:embed build/particle_update.comp.spv
var ParticleUpdate []byte

//go:embed build/particle_draw.vert.spv
var ParticleDraw []byte

//go:embed build/particle_basic.frag.spv
var ParticleBasicForward []byte
