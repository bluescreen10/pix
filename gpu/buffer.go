package gpu

import (
	"github.com/bluescreen10/dawn-go/wgpu"
)

// BufferUsage mirrors the underlying buffer usage flags. Re-exported so callers
// depend on the gpu package rather than importing wgpu directly.
type BufferUsage = wgpu.BufferUsage

const (
	BufferUsageCopySrc  = wgpu.BufferUsageCopySrc
	BufferUsageCopyDst  = wgpu.BufferUsageCopyDst
	BufferUsageIndex    = wgpu.BufferUsageIndex
	BufferUsageVertex   = wgpu.BufferUsageVertex
	BufferUsageUniform  = wgpu.BufferUsageUniform
	BufferUsageStorage  = wgpu.BufferUsageStorage
	BufferUsageIndirect = wgpu.BufferUsageIndirect
)

// Buffer is an opaque, copyable handle to a GPU buffer — or, once the arena
// lands, to a sub-range of one. It carries no data; the backing info lives in
// the Instance's registry, keyed by this handle. The zero value is invalid.
type Buffer struct {
	idx uint32
	gen uint32
}

// Valid reports whether the handle refers to a live buffer.
func (b Buffer) Valid() bool { return b.gen != 0 }

// bufferData is the registry entry behind a Buffer handle. offset is 0 for a
// dedicated buffer; sub-allocations set it (and an arena back-reference) once
// the suballocator is in place.
type bufferData struct {
	raw    *wgpu.Buffer
	offset uint64
	size   uint64
	usage  BufferUsage
}

// BufferDescriptor describes a buffer to allocate.
type BufferDescriptor struct {
	Label string
	Size  uint64
	Usage BufferUsage
}

// CreateBuffer allocates a dedicated, uninitialized buffer and returns a handle.
func (i *Instance) CreateBuffer(desc BufferDescriptor) Buffer {
	raw := i.device.CreateBuffer(wgpu.BufferDescriptor{
		Label: desc.Label,
		Size:  desc.Size,
		Usage: desc.Usage,
	})
	idx, gen := i.buffers.Alloc(bufferData{raw: raw, offset: 0, size: desc.Size, usage: desc.Usage})
	return Buffer{idx: idx, gen: gen}
}

// BufferInitDescriptor describes a buffer allocated with initial contents.
type BufferInitDescriptor struct {
	Label    string
	Contents []byte
	Usage    BufferUsage
}

// CreateBufferInit allocates a dedicated buffer, uploads initial contents, and
// returns a handle.
func (i *Instance) CreateBufferInit(desc BufferInitDescriptor) Buffer {
	raw := i.device.CreateBufferInit(wgpu.BufferInitDescriptor{
		Label:    desc.Label,
		Contents: desc.Contents,
		Usage:    desc.Usage,
	})
	idx, gen := i.buffers.Alloc(bufferData{raw: raw, offset: 0, size: uint64(len(desc.Contents)), usage: desc.Usage})
	return Buffer{idx: idx, gen: gen}
}

// WriteBuffer uploads data into the buffer at the given byte offset (relative to
// the handle's own range). No-op if the handle is stale.
func (i *Instance) WriteBuffer(b Buffer, offset uint64, data []byte) {
	if !i.buffers.Valid(b.idx, b.gen) {
		return
	}
	e := i.buffers.Get(b.idx)
	i.queue.WriteBuffer(e.raw, e.offset+offset, data)
}

// ReleaseBuffer frees the buffer. For a dedicated buffer this destroys the GPU
// allocation; for a sub-allocation (once arenas land) it returns the range to
// the arena. No-op if the handle is stale.
func (i *Instance) ReleaseBuffer(b Buffer) {
	if !i.buffers.Valid(b.idx, b.gen) {
		return
	}
	e := i.buffers.Get(b.idx)
	e.raw.Destroy()
	// GPU handle is destroyed synchronously here, so Free recycles the slot.
	i.buffers.Free(b.idx)
}

// BufferOffset returns the handle's byte offset into its underlying buffer
// (0 for a dedicated buffer). Used to fill descriptors for manual pulling.
func (i *Instance) BufferOffset(b Buffer) uint64 {
	if !i.buffers.Valid(b.idx, b.gen) {
		return 0
	}
	return i.buffers.Get(b.idx).offset
}

// BufferSize returns the handle's size in bytes.
func (i *Instance) BufferSize(b Buffer) uint64 {
	if !i.buffers.Valid(b.idx, b.gen) {
		return 0
	}
	return i.buffers.Get(b.idx).size
}

// resolveBuffer returns the underlying wgpu buffer and the handle's range, for
// the pass/bind wrappers as they move into the gpu package. Returns nil when the
// handle is stale.
func (i *Instance) resolveBuffer(b Buffer) (raw *wgpu.Buffer, offset, size uint64) {
	if !i.buffers.Valid(b.idx, b.gen) {
		return nil, 0, 0
	}
	e := i.buffers.Get(b.idx)
	return e.raw, e.offset, e.size
}
