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
//go:generate go run ../cmd/shadercompile -i src/scene_basic.frag.glsl -o spv:build/scene_basic.frag.spv -o metallib:build/scene_basic.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_lit.frag.glsl -o spv:build/scene_lit.frag.spv -o metallib:build/scene_lit.frag.metalbin

// PBRMaterial's three render paths all come from one source, selected by -D.
//go:generate go run ../cmd/shadercompile -i src/scene_pbr.frag.glsl -o spv:build/scene_pbr.frag.spv -o metallib:build/scene_pbr.frag.metalbin

//go:generate go run ../cmd/shadercompile -i src/scene_debug_id.vert.glsl -o spv:build/scene_debug_id.vert.spv -o metallib:build/scene_debug_id.vert.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_debug_normal.frag.glsl -o spv:build/scene_debug_normal.frag.spv -o metallib:build/scene_debug_normal.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_debug_depth.frag.glsl -o spv:build/scene_debug_depth.frag.spv -o metallib:build/scene_debug_depth.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_debug_position.frag.glsl -o spv:build/scene_debug_position.frag.spv -o metallib:build/scene_debug_position.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_debug_object.frag.glsl -o spv:build/scene_debug_object.frag.spv -o metallib:build/scene_debug_object.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_debug_triangle.frag.glsl -o spv:build/scene_debug_triangle.frag.spv -o metallib:build/scene_debug_triangle.frag.metalbin

// --- particles ---

//go:generate go run ../cmd/shadercompile -i src/fullscreen.vert.glsl -o spv:build/fullscreen.vert.spv -o metallib:build/fullscreen.vert.metalbin
//go:generate go run ../cmd/shadercompile -i src/bloom_downsample.frag.glsl -o spv:build/bloom_downsample.frag.spv -o metallib:build/bloom_downsample.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/bloom_upsample.frag.glsl -o spv:build/bloom_upsample.frag.spv -o metallib:build/bloom_upsample.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/bloom_composite.frag.glsl -o spv:build/bloom_composite.frag.spv -o metallib:build/bloom_composite.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/tonemap.frag.glsl -o spv:build/tonemap.frag.spv -o metallib:build/tonemap.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/halftone.frag.glsl -o spv:build/halftone.frag.spv -o metallib:build/halftone.frag.metalbin
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

// --- debug views (see Renderer.SetDebugView) ---
//
// Each view is a dedicated fragment shader over a real geometry pass, not a re-read of
// a stored target. SceneDebugIDVert forwards the drawable index for the object view;
// the surface views (normal/depth/position) run over the ordinary scene vertex stage
// and read only its varyings, so they work for every material type.

//go:embed build/scene_debug_id.vert.spv
var SceneDebugIDVert []byte

//go:embed build/scene_debug_normal.frag.spv
var SceneDebugNormal []byte

//go:embed build/scene_debug_depth.frag.spv
var SceneDebugDepth []byte

//go:embed build/scene_debug_position.frag.spv
var SceneDebugPosition []byte

//go:embed build/scene_debug_object.frag.spv
var SceneDebugObject []byte

//go:embed build/scene_debug_triangle.frag.spv
var SceneDebugTriangle []byte

// --- built-in material fragment shaders (see the materials package) ---

//go:embed build/scene_basic.frag.spv
var BasicFragment []byte

//go:embed build/scene_lit.frag.spv
var BlinnPhongFragment []byte

//go:embed build/scene_pbr.frag.spv
var PBRFragment []byte

// --- post-processing (see Renderer.SetPostProcessing) ---
//
// Every post-processing pass draws one full-screen triangle, generated in the vertex
// stage. The fragment shaders share postfx.glsl's root layout. ToneMap is the HDR
// frame's last pass: it tone-maps and encodes for display, into the display target.

//go:embed build/fullscreen.vert.spv
var FullscreenVert []byte

//go:embed build/bloom_downsample.frag.spv
var BloomDownsample []byte

//go:embed build/bloom_upsample.frag.spv
var BloomUpsample []byte

//go:embed build/bloom_composite.frag.spv
var BloomComposite []byte

//go:embed build/tonemap.frag.spv
var ToneMap []byte

//go:embed build/halftone.frag.spv
var Halftone []byte

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
var ParticleBasicFragment []byte
