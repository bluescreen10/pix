// A scene's GPU light table: the flat, fixed-size buffer the lit shaders read,
// re-derived each frame from the scene's light objects (see lights.go for those).
package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
)

// Light-count limits (mirror scene_lit.frag).
const (
	MaxDirLights   = 4
	MaxPointLights = 16
	MaxSpotLights  = 8
)

// noShadowMap is the shadowMap sentinel: a directional light that casts no shadow
// (or whose map isn't allocated) stores this, and the lit shaders skip sampling.
const noShadowMap uint32 = 0xFFFFFFFF

type gpuDirLight struct {
	dir        [4]float32 // xyz = travel direction; w unused
	color      [4]float32 // rgb; w = intensity
	shadowVP   glm.Mat4f  // world → light clip (same matrix the depth pass rendered with)
	shadowMap  uint32     // bindless heap index of the depth map, or noShadowMap
	shadowBias float32    // depth-compare bias, in this camera's normalized depth units
	pad0, pad1 uint32
}

type gpuPointLight struct {
	pos        [4]float32   // xyz world; w = range
	color      [4]float32   // rgb; w = intensity
	shadowVP   [6]glm.Mat4f // per cube face: world → face clip
	shadowMap  [6]uint32    // per cube face: depth map heap index, or noShadowMap
	shadowBias float32      // depth-compare bias, in the face cameras' normalized depth units
	pad0       uint32
}

type gpuSpotLight struct {
	pos        [4]float32 // xyz world; w = range
	dir        [4]float32 // xyz cone axis (travel); w = cosOuter (outer cutoff)
	color      [4]float32 // rgb; w = intensity
	shadowVP   glm.Mat4f  // world → light clip (same matrix the depth pass rendered with)
	cosInner   float32    // inner cutoff cos (smooth edge between inner and outer)
	shadowMap  uint32     // bindless heap index of the depth map, or noShadowMap
	shadowBias float32    // depth-compare bias, in this camera's normalized depth units
	pad0       uint32
}

// gpuLights is the whole light table (scalar; matches LightBuf in scene_lit.frag).
type gpuLights struct {
	ambient [4]float32
	// fogColor is rgb + the fog mode in w; fogParams is (near, far, density, _).
	// Fog rides in the light table rather than in each root because both the forward
	// and the deferred lighting passes already carry the table, and neither push
	// constant has to grow.
	fogColor  [4]float32
	fogParams [4]float32
	numDir    uint32
	numPoint  uint32
	numSpot   uint32
	pad0      uint32
	dirs      [MaxDirLights]gpuDirLight
	points    [MaxPointLights]gpuPointLight
	spots     [MaxSpotLights]gpuSpotLight
}

var lightsSize = uint64(unsafe.Sizeof(gpuLights{}))

// Lights is the scene light table: ambient + directional + point lights in one
// fixed-size BDA buffer the lit fragment shader reads through the draw root.
type Lights struct {
	backend gpu.Backend
	data    gpuLights
	buf     gpu.Buffer
	dirty   bool
}

// NewLights creates the table. ambient defaults to a low neutral fill so an
// unlit-looking scene still shows geometry; call SetAmbient to change it.
//
// MemoryHost (not Device): a shadow-casting light's shadowVP is refit from the
// current scene bounds every frame (see Renderer.prepareShadows), so any moving
// or animated geometry — a SkinnedMesh, say — makes the table "dirty" essentially
// every frame, not just on user edits. Staging that through the shared uploader
// would force a real GPU submit+wait every frame just for a few KB of light data;
// a direct host write (like worldBuf/drawableBuf already do for the same reason)
// costs a memcpy instead.
func NewLights(b gpu.Backend) *Lights {
	l := &Lights{backend: b}
	l.data.ambient = [4]float32{0.08, 0.08, 0.08, 0}
	l.buf = b.Alloc(lightsSize, gpu.MemoryHost, "Lights")
	l.dirty = true
	return l
}

