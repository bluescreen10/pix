// Package materials owns material resources for pix's bindless renderer. It is
// two-level, because unlike geometry or textures a material's GPU record has no
// single layout: the shader decides it. A Store owns one Pool per material kind
// (keyed by shader), and a Pool owns every instance of that kind plus the
// device-local record buffer they serialize into.
//
// A custom material type lives outside this package: build it on a Pool of your own
// shader (Store.Pool) and implement Material — nothing here is sealed.
package materials

import (
	"github.com/bluescreen10/pix/gpu"
)

// Store owns every material Pool (the renderer owns pipelines and issues draws, never
// a Pool). Pools are byte-based and created automatically, one per material kind
// (keyed by shader): a material declares its shader, and the Pool learns the record
// size from the first instance registered — no hand-written per-type storage.
type Store struct {
	backend gpu.Backend
	pools   []*Pool
}

func NewStore(backend gpu.Backend) *Store {
	return &Store{backend: backend}
}

// Pool returns the pool for a material kind, creating it on first use. The kind is
// keyed by the shader, which fixes the record layout; the record size comes from the
// first material registered. Textures are not the pool's concern — each material
// holds its own references.
func (s *Store) Pool(sh Shader, label string) *Pool {
	// Fast path. Every built-in constructor hands us the same //go:embed slices on
	// every call, so matching slice headers settle it outright — worth a special case
	// because the slow path below hashes ~19KB of SPIR-V, which measured as ~99% of
	// the cost of creating a material (and a glTF scene creates hundreds).
	for _, p := range s.pools {
		if sameShaderData(p.sh, sh) {
			return p
		}
	}
	h := HashSPIRV(sh.Forward)
	for _, p := range s.pools {
		// The forward-shader hash fixes the record layout, but the whole Shader must
		// match before sharing a pool: a pool hands its own sh to every instance, so
		// matching on Forward alone would silently give a caller another Shader's
		// Deferred/Lighting (routing it through the G-buffer against a record layout it
		// never asked for, or losing its deferred path — depending on creation order).
		if p.forwardHash == h && sameShader(p.sh, sh) {
			return p
		}
	}
	p := newPool(s.backend, sh, label)
	s.pools = append(s.pools, p)
	return p
}

// Sync uploads every pool's changed records into device memory. Material records are
// device-local, so accessor writes only touch the material itself until this runs;
// call once per frame before recording draws. The caller flushes the uploader.
func (s *Store) Sync(u Uploader) {
	for _, p := range s.pools {
		p.Sync(u)
	}
}

// Destroy releases every pool.
func (s *Store) Destroy() {
	for _, p := range s.pools {
		p.destroy()
	}
	s.pools = nil
}
