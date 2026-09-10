package shaders

import (
	"bytes"
	_ "embed"
)

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

//go:embed build/scene_shadow.frag.metalbin
var metalSceneShadowFrag []byte

//go:embed build/fullscreen.vert.metalbin
var metalFullscreenVert []byte

//go:embed build/gbuffer_debug.frag.metalbin
var metalGBufferDebug []byte

//go:embed build/scene_basic.frag.metalbin
var metalBasicForward []byte

//go:embed build/scene_lit.frag.metalbin
var metalBlinnPhongForward []byte

//go:embed build/scene_forward_pbr.frag.metalbin
var metalPBRForward []byte

//go:embed build/scene_deferred_pbr.frag.metalbin
var metalPBRDeferred []byte

//go:embed build/scene_lighting_pbr.frag.metalbin
var metalPBRLighting []byte

//go:embed build/overlay.vert.metalbin
var metalOverlayVert []byte

//go:embed build/overlay.frag.metalbin
var metalOverlayFrag []byte

//go:embed build/particle_update.comp.metalbin
var metalParticleUpdate []byte

//go:embed build/particle_draw.vert.metalbin
var metalParticleDraw []byte

//go:embed build/particle_basic.frag.metalbin
var metalParticleBasicForward []byte

var metalVariants = []struct{ spirv, metal []byte }{
	{SceneCull, metalSceneCull},
	{SceneSkin, metalSceneSkin},
	{SceneDraw, metalSceneDraw},
	{SceneShadowVert, metalSceneShadowVert},
	{SceneShadowFrag, metalSceneShadowFrag},
	{FullscreenVert, metalFullscreenVert},
	{GBufferDebug, metalGBufferDebug},
	{BasicForward, metalBasicForward},
	{BlinnPhongForward, metalBlinnPhongForward},
	{PBRForward, metalPBRForward},
	{PBRDeferred, metalPBRDeferred},
	{PBRLighting, metalPBRLighting},
	{OverlayVert, metalOverlayVert},
	{OverlayFrag, metalOverlayFrag},
	{ParticleUpdate, metalParticleUpdate},
	{ParticleDraw, metalParticleDraw},
	{ParticleBasicForward, metalParticleBasicForward},
}
