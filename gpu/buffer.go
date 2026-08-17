package gpu

import "github.com/bluescreen10/dawn-go/wgpu"

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

// Buffer is an opaque handle to a GPU buffer. It hides the graphics API so the
// engine can hold buffers without knowing about wgpu.
type Buffer struct {
	raw   *wgpu.Buffer
	size  uint64
	usage BufferUsage
}

// Size returns the buffer's capacity in bytes.
func (b *Buffer) Size() uint64 { return b.size }

// Usage returns the buffer's usage flags.
func (b *Buffer) Usage() BufferUsage { return b.usage }

// Raw exposes the underlying wgpu buffer. Transitional: call sites that still
// build bind groups or set vertex/index buffers with wgpu need this until those
// paths also move into the gpu package. New code should avoid it.
func (b *Buffer) Raw() *wgpu.Buffer { return b.raw }

// Destroy frees the GPU allocation.
func (b *Buffer) Destroy() {
	if b.raw != nil {
		b.raw.Destroy()
		b.raw = nil
	}
}

// BufferDescriptor describes a buffer to allocate.
type BufferDescriptor struct {
	Label string
	Size  uint64
	Usage BufferUsage
}

// CreateBuffer allocates an uninitialized buffer.
func (i *Instance) CreateBuffer(desc BufferDescriptor) *Buffer {
	raw := i.device.CreateBuffer(wgpu.BufferDescriptor{
		Label: desc.Label,
		Size:  desc.Size,
		Usage: desc.Usage,
	})
	return &Buffer{raw: raw, size: desc.Size, usage: desc.Usage}
}

// BufferInitDescriptor describes a buffer allocated with initial contents.
type BufferInitDescriptor struct {
	Label    string
	Contents []byte
	Usage    BufferUsage
}

// CreateBufferInit allocates a buffer and uploads initial contents.
func (i *Instance) CreateBufferInit(desc BufferInitDescriptor) *Buffer {
	raw := i.device.CreateBufferInit(wgpu.BufferInitDescriptor{
		Label:    desc.Label,
		Contents: desc.Contents,
		Usage:    desc.Usage,
	})
	return &Buffer{raw: raw, size: uint64(len(desc.Contents)), usage: desc.Usage}
}

// Write uploads data into the buffer at the given byte offset via the queue.
func (i *Instance) Write(b *Buffer, offset uint64, data []byte) {
	i.queue.WriteBuffer(b.raw, offset, data)
}
