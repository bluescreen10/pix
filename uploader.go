package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
)

// uploader batches staged uploads to MemoryDevice buffers into a single command
// list. Subsystems record copies via copy(); they never allocate staging and
// never free it. The renderer owns the lifecycle: create one with newUploader,
// hand it to each thing that needs syncing, then flush() to submit, wait for the
// GPU, and release the staging buffers.
//
// The command list is created lazily on the first copy, so a flush with nothing
// recorded is a no-op (no empty submit).
type uploader struct {
	backend gpu.Backend
	cmd     gpu.CommandBuffer
	began   bool
	staging []gpu.Buffer
}

func newUploader(backend gpu.Backend) *uploader {
	return &uploader{backend: backend}
}

// copy stages data on the host and records a device copy into dst at dstOffset.
// The staging buffer lives until flush() frees it, so data need not outlive this
// call. Empty data records nothing.
func (u *uploader) copy(dst gpu.Buffer, dstOffset uint32, data []byte) {
	if len(data) == 0 {
		return
	}
	if !u.began {
		u.cmd = u.backend.Begin()
		u.began = true
	}
	s := u.backend.Alloc(uint64(len(data)), gpu.MemoryHost, "upload-staging")
	copy(unsafe.Slice((*byte)(s.Ptr), len(data)), data)
	u.cmd.CopyBuffer(dst, s, uint64(dstOffset), 0, uint64(len(data)))
	u.staging = append(u.staging, s)
}

// scatterCopy stages every part into ONE staging buffer, then records one CopyBuffer
// per part into dst at its own offset. Unlike calling copy per part, this pays for a
// single staging allocation no matter how many parts there are — worth it when a
// subsystem has many small, non-adjacent writes into the same destination buffer (see
// materialStore.Sync). Parts may land at any offsets in any order; nothing here
// assumes they are contiguous or sorted. Empty parts records nothing.
func (u *uploader) scatterCopy(dst gpu.Buffer, parts []scatterPart) {
	var total uint64
	for _, p := range parts {
		total += uint64(len(p.data))
	}
	if total == 0 {
		return
	}
	if !u.began {
		u.cmd = u.backend.Begin()
		u.began = true
	}
	s := u.backend.Alloc(total, gpu.MemoryHost, "upload-staging")
	staging := unsafe.Slice((*byte)(s.Ptr), total)
	var srcOffset uint64
	for _, p := range parts {
		n := uint64(len(p.data))
		if n == 0 {
			continue
		}
		copy(staging[srcOffset:srcOffset+n], p.data)
		u.cmd.CopyBuffer(dst, s, uint64(p.dstOffset), srcOffset, n)
		srcOffset += n
	}
	u.staging = append(u.staging, s)
}

// scatterPart is one region of a scatterCopy: data lands at dstOffset in the
// destination buffer, wherever scatterCopy happens to place it in the shared staging
// buffer.
type scatterPart struct {
	dstOffset uint32
	data      []byte
}

// copyToTexture stages image pixels and records a buffer->texture copy into mip 0
// / layer 0, then the transition to sampled layout for reads at stage. Like copy,
// the staging buffer lives until flush() frees it. Empty pixels record nothing.
func (u *uploader) copyToTexture(tex gpu.Texture, pixels []byte, stage gpu.Stage) {
	if len(pixels) == 0 {
		return
	}
	if !u.began {
		u.cmd = u.backend.Begin()
		u.began = true
	}
	s := u.backend.Alloc(uint64(len(pixels)), gpu.MemoryHost, "upload-staging")
	copy(unsafe.Slice((*byte)(s.Ptr), len(pixels)), pixels)
	u.cmd.CopyBufferToTexture(tex, 0, 0, s, 0)
	u.cmd.PrepareSampled(tex, stage)
	u.staging = append(u.staging, s)
}

// flush submits the recorded copies, waits for completion, then frees staging.
// It is a no-op if nothing was recorded. Safe to call and reuse afterwards.
func (u *uploader) flush() {
	if !u.began {
		return
	}
	fence := u.backend.Submit(u.cmd)
	u.backend.Wait(fence)
	for _, s := range u.staging {
		u.backend.Free(s)
	}
	u.staging = u.staging[:0]
	u.began = false
}
