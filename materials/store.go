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
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/shaders"
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
	sh.Vertex = shaders.ForBackend(s.backend, sh.Vertex)
	sh.Forward = shaders.ForBackend(s.backend, sh.Forward)
	sh.Deferred = shaders.ForBackend(s.backend, sh.Deferred)
	sh.Lighting = shaders.ForBackend(s.backend, sh.Lighting)
	// Fast path. Every built-in constructor hands us the same //go:embed slices on
	// every call, so matching slice headers settle it outright — worth a special case
	// because the slow path below hashes ~19KB of SPIR-V, which measured as ~99% of
	// the cost of creating a material (and a glTF scene creates hundreds).
	for _, p := range s.pools {
		if sameShaderData(p.sh, sh) {
			return p
		}
	}
	// Slow path: a caller holding its own copy of SPIR-V that some pool already has.
	// The hash covers every stage, so it settles pool identity on its own — matching on
	// Forward alone would silently hand a caller another Shader's Deferred/Lighting
	// (routing it through the G-buffer against a record layout it never asked for, or
	// losing its deferred path, depending on creation order), and confirming a 32-bit
	// match by comparing every byte of every stage is what this width replaces.
	h := hashShader(sh)
	for _, p := range s.pools {
		if p.hash == h {
			return p
		}
	}
	p := newPool(s.backend, sh, label, uint32(len(s.pools)))
	s.pools = append(s.pools, p)
	return p
}

// PoolAt returns the pool with the given index, or nil if there is none. It is the
// reverse of Pool.Index: a material reference that travels as plain data carries the
// index, and whoever receives it resolves the pool through here.
func (s *Store) PoolAt(index uint32) *Pool {
	if index >= uint32(len(s.pools)) {
		return nil
	}
	return s.pools[index]
}

// Pools is the number of pools the store holds. Indices are dense in [0, Pools), so a
// consumer keeping per-pool state can size an array from it.
func (s *Store) Pools() int {
	return len(s.pools)
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
