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
)

type gpuDirLight struct {
	dir   [4]float32 // xyz = travel direction; w unused
	color [4]float32 // rgb; w = intensity
}

type gpuPointLight struct {
	pos   [4]float32 // xyz world; w = range
	color [4]float32 // rgb; w = intensity
}

// gpuLights is the whole light table (scalar; matches LightBuf in scene_lit.frag).
type gpuLights struct {
	ambient    [4]float32
	numDir     uint32
	numPoint   uint32
	pad0, pad1 uint32
	dirs       [MaxDirLights]gpuDirLight
	points     [MaxPointLights]gpuPointLight
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
func NewLights(b gpu.Backend) *Lights {
	l := &Lights{backend: b}
	l.data.ambient = [4]float32{0.08, 0.08, 0.08, 0}
	l.buf = b.Alloc(lightsSize, gpu.MemoryDevice, "Lights")
	l.dirty = true
	return l
}

// SetAmbient sets the constant ambient term.
func (l *Lights) SetAmbient(c glm.Vec3f) {
	l.data.ambient = [4]float32{c[0], c[1], c[2], 0}
	l.dirty = true
}

// AddDirectional adds a directional light. dir is the direction the light travels
// (e.g. {0,-1,0} for a downward sun); color is linear RGB, intensity scales it.
func (l *Lights) AddDirectional(dir, color glm.Vec3f, intensity float32) {
	if l.data.numDir >= MaxDirLights {
		return
	}
	d := dir.Normalize()
	l.data.dirs[l.data.numDir] = gpuDirLight{
		dir:   [4]float32{d[0], d[1], d[2], 0},
		color: [4]float32{color[0], color[1], color[2], intensity},
	}
	l.data.numDir++
	l.dirty = true
}

// AddPoint adds a point light at pos with a linear falloff to zero at rng.
func (l *Lights) AddPoint(pos, color glm.Vec3f, intensity, rng float32) {
	if l.data.numPoint >= MaxPointLights {
		return
	}
	l.data.points[l.data.numPoint] = gpuPointLight{
		pos:   [4]float32{pos[0], pos[1], pos[2], rng},
		color: [4]float32{color[0], color[1], color[2], intensity},
	}
	l.data.numPoint++
	l.dirty = true
}

// Clear removes all lights (keeps ambient).
func (l *Lights) Clear() {
	l.data.numDir, l.data.numPoint = 0, 0
	l.dirty = true
}

// Addr returns the table's device address.
func (l *Lights) Addr() uint64 { return l.buf.Addr }

// Sync uploads the table through the uploader when it changed. The buffer is
// MemoryDevice, so the write is staged and copied (the caller flushes).
func (l *Lights) Sync(u *uploader) {
	if !l.dirty {
		return
	}
	u.copy(l.buf, 0, unsafe.Slice((*byte)(unsafe.Pointer(&l.data)), lightsSize))
	l.dirty = false
}

// Destroy releases the table buffer.
func (l *Lights) Destroy() {
	if l.buf.Valid() {
		l.backend.Free(l.buf)
		l.buf = gpu.Buffer{}
	}
}
