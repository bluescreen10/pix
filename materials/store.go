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
	// defaultSampler is the sampler every map of a new material reads with until one is
	// set (see DefaultSampler).
	defaultSampler uint32
}

// NewStore creates a store whose materials' maps read with defaultSampler, a bindless
// sampler heap index, until each is given one of its own.
func NewStore(backend gpu.Backend, defaultSampler uint32) *Store {
	return &Store{backend: backend, defaultSampler: defaultSampler}
}

// DefaultSampler is the sampler every map of a new material reads with until one is
// set: without one a map would read through heap index 0, which need not be a sampler
// meant for colour, and could read black.
func (s *Store) DefaultSampler() uint32 {
	return s.defaultSampler
}

// Pool returns the pool for a material kind, creating it on first use. The kind is
// keyed by the shader, which fixes the record layout; the record size comes from the
// first material registered. Textures are not the pool's concern — each material
// holds its own references.
func (s *Store) Pool(sh Shader, label string) *Pool {
	sh.Vertex = shaders.ForBackend(s.backend, sh.Vertex)
	sh.Fragment = shaders.ForBackend(s.backend, sh.Fragment)
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
	// The hash covers both stages, so it settles pool identity on its own — matching on
	// the fragment stage alone would hand a caller a pool whose vertex stage it never
	// asked for. Confirming a 32-bit match by comparing every byte is what this width
	// replaces.
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
