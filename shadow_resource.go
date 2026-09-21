// Renderer-owned shadow resources: the cameras and depth maps a casting light needs,
// cached per light identity. The light itself carries only settings (see LightShadow in
// lights.go) — fitting a shadow camera depends on the view being rendered and on caster
// bounds the renderer tracks, so it was never something a producer could own.
package pix

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
	"github.com/bluescreen10/pix/textures"
)

// defaultLocalShadowBias is the baseline depth-compare bias, in normalized depth units,
// for lights whose shadow camera is perspective and bounded by the light's range (spot,
// point). Their depth range is local to the light, so a constant behaves consistently —
// unlike a directional light's orthographic range, which spans the scene and must have
// its bias derived from the fit (see fitDirectionalShadow).
const defaultLocalShadowBias float32 = 0.0015

// pointFace is one cube face of a point light's shadow: a perspective camera aimed
// down a ±axis and the depth map it renders into.
type pointFace struct {
	cam Camera
	m   textures.Texture
}

// shadowResource is one casting light's depth-map state. Directional and spot lights
// use cam/m; point lights render six cube faces instead and use faces.
type shadowResource struct {
	cam Camera
	m   textures.Texture

	// ndcBias is the light's world-space bias plus a term derived from the map's texel
	// footprint, converted into the shadow camera's normalized depth units, which is
	// what the comparison actually needs. Recomputed whenever the fit changes: an
	// orthographic shadow camera's depth range spans the scene, so a constant in NDC is
	// a wildly different world distance from one scene to the next.
	ndcBias float32
	// fit carries the angle-dependent scales an orthographic fit leaves to the shader.
	// Zero for the warps, which bias by their own rules and want no normal offset.
	fit orthoFit
	// cascades are the fitted slices when the algorithm is ShadowCascaded, innermost
	// first, sharing one map laid out side by side. Empty for every other algorithm,
	// which fits cam alone.
	cascades []cascadeLevel
	// width and height are the resolution the map was created at, so a settings change
	// can be noticed and the map reallocated. They differ from each other for a cascade
	// atlas, which is one square per slice laid out along the width.
	width, height uint32
	// faces is the six per-face camera + map pairs of a point light; nil otherwise.
	faces []pointFace
	// seen marks the resource as referenced by the current frame's light table, so
	// resources for lights that went away can be retired.
	seen bool
}

// updateOrthoBias recomputes ndcBias for an orthographic (directional) shadow camera
// whose fitted box is radius wide and whose depth range is depthRange, both in world
// units. Acne scales with how much world space a single shadow texel covers, so that
// is the natural unit for the derived term; the light's own bias adds an explicit
// world-space offset.
//
// The division is the whole point: an orthographic shadow camera's depth range spans
// the scene, so a bias expressed directly in normalized depth silently becomes a
// wildly different world distance from one scene to the next.
func (s *shadowResource) updateOrthoBias(radius, depthRange, bias float32) {
	s.fit = orthoBias(radius, depthRange, s.width, bias)
	s.ndcBias = s.fit.bias
}

// orthoFit is what an orthographic fit hands back: the constant part of its depth bias,
// already in normalized units, plus the two scales the lit shader needs to finish the
// job per fragment.
//
// The constant is deliberately small. Most of the bias a surface needs depends on how it
// is turned relative to the light, which the fit cannot know — see ShadowOffsets in
// lighting.glsl.
type orthoFit struct {
	bias float32
	// texel is how much world space one shadow texel covers, which is the unit the
	// angle-dependent offsets are measured in.
	texel float32
	// depthScale converts a world-space depth offset into this camera's normalized
	// depth, so the shader can add a world-sized slope bias to a normalized comparison.
	depthScale float32
}

// orthoBias derives the above from a fitted box, split out from the resource so a
// cascade can ask for one per slice: each covers a different range and so implies a
// different texel footprint.
func orthoBias(radius, depthRange float32, size uint32, bias float32) orthoFit {
	if depthRange <= 0 || size == 0 {
		return orthoFit{}
	}
	texel := 2 * radius / float32(size)
	return orthoFit{
		bias:       (texel*orthoConstantTexels + bias) / depthRange,
		texel:      texel,
		depthScale: 1 / depthRange,
	}
}

// orthoConstantTexels is the angle-independent part of an orthographic fit's bias, in
// texels. It only has to cover the filter's own footprint; the part that varies with the
// surface is applied per fragment, where the normal is known.
const orthoConstantTexels float32 = 0.5

// updateLocalBias recomputes ndcBias for a perspective shadow camera bounded by a
// light's range (spot, point). Perspective depth is non-linear, so there is no exact
// world→normalized factor — scaling the light's bias by the range is the
// approximation, chosen so bias means something for every light type rather than being
// ignored on these two.
func (s *shadowResource) updateLocalBias(rng, bias float32) {
	s.ndcBias = defaultLocalShadowBias
	if rng > 0 {
		s.ndcBias += bias / rng
	}
}

