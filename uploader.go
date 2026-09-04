package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
)

// Staging arena tuning.
const (
	// minSize is the arena's floor: its size when it first allocates, the size of
	// an overflow chunk, and the smallest it is ever trimmed back to.
	minSize uint64 = 1 << 20 // 1 MiB

	// copyAlign is the suballocation alignment. Buffer-to-buffer copies have no
	// hardware alignment requirement; 16 bytes just keeps records off shared cache
	// lines for free. (Texture uploads don't come through here — they have
	// stricter alignment rules and their own one-shot path in textures.go.)
	copyAlign uint64 = 16

	// trimFrames is how many consecutive mostly-idle resets the arena must see
	// before shrinking: a load-time spike shouldn't pin peak memory forever, but
	// neither should a couple of quiet frames cost a realloc.
	trimFrames = 120
)

// Uploader stages uploads to MemoryDevice buffers into the frame's command buffer.
// Subsystems record copies via Copy(); they never allocate staging and never free
// it. The renderer owns one for the process: call Begin on the frame's command
// buffer, let each subsystem sync into it, then End to drop the barrier that orders
// the copies before the stages that read them.
//
// The copies ride the frame's single submit rather than getting one of their own.
// That matters because the backend currently drains the queue at Present, so a
// separate upload submit+wait would be a *second* full pipeline drain per frame,
// with the GPU idle while the CPU records the rest of the frame.
//
// Staging memory is a bump-allocated arena, not an allocation per copy. Every copy
// in a frame retires at the same instant (when that frame's submit completes),
// which is linear-allocator lifetime, not general alloc/free: Copy aligns and
// advances an offset, and Begin rewinds it. The arena keeps its memory across
// frames, so once it has grown to the high-water mark it stops allocating entirely
// — the steady state is zero allocations per frame, versus one vkAllocateMemory per
// copy before.
//
// The arena is a list of arenas only so overflow is safe. A copy that doesn't fit
// takes a new chunk rather than growing the existing one, because copies already
// recorded into the frame's command buffer still name it as their source. At the
// next Begin the arenas collapse into a single buffer sized to what the frame
// actually needed. Multi-chunk is a transient state; one buffer is the steady state.
type uploader struct {
	backend gpu.Backend
	// cmd is the frame's command buffer, bound between Begin and End; recorded
	// says whether anything was staged into it (so End can skip the barrier).
	cmd      gpu.CommandBuffer
	recorded bool

	arenas []stagingArena
	cur    int // chunk currently being filled; earlier ones are full
	idle   int // consecutive resets that left the arena mostly unused
}

// stagingArena is one host-visible buffer of the arena plus its bump offset.
type stagingArena struct {
	buf    gpu.Buffer
	offset uint64
}

// New creates an Uploader for backend.
func newUploader(backend gpu.Backend) *uploader {
	return &uploader{backend: backend}
}

// Begin binds the frame's command buffer and reclaims the arena. Copies recorded
// between Begin and End go into cmd.
//
// Reclaiming here is what keeps the arena safe without fence tracking: Begin is
// only reached after the previous frame's submit has completed (the backend drains
// the queue at Present today), so the GPU is provably finished with the staging
// memory about to be handed out again. When frames-in-flight lands, this is the one
// place that has to start retiring arenas against a fence instead.
func (u *uploader) Begin(cmd gpu.CommandBuffer) {
	u.cmd = cmd
	u.recorded = false
	u.reset()
}

// End orders this frame's staged copies before the stages that read them, and
// unbinds the command buffer. Nothing staged, no barrier.
func (u *uploader) End(cmd gpu.CommandBuffer) {
	if u.recorded {
		// What reads staged data: vertex pulling (positions/attributes/skin),
		// compute (skinning reads those same streams plus the descriptors) and
		// fragment (material records). Indirect args are host-written, never staged.
		cmd.Barrier(gpu.StageTransfer, gpu.StageVertex|gpu.StageFragment|gpu.StageCompute, 0)
	}
	u.cmd = nil
	u.recorded = false
}

