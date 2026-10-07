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
//     (vertex-pull), scene_depth (the depth-only passes: shadows and the prepass) and fullscreen (the
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
//go:generate go run ../cmd/shadercompile -i src/scene_depth.vert.glsl -o spv:build/scene_depth.vert.spv -o metallib:build/scene_depth.vert.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_depth.vert.glsl -D USE_MASK -o spv:build/scene_depth_masked.vert.spv -o metallib:build/scene_depth_masked.vert.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_depth_masked.frag.glsl -o spv:build/scene_depth_masked.frag.spv -o metallib:build/scene_depth_masked.frag.metalbin
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

// --- ambient occlusion (see Renderer.EnableAmbientOcclusion) ---
//
// Each method's passes are named for it, vbao_* and assao_*; what both share is
// occlusion_*: occlusion.glsl, occlusion_openness.glsl and occlusion_apply.frag.
//
// VBAODepth and VBAODepthMip build a mip chain of the scene's depth; VBAOSlices
// measures how open each surface is from it, VBAODenoise smooths the result, and
// VBAOUpsample brings it up to full resolution. OcclusionApply darkens the scene by
// either method's result, or shows it.

//go:embed build/vbao_depth.comp.spv
var VBAODepth []byte

//go:embed build/vbao_depth_mip.comp.spv
var VBAODepthMip []byte

//go:embed build/vbao_slices.comp.spv
var VBAOSlices []byte

//go:embed build/vbao_denoise.comp.spv
var VBAODenoise []byte

//go:embed build/vbao_upsample.comp.spv
var VBAOUpsample []byte

// ASSAODepth, ASSAOGather and ASSAOBlur are the ASSAO method's passes (see
// AmbientOcclusionASSAO): depth split into four interleaved images, and occlusion
// gathered and blurred in each. OcclusionApply puts the four back together as it draws.

//go:embed build/assao_depth.comp.spv
var ASSAODepth []byte

//go:embed build/assao_gather.comp.spv
var ASSAOGather []byte

//go:embed build/assao_blur.comp.spv
var ASSAOBlur []byte

//go:embed build/occlusion_apply.frag.spv
var OcclusionApply []byte

// --- particles ---

//go:generate go run ../cmd/shadercompile -i src/fullscreen.vert.glsl -o spv:build/fullscreen.vert.spv -o metallib:build/fullscreen.vert.metalbin
//go:generate go run ../cmd/shadercompile -i src/bloom_downsample.frag.glsl -o spv:build/bloom_downsample.frag.spv -o metallib:build/bloom_downsample.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/bloom_upsample.frag.glsl -o spv:build/bloom_upsample.frag.spv -o metallib:build/bloom_upsample.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/bloom_composite.frag.glsl -o spv:build/bloom_composite.frag.spv -o metallib:build/bloom_composite.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/tonemap.frag.glsl -o spv:build/tonemap.frag.spv -o metallib:build/tonemap.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/fxaa.frag.glsl -o spv:build/fxaa.frag.spv -o metallib:build/fxaa.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/halftone.frag.glsl -o spv:build/halftone.frag.spv -o metallib:build/halftone.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/env_prefilter.comp.glsl -o spv:build/env_prefilter.comp.spv -o metallib:build/env_prefilter.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_copy.comp.glsl -o spv:build/scene_copy.comp.spv -o metallib:build/scene_copy.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/scene_copy_mip.comp.glsl -o spv:build/scene_copy_mip.comp.spv -o metallib:build/scene_copy_mip.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/env_brdf.comp.glsl -o spv:build/env_brdf.comp.spv -o metallib:build/env_brdf.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/env_irradiance.comp.glsl -o spv:build/env_irradiance.comp.spv -o metallib:build/env_irradiance.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/env_background.frag.glsl -o spv:build/env_background.frag.spv -o metallib:build/env_background.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/light_clusters.comp.glsl -o spv:build/light_clusters.comp.spv -o metallib:build/light_clusters.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/fog_inject.comp.glsl -o spv:build/fog_inject.comp.spv -o metallib:build/fog_inject.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/fog_integrate.comp.glsl -o spv:build/fog_integrate.comp.spv -o metallib:build/fog_integrate.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/fog_background.frag.glsl -o spv:build/fog_background.frag.spv -o metallib:build/fog_background.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/vbao_depth.comp.glsl -o spv:build/vbao_depth.comp.spv -o metallib:build/vbao_depth.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/vbao_depth_mip.comp.glsl -o spv:build/vbao_depth_mip.comp.spv -o metallib:build/vbao_depth_mip.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/vbao_slices.comp.glsl -o spv:build/vbao_slices.comp.spv -o metallib:build/vbao_slices.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/vbao_denoise.comp.glsl -o spv:build/vbao_denoise.comp.spv -o metallib:build/vbao_denoise.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/vbao_upsample.comp.glsl -o spv:build/vbao_upsample.comp.spv -o metallib:build/vbao_upsample.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/assao_depth.comp.glsl -o spv:build/assao_depth.comp.spv -o metallib:build/assao_depth.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/assao_gather.comp.glsl -o spv:build/assao_gather.comp.spv -o metallib:build/assao_gather.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/assao_blur.comp.glsl -o spv:build/assao_blur.comp.spv -o metallib:build/assao_blur.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/occlusion_apply.frag.glsl -o spv:build/occlusion_apply.frag.spv -o metallib:build/occlusion_apply.frag.metalbin
//go:generate go run ../cmd/shadercompile -i src/particle_update.comp.glsl -o spv:build/particle_update.comp.spv -o metallib:build/particle_update.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/particle_sort_keys.comp.glsl -o spv:build/particle_sort_keys.comp.spv -o metallib:build/particle_sort_keys.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/particle_sort_step.comp.glsl -o spv:build/particle_sort_step.comp.spv -o metallib:build/particle_sort_step.comp.metalbin
//go:generate go run ../cmd/shadercompile -i src/particle_draw.vert.glsl -o spv:build/particle_draw.vert.spv -o metallib:build/particle_draw.vert.metalbin
//go:generate go run ../cmd/shadercompile -i src/particle_basic.frag.glsl -o spv:build/particle_basic.frag.spv -o metallib:build/particle_basic.frag.metalbin