// rebuild derives the flat GPU light table from the scene's light objects + ambient.
// Called every frame (the objects — and each casting light's fitted shadow camera —
// are mutable), but it only marks the buffer dirty when the derived table actually
// changed, so a static scene re-uploads nothing. Lights past the fixed caps
// (MaxDirLights/MaxPointLights/MaxSpotLights) are dropped.
func (l *Lights) rebuild(ambient glm.Vec3f, fog Fog, dirs []*DirectionalLight, points []*PointLight, spots []*SpotLight) {
	var next gpuLights
	next.ambient = [4]float32{ambient[0], ambient[1], ambient[2], 0}
	fs := stateOf(fog)
	next.fogColor = [4]float32{fs.color[0], fs.color[1], fs.color[2], float32(fs.mode)}
	next.fogParams = [4]float32{fs.near, fs.far, fs.density, 0}
	for _, d := range dirs {
		if next.numDir >= MaxDirLights {
			break
		}
		dir := d.Direction.Normalize()
		gl := gpuDirLight{
			dir:       [4]float32{dir[0], dir[1], dir[2], 0},
			color:     [4]float32{d.Color[0], d.Color[1], d.Color[2], d.Intensity},
			shadowMap: noShadowMap,
		}
		// A casting light with an allocated map contributes its view-projection (the
		// un-flipped matrix the depth pass used) and heap index for shader sampling.
		if d.shadow != nil && d.shadow.Map.Valid() {
			gl.shadowVP = d.shadow.Camera.ViewProjection()
			gl.shadowMap = d.shadow.Map.Index()
			gl.shadowBias = d.shadow.ndcBias
		}
		next.dirs[next.numDir] = gl
		next.numDir++
	}
	for _, p := range points {
		if next.numPoint >= MaxPointLights {
			break
		}
		gp := gpuPointLight{
			pos:   [4]float32{p.Position[0], p.Position[1], p.Position[2], p.Range},
			color: [4]float32{p.Color[0], p.Color[1], p.Color[2], p.Intensity},
		}
		for f := range gp.shadowMap {
			gp.shadowMap[f] = noShadowMap
		}
		if s := p.shadow; s != nil {
			gp.shadowBias = s.ndcBias
			for f := range s.faces {
				if s.faces[f].m.Valid() {
					gp.shadowVP[f] = s.faces[f].cam.ViewProjection()
					gp.shadowMap[f] = s.faces[f].m.Index()
				}
			}
		}
		next.points[next.numPoint] = gp
		next.numPoint++
	}
	for _, sp := range spots {
		if next.numSpot >= MaxSpotLights {
			break
		}
		dir := sp.Direction.Normalize()
		cosOuter := cos32(sp.Angle)
		cosInner := cos32(sp.Angle * (1 - glm.Clamp(sp.Penumbra, 0, 1)))
		gs := gpuSpotLight{
			pos:       [4]float32{sp.Position[0], sp.Position[1], sp.Position[2], sp.Range},
			dir:       [4]float32{dir[0], dir[1], dir[2], cosOuter},
			color:     [4]float32{sp.Color[0], sp.Color[1], sp.Color[2], sp.Intensity},
			cosInner:  cosInner,
			shadowMap: noShadowMap,
		}
		if sp.shadow != nil && sp.shadow.Map.Valid() {
			gs.shadowVP = sp.shadow.Camera.ViewProjection()
			gs.shadowMap = sp.shadow.Map.Index()
			gs.shadowBias = sp.shadow.ndcBias
		}
		next.spots[next.numSpot] = gs
		next.numSpot++
	}
	// gpuLights is comparable (only fixed arrays of floats/uints), so a value compare
	// detects any change — light edits, added/removed lights, or a moved shadow camera.
	if next != l.data {
		l.data = next
		l.dirty = true
	}
}

// Addr returns the table's device address.
func (l *Lights) Addr() uint64 { return l.buf.Addr }

// Sync writes the table directly (MemoryHost, no staging/uploader) when it
// changed since the last call.
func (l *Lights) Sync() {
	if !l.dirty {
		return
	}
	writeAt(l.buf, 0, unsafe.Slice((*byte)(unsafe.Pointer(&l.data)), lightsSize))
	l.dirty = false
}

// Destroy releases the table buffer.
func (l *Lights) Destroy() {
	if l.buf.Valid() {
		l.backend.Free(l.buf)
		l.buf = gpu.Buffer{}
	}
}
