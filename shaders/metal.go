package shaders

import (
	"bytes"
	_ "embed"
)

// Select returns the variant of a shader that backend runs: metallib on Metal, spirv on
// everything else. It is for shaders compiled outside this package — by a package of
// effects built on pix, say, with cmd/shadercompile — which ForBackend does not know.
func Select(backend any, spirv, metallib []byte) []byte {
	if format, ok := backend.(interface{ ShaderFormat() string }); ok && format.ShaderFormat() == "metal" {
		return metallib
	}
	return spirv
}

// ForBackend selects a precompiled native variant of a built-in shader. Unknown
// shader bytes pass through unchanged: custom materials must provide shaders in
// their backend's format. No shader tools run when an application starts.
func ForBackend(backend any, code []byte) []byte {
	format, ok := backend.(interface{ ShaderFormat() string })
	if !ok || format.ShaderFormat() != "metal" || len(code) == 0 {
		return code
	}
	for _, p := range metalVariants {
		if len(code) == len(p.spirv) && &code[0] == &p.spirv[0] {
			return p.metal
		}
	}
	for _, p := range metalVariants {
		if bytes.Equal(code, p.spirv) {
			return p.metal
		}
	}
	return code
}

//go:embed build/scene_cull.comp.metalbin
var metalSceneCull []byte

//go:embed build/scene_skin.comp.metalbin
var metalSceneSkin []byte

//go:embed build/scene_draw.vert.metalbin
var metalSceneDraw []byte

//go:embed build/scene_shadow.vert.metalbin
var metalSceneShadowVert []byte

//go:embed build/scene_debug_id.vert.metalbin
var metalSceneDebugIDVert []byte

//go:embed build/scene_debug_normal.frag.metalbin
var metalSceneDebugNormal []byte

//go:embed build/scene_debug_depth.frag.metalbin
var metalSceneDebugDepth []byte

//go:embed build/scene_debug_position.frag.metalbin
var metalSceneDebugPosition []byte

//go:embed build/scene_debug_object.frag.metalbin
var metalSceneDebugObject []byte

//go:embed build/scene_debug_triangle.frag.metalbin
var metalSceneDebugTriangle []byte

//go:embed build/scene_basic.frag.metalbin
var metalBasicFragment []byte

//go:embed build/scene_lit.frag.metalbin
var metalBlinnPhongFragment []byte

//go:embed build/scene_pbr.frag.metalbin
var metalPBRFragment []byte

//go:embed build/overlay.vert.metalbin
var metalOverlayVert []byte

//go:embed build/overlay.frag.metalbin
var metalOverlayFrag []byte

//go:embed build/env_prefilter.comp.metalbin
var metalEnvPrefilter []byte

//go:embed build/env_brdf.comp.metalbin
var metalEnvBRDF []byte

//go:embed build/env_background.frag.metalbin
var metalEnvBackground []byte

//go:embed build/fog_inject.comp.metalbin
var metalFogInject []byte

//go:embed build/fog_integrate.comp.metalbin
var metalFogIntegrate []byte

//go:embed build/fog_background.frag.metalbin
var metalFogBackground []byte

//go:embed build/particle_update.comp.metalbin
var metalParticleUpdate []byte

//go:embed build/particle_sort_keys.comp.metalbin
var metalParticleSortKeys []byte

//go:embed build/particle_sort_step.comp.metalbin
var metalParticleSortStep []byte

//go:embed build/particle_draw.vert.metalbin
var metalParticleDraw []byte

//go:embed build/particle_basic.frag.metalbin
var metalParticleBasicFragment []byte

//go:embed build/fullscreen.vert.metalbin
var metalFullscreenVert []byte

//go:embed build/bloom_downsample.frag.metalbin
var metalBloomDownsample []byte

//go:embed build/bloom_upsample.frag.metalbin
var metalBloomUpsample []byte

//go:embed build/bloom_composite.frag.metalbin
var metalBloomComposite []byte

//go:embed build/tonemap.frag.metalbin
var metalToneMap []byte

//go:embed build/fxaa.frag.metalbin
var metalFXAA []byte

//go:embed build/halftone.frag.metalbin
var metalHalftone []byte

var metalVariants = []struct{ spirv, metal []byte }{
	{FullscreenVert, metalFullscreenVert},
	{BloomDownsample, metalBloomDownsample},
	{BloomUpsample, metalBloomUpsample},
	{BloomComposite, metalBloomComposite},
	{ToneMap, metalToneMap},
	{FXAA, metalFXAA},
	{Halftone, metalHalftone},
	{SceneCull, metalSceneCull},
	{SceneSkin, metalSceneSkin},
	{SceneDraw, metalSceneDraw},
	{SceneShadowVert, metalSceneShadowVert},
	{SceneDebugIDVert, metalSceneDebugIDVert},
	{SceneDebugNormal, metalSceneDebugNormal},
	{SceneDebugDepth, metalSceneDebugDepth},
	{SceneDebugPosition, metalSceneDebugPosition},
	{SceneDebugObject, metalSceneDebugObject},
	{SceneDebugTriangle, metalSceneDebugTriangle},
	{BasicFragment, metalBasicFragment},
	{BlinnPhongFragment, metalBlinnPhongFragment},
	{PBRFragment, metalPBRFragment},
	{OverlayVert, metalOverlayVert},
	{OverlayFrag, metalOverlayFrag},
	{EnvPrefilter, metalEnvPrefilter},
	{EnvBRDF, metalEnvBRDF},
	{EnvBackground, metalEnvBackground},
	{FogInject, metalFogInject},
	{FogIntegrate, metalFogIntegrate},
	{FogBackground, metalFogBackground},
	{ParticleUpdate, metalParticleUpdate},
	{ParticleSortKeys, metalParticleSortKeys},
	{ParticleSortStep, metalParticleSortStep},
	{ParticleDraw, metalParticleDraw},
	{ParticleBasicFragment, metalParticleBasicFragment},
}