//go:embed build/scene_cull.comp.spv
var SceneCull []byte

//go:embed build/scene_skin.comp.spv
var SceneSkin []byte

//go:embed build/scene_draw.vert.spv
var SceneDraw []byte

// SceneDepthVert is the depth-only passes' vertex stage — the shadow maps and the depth
// prepass — reading positions alone, with no fragment stage after it.
//
//go:embed build/scene_depth.vert.spv
var SceneDepthVert []byte

// SceneDepthMaskedVert and SceneDepthMaskedFrag are the depth-only passes' program for
// masked materials, whatever their type: scene_depth.vert with USE_MASK defined, and a fragment stage that discards where the material has
// no surface.
//
//go:embed build/scene_depth_masked.vert.spv
var SceneDepthMaskedVert []byte

//go:embed build/scene_depth_masked.frag.spv
var SceneDepthMaskedFrag []byte

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

// FXAA smooths the finished frame's edges, after tone mapping (see pix.AntiAliasingFXAA).
//
//go:embed build/fxaa.frag.spv
var FXAA []byte

//go:embed build/halftone.frag.spv
var Halftone []byte

// --- overlay (debug HUD) ---

//go:generate go run ../cmd/shadercompile -i src/overlay.vert.glsl -o spv:build/overlay.vert.spv -o metallib:build/overlay.vert.metalbin
//go:generate go run ../cmd/shadercompile -i src/overlay.frag.glsl -o spv:build/overlay.frag.spv -o metallib:build/overlay.frag.metalbin

//go:embed build/overlay.vert.spv
var OverlayVert []byte

//go:embed build/overlay.frag.spv
var OverlayFrag []byte

// --- scene copy ---
//
// SceneCopy copies the opaque scene before the transparent pass, and SceneCopyMip
// halves it level by level: what materials that show the scene behind them read, while
// they draw into the scene itself.

//go:embed build/scene_copy.comp.spv
var SceneCopy []byte

//go:embed build/scene_copy_mip.comp.spv
var SceneCopyMip []byte

// --- environment lighting (see scenes.Environment) ---
//
// EnvPrefilter, EnvIrradiance and EnvBRDF derive an environment's light — its blurred
// reflections, its diffuse light, and the table reflections are weighted by;
// EnvBackground draws it behind the scene.

//go:embed build/env_prefilter.comp.spv
var EnvPrefilter []byte

//go:embed build/env_brdf.comp.spv
var EnvBRDF []byte

// EnvIrradiance projects an environment's reflections onto spherical harmonics: its
// diffuse light.
//
//go:embed build/env_irradiance.comp.spv
var EnvIrradiance []byte

//go:embed build/env_background.frag.spv
var EnvBackground []byte

// --- light clusters ---

// LightClusters lists, for every cell of the main view's light cluster grid, the point
// and spot lights that reach into it; lit shaders read only their cell's lights.
//
//go:embed build/light_clusters.comp.spv
var LightClusters []byte

// --- volumetric fog (see Renderer.SetVolumetricFog) ---
//
// FogInject and FogIntegrate build the fog volume each frame; lit shaders read it in
// applyFog, and FogBackground fogs the pixels no geometry covers.

//go:embed build/fog_inject.comp.spv
var FogInject []byte

//go:embed build/fog_integrate.comp.spv
var FogIntegrate []byte

//go:embed build/fog_background.frag.spv
var FogBackground []byte

// --- particles ---

//go:embed build/particle_update.comp.spv
var ParticleUpdate []byte

//go:embed build/particle_sort_keys.comp.spv
var ParticleSortKeys []byte

//go:embed build/particle_sort_step.comp.spv
var ParticleSortStep []byte

//go:embed build/particle_draw.vert.spv
var ParticleDraw []byte

//go:embed build/particle_basic.frag.spv
var ParticleBasicFragment []byte
