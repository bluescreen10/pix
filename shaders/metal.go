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

//go:embed build/particle_update.comp.metalbin
var metalParticleUpdate []byte

//go:embed build/particle_draw.vert.metalbin
var metalParticleDraw []byte

//go:embed build/particle_basic.frag.metalbin
var metalParticleBasicFragment []byte

var metalVariants = []struct{ spirv, metal []byte }{
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
	{ParticleUpdate, metalParticleUpdate},
	{ParticleDraw, metalParticleDraw},
	{ParticleBasicFragment, metalParticleBasicFragment},
}
