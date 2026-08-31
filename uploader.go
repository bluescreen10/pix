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