// stage copies data into the arena and returns the chunk buffer plus the byte offset
// within it to copy from. The host copy happens here, so callers may reuse or free
// data as soon as stage returns.
func (u *uploader) stage(data []byte, align uint64) (gpu.Buffer, uint64) {
	n := uint64(len(data))
	for {
		if u.cur < len(u.arenas) {
			c := &u.arenas[u.cur]
			if off := alignUp(c.offset, align); off+n <= c.buf.Size {
				c.offset = off + n
				copy(unsafe.Slice((*byte)(c.buf.Ptr), int(c.buf.Size))[off:], data)
				return c.buf, off
			}
			u.cur++ // no room for this one; the next chunk starts empty
			continue
		}
		// Out of arenas: add one big enough for this copy. Existing arenas stay
		// alive — copies already recorded still reference them as their source.
		u.arenas = append(u.arenas, stagingArena{
			buf: u.backend.Alloc(max(n+align, minSize), gpu.MemoryHost, "upload-staging"),
		})
	}
}

// Copy stages data on the host and records a device copy into dst at dstOffset.
// The staging is copied immediately, so data need not outlive this call.
// Empty data records nothing.
func (u *uploader) Copy(dst gpu.Buffer, dstOffset uint32, data []byte) {
	if len(data) == 0 {
		return
	}
	if u.cmd == nil {
		panic("upload: Copy outside Begin/End")
	}
	src, srcOffset := u.stage(data, copyAlign)
	u.cmd.CopyBuffer(dst, src, uint64(dstOffset), srcOffset, uint64(len(data)))
	u.recorded = true
}

// reset rewinds the arena for the next frame and right-sizes it: arenas collapse
// into one buffer after an overflow, and a long-idle oversized arena is trimmed.
// Only safe where Begin calls it — previous submit complete — since it frees the
// buffers earlier copies referenced.
func (u *uploader) reset() {
	var used uint64
	for i := range u.arenas {
		used += u.arenas[i].offset
		u.arenas[i].offset = 0
	}
	u.cur = 0

	switch {
	case len(u.arenas) > 1:
		// Overflowed into extra arenas: collapse to one buffer sized for what that
		// frame actually needed, plus headroom, so the next one fits in a single
		// chunk and allocates nothing.
		u.reserve(used + used/2)
		u.idle = 0
	case len(u.arenas) == 1 && u.arenas[0].buf.Size > minSize && used < u.arenas[0].buf.Size/4:
		// Oversized (a load spike) and mostly unused since. Shrink, but only after
		// a long quiet stretch, so this never thrashes.
		u.idle++
		if u.idle >= trimFrames {
			u.reserve(used * 2)
			u.idle = 0
		}
	default:
		u.idle = 0
	}
}

// reserve replaces the arena with a single chunk of at least size bytes (never
// below minSize). Callers must satisfy reset's safety rule.
func (u *uploader) reserve(size uint64) {
	for i := range u.arenas {
		u.backend.Free(u.arenas[i].buf)
	}
	u.arenas = append(u.arenas[:0], stagingArena{
		buf: u.backend.Alloc(max(size, minSize), gpu.MemoryHost, "upload-staging"),
	})
	u.cur = 0
}

// ArenaCount reports how many staging chunks the arena currently holds. 1 in the
// steady state; more only right after a frame's copies overflowed a single chunk
// (see the Uploader doc comment). A diagnostic for callers that want to confirm a
// change didn't force new staging allocations — not needed for normal use.
func (u *uploader) ArenaCount() int {
	return len(u.arenas)
}

// Destroy releases the arena. The Uploader must not be used afterwards.
func (u *uploader) Destroy() {
	for i := range u.arenas {
		u.backend.Free(u.arenas[i].buf)
	}
	u.arenas = nil
	u.cur = 0
}

// alignUp rounds v up to a multiple of a, which must be a power of two.
func alignUp(v, a uint64) uint64 {
	return (v + a - 1) &^ (a - 1)
}