// size is the resolution of one square a fit renders into: the whole map for a single
// fit, or one cascade's slot in the atlas. Both layouts keep the map's height as that
// square's side, so this needs no special case.
func (s *shadowResource) size() uint32 {
	return s.height
}

// ensureCascades makes sure count cascade levels exist, each with its own orthographic
// camera. The cameras persist between frames so a fit only has to re-aim them.
func (s *shadowResource) ensureCascades(count int) {
	if len(s.cascades) > count {
		s.cascades = s.cascades[:count]
	}
	for len(s.cascades) < count {
		s.cascades = append(s.cascades, cascadeLevel{
			cam: cameras.NewOrthographicCamera(-10, 10, -10, 10, 0.1, 100),
		})
	}
}

// requestedSize is the resolution to allocate for a light, honouring its setting and
// falling back to the default when it asks for none.
func requestedSize(l scenes.LightPacket) uint32 {
	if l.ShadowSize == 0 {
		return scenes.DefaultShadowSize
	}
	return l.ShadowSize
}

// maxShadowMapWidth bounds the shadow atlas so it cannot be asked for a texture wider
// than a backend will create. Cascades lay their squares out along the width, so the
// width is the requested resolution MULTIPLIED by the number of slices — four cascades
// of a 4096 map is already 16384, and one more of either overruns a limit that is 16384
// on the common backends. The RHI does not publish the limit, so this is the widely
// supported floor rather than a query; exceeding it is not a soft failure but a driver
// assertion that takes the process with it.
const maxShadowMapWidth uint32 = 16384

// cascadeAtlas is the texture size for count slices of a requested square resolution,
// laid out along the width, reduced to fit maxShadowMapWidth. Halving keeps the squares
// square, which everything downstream assumes.
func cascadeAtlas(size, count uint32) (width, height uint32) {
	for size > 1 && size*count > maxShadowMapWidth {
		size /= 2
	}
	return size * count, size
}

// orthoCamera returns this resource's orthographic camera, creating it (or replacing a
// camera of another kind, left behind by a different shadow algorithm) as needed. The
// renderer chooses the projection, not the light: which one a shadow needs follows from
// how the renderer intends to render it, and the fit is recomputed every frame anyway.
func (s *shadowResource) orthoCamera() Camera {
	if _, ok := s.cam.(*cameras.OrthographicCamera); !ok {
		s.cam = cameras.NewOrthographicCamera(-10, 10, -10, 10, 0.1, 100)
	}
	return s.cam
}

// ensurePerspective gives a spot light its camera, matching the cone.
func (s *shadowResource) ensurePerspective(angle, rng float32) {
	if s.cam == nil {
		s.cam = cameras.NewPerspectiveCamera(glm.ToDegrees(2*angle), 1, 0.05, rng)
	}
}

// ensureMap allocates the depth map, or reallocates it when the requested resolution
// changed. Point lights use ensureFaceMaps instead.
func (s *shadowResource) ensureMap(store *textures.Store, width, height uint32) {
	if s.m.IsValid() && s.width == width && s.height == height {
		return
	}
	if s.m.IsValid() {
		s.m.Release()
	}
	s.width, s.height = width, height
	s.m = store.CreateDepthTarget(width, height)
}

// ensureFaceMaps does the same for a point light's six cube faces.
func (s *shadowResource) ensureFaceMaps(store *textures.Store, size uint32) {
	if len(s.faces) != 6 {
		s.faces = make([]pointFace, 6)
	}
	for i := range s.faces {
		if s.faces[i].cam == nil {
			s.faces[i].cam = cameras.NewPerspectiveCamera(90, 1, 0.05, 1)
		}
	}
	if s.faces[0].m.IsValid() && s.width == size && s.height == size {
		return
	}
	s.width, s.height = size, size
	for i := range s.faces {
		if s.faces[i].m.IsValid() {
			s.faces[i].m.Release()
		}
		s.faces[i].m = store.CreateDepthTarget(size, size)
	}
}

// destroy releases every map this resource allocated.
func (s *shadowResource) destroy() {
	if s.m.IsValid() {
		s.m.Release()
	}
	for i := range s.faces {
		if s.faces[i].m.IsValid() {
			s.faces[i].m.Release()
		}
	}
	s.faces = nil
}

// shadowFor returns the cached resource for a light, creating it on first sight.
func (st *renderState) shadowFor(id scenes.LightID) *shadowResource {
	if s, ok := st.shadows[id]; ok {
		return s
	}
	if st.shadows == nil {
		st.shadows = make(map[scenes.LightID]*shadowResource)
	}
	s := &shadowResource{}
	st.shadows[id] = s
	return s
}

