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
	// size is the resolution the map was created at, so a settings change can be
	// noticed and the map reallocated.
	size uint32
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
	if depthRange <= 0 {
		s.ndcBias = 0
		return
	}
	texel := 2 * radius / float32(s.size)
	s.ndcBias = (texel*1.5 + bias) / depthRange
}

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

// requestedSize is the resolution to allocate for a light, honouring its setting and
// falling back to the default when it asks for none.
func requestedSize(l scenes.LightPacket) uint32 {
	if l.ShadowSize == 0 {
		return scenes.DefaultShadowSize
	}
	return l.ShadowSize
}

// ensureOrtho gives a directional light its orthographic camera. The renderer creates
// it, not the light: which projection a shadow needs follows from how the renderer
// intends to render it, and the fit is recomputed from the view every frame anyway.
func (s *shadowResource) ensureOrtho() {
	if s.cam == nil {
		s.cam = cameras.NewOrthographicCamera(-10, 10, -10, 10, 0.1, 100)
	}
}

// ensurePerspective gives a spot light its camera, matching the cone.
func (s *shadowResource) ensurePerspective(angle, rng float32) {
	if s.cam == nil {
		s.cam = cameras.NewPerspectiveCamera(glm.ToDegrees(2*angle), 1, 0.05, rng)
	}
}

// ensureMap allocates the depth map, or reallocates it when the requested resolution
// changed. Point lights use ensureFaceMaps instead.
func (s *shadowResource) ensureMap(store *textures.Store, size uint32) {
	if s.m.IsValid() && s.size == size {
		return
	}
	if s.m.IsValid() {
		s.m.Release()
	}
	s.size = size
	s.m = store.CreateDepthTarget(size, size)
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
	if s.faces[0].m.IsValid() && s.size == size {
		return
	}
	s.size = size
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
	if len(s.faces) > 0 {
		v.Camera, v.Map = s.faces[0].cam, s.faces[0].m
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
