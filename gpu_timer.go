package pix

import (
	"encoding/binary"

	"github.com/bluescreen10/dawn-go/wgpu"
)

// gpuTimer measures the GPU execution time of the main color pass via a pair of
// timestamp queries attached to that pass. Readback is asynchronous — the map
// callback fires a few frames later — so the reported time lags slightly and is
// smoothed into an EMA.
//
// Scope: the color pass only. (Cross-pass timestamps aren't usable here: on
// Dawn/Metal only the first pass writing a shared query set records, and
// encoder.WriteTimestamp is a Dawn unsafe API.)
//
// REQUIRES the dawn-go BeginRenderPass timestampWrites fix — without it,
// attaching TimestampWrites to a render pass segfaults (see the bug report).
type gpuTimer struct {
	r           *Renderer
	querySet    wgpu.QuerySet
	resolve     *wgpu.Buffer // QueryResolve | CopySrc
	readbackBuf *wgpu.Buffer // MapRead | CopyDst
	pending     bool         // a map is in flight; don't overwrite readbackBuf yet
	copied      bool         // a resolve+copy was encoded this frame; map after submit
}

func newGPUTimer(r *Renderer) *gpuTimer {
	dev := r.gpu.Device()
	return &gpuTimer{
		r: r,
		querySet: dev.CreateQuerySet(wgpu.QuerySetDescriptor{
			Label: "GPU Timer",
			Type:  wgpu.QueryTypeTimestamp,
			Count: 2,
		}),
		resolve: dev.CreateBuffer(wgpu.BufferDescriptor{
			Label: "Timestamp Resolve",
			Size:  16,
			Usage: wgpu.BufferUsageQueryResolve | wgpu.BufferUsageCopySrc,
		}),
		readbackBuf: dev.CreateBuffer(wgpu.BufferDescriptor{
			Label: "Timestamp Readback",
			Size:  16,
			Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst,
		}),
	}
}

// passWrites returns the timestamp writes to attach to the color render pass:
// query 0 at the pass start, query 1 at the pass end.
func (t *gpuTimer) passWrites() *wgpu.PassTimestampWrites {
	return &wgpu.PassTimestampWrites{
		QuerySet:                  &t.querySet,
		BeginningOfPassWriteIndex: 0,
		EndOfPassWriteIndex:       1,
	}
}

// end resolves the queries and (unless a readback is in flight) copies them to
// the mappable buffer. Call after the color pass has ended, before finishing.
func (t *gpuTimer) end(enc *wgpu.CommandEncoder) {
	enc.ResolveQuerySet(&t.querySet, 0, 2, t.resolve, 0)
	if !t.pending {
		enc.CopyBufferToBuffer(t.resolve, 0, t.readbackBuf, 0, 16)
		t.copied = true
	}
}

// readback maps the copied timestamps (call after submit). The async callback
// records the elapsed GPU time; WebGPU timestamps are in nanoseconds.
func (t *gpuTimer) readback() {
	if !t.copied {
		return
	}
	t.copied = false
	t.pending = true
	t.readbackBuf.MapAsync(wgpu.MapModeRead, 0, 16, func(status wgpu.MapAsyncStatus, _ string) {
		if status == wgpu.MapAsyncStatusSuccess {
			b := t.readbackBuf.GetConstMappedRange(0, 16)
			t0 := binary.LittleEndian.Uint64(b[0:8])
			t1 := binary.LittleEndian.Uint64(b[8:16])
			if t1 > t0 {
				t.r.Stats.AddGPUTime(float64(t1-t0) / 1e9) // ns -> seconds
			}
			t.readbackBuf.Unmap()
		}
		t.pending = false
	})
}

func (t *gpuTimer) Destroy() {
	t.querySet.Destroy()
	t.resolve.Destroy()
	t.readbackBuf.Destroy()
}