// retireUnseenShadows frees the resources of lights that stopped casting, or went away
// entirely. Without it a scene that toggles shadows on a long-lived light would hold
// every depth map it ever allocated.
func (st *renderState) retireUnseenShadows() {
	for id, s := range st.shadows {
		if s.seen {
			s.seen = false
			continue
		}
		s.destroy()
		delete(st.shadows, id)
	}
}

// ShadowView is a light's renderer-owned shadow resources, exposed for inspection.
// Camera is the view the depth pass rendered with and Map the depth texture the lit
// shaders sample; for a point light these are the first cube face, and Faces holds all
// six. Everything here is derived state, valid only until the next frame prepares it.
type ShadowView struct {
	Camera Camera
	Map    textures.Texture
	Faces  []pointFace
	// Cascades is one camera per slice when the light was fitted with ShadowCascaded,
	// innermost first, all rendering into Map side by side. Empty otherwise, and Camera
	// is then the only fit. When it is not empty Camera is its innermost slice, so a
	// caller that only wants "the shadow camera" still gets a useful one.
	Cascades []Camera
	// Splits[i] is the view distance Cascades[i] covers out to, which is where the lit
	// shader stops using it. Same length as Cascades.
	Splits []float32
}

// ShadowView returns the shadow resources the renderer holds for one light of one
// source, or nil if it has none — the light does not cast, shadows are disabled, or
// nothing has been rendered yet.
//
// This is the resource half of what LightShadow used to be. The settings half stayed on
// the light (see LightShadow); what could not stay is anything whose value depends on
// the view being rendered, which is all of this.
func (r *Renderer) ShadowView(source scenes.SourceID, light scenes.LightID) *ShadowView {
	st, ok := r.sources[source]
	if !ok {
		return nil
	}
	s, ok := st.shadows[light]
	if !ok {
		return nil
	}
	v := &ShadowView{Camera: s.cam, Map: s.m, Faces: s.faces}
	for _, c := range s.cascades {
		v.Cascades = append(v.Cascades, c.cam)
		v.Splits = append(v.Splits, c.far)
	}
	switch {
	case len(s.faces) > 0:
		v.Camera, v.Map = s.faces[0].cam, s.faces[0].m
	case len(v.Cascades) > 0:
		v.Camera = v.Cascades[0]
	}
	return v
}

// Camera and Map of one cube face, for inspecting a point light's shadow.
func (f pointFace) Camera() Camera {
	return f.cam
}

func (f pointFace) Map() textures.Texture {
	return f.m
}

// particleState is one particle system's GPU simulation state. The buffers ARE the
// simulation — nothing on the CPU mirrors them — so they are keyed by the system's
// stable id and outlive any single frame.
type particleState struct {
	// buffers ping-pong: the update kernel reads one frame's compacted survivors from
	// one and writes the next frame's into the other, since particle state persists
	// across frames (unlike culling's stateless per-frame visibility compaction).
	buffers     [2]gpu.Buffer
	current     int
	indirectBuf gpu.Buffer
	// pendingBuf holds this frame's staged newborns; the update kernel's tail range
	// reads it directly, so births reach the compacted buffer through the same atomic
	// append survivors use, without the CPU tracking the GPU's running alive count.
	pendingBuf gpu.Buffer
	ready      bool
	// epoch mirrors the packet's; a change means the system was cleared and its buffers
	// must start empty again.
	epoch uint64
	seen  bool
}

func (st *renderState) particleFor(id scenes.ParticleID) *particleState {
	if ps, ok := st.particles[id]; ok {
		return ps
	}
	if st.particles == nil {
		st.particles = make(map[scenes.ParticleID]*particleState)
	}
	ps := &particleState{}
	st.particles[id] = ps
	return ps
}

// retireUnseenParticles frees the buffers of systems that left the packet — detached,
// or destroyed. Their simulation state goes with them, which is the documented
// consequence of detaching: there is nothing on the CPU to restore it from.
func (st *renderState) retireUnseenParticles(backend gpu.Backend) {
	for id, ps := range st.particles {
		if ps.seen {
			ps.seen = false
			continue
		}
		ps.destroy(backend)
		delete(st.particles, id)
	}
}

func (ps *particleState) destroy(backend gpu.Backend) {
	for i := range ps.buffers {
		if ps.buffers[i].IsValid() {
			backend.Free(ps.buffers[i])
		}
	}
	if ps.indirectBuf.IsValid() {
		backend.Free(ps.indirectBuf)
	}
	if ps.pendingBuf.IsValid() {
		backend.Free(ps.pendingBuf)
	}
}
