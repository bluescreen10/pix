package pix

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
)

// screenshot is a queued frame capture: the renderer records the copy into the frame
// it is already building, so the image is the frame the user actually saw.
//
// Capturing after the fact does not work for a windowed renderer — by then the frame
// has been presented and the swapchain image is the compositor's — which is why this
// rides the frame rather than doing its own submit the way Capture does offscreen.
type screenshot struct {
	path string
	done func(path string, err error)
}

// Screenshot queues a PNG of the next completed frame and calls done (which may be
// nil) once it has been written or has failed.
//
// An empty path writes a timestamped file in the working directory. The callback is
// invoked from Render, on the frame that produced the image, so a console command can
// report the result into its own scrollback.
//
// It relies on the frame's colour target being readable, which for a window means the
// swapchain was created with transfer-source usage. Where a driver does not allow that,
// the callback reports it rather than the renderer failing quietly.
func (r *Renderer) Screenshot(path string, done func(path string, err error)) {
	if path == "" {
		path = fmt.Sprintf("screenshot-%s.png", time.Now().Format("20060102-150405"))
	}
	r.pendingShot = &screenshot{path: path, done: done}
}

// recordScreenshot copies the frame's colour target into the readback buffer. Called
// with the frame's command buffer still open, after everything has been drawn into
// target, so the capture includes the overlay and console exactly as presented.
func (r *Renderer) recordScreenshot(cmd gpu.CommandBuffer, target gpu.Texture) {
	if r.pendingShot == nil {
		return
	}
	n := int(r.width * r.height * 4)
	if !r.readback.Valid() || len(r.pixels) < n {
		if r.readback.Valid() {
			r.backend.Free(r.readback)
		}
		r.readback = r.backend.Alloc(uint64(n), gpu.MemoryHost, "readback")
		r.pixels = make([]byte, n)
	}
	cmd.CopyTextureToBuffer(r.readback, target, 0, 0)
}

// writeScreenshot encodes and writes the captured frame. Called after the frame has
// been submitted and drained, so the readback buffer holds finished pixels.
func (r *Renderer) writeScreenshot() {
	shot := r.pendingShot
	if shot == nil {
		return
	}
	r.pendingShot = nil

	err := r.encodePNG(shot.path)
	if shot.done != nil {
		shot.done(shot.path, err)
	}
}

// encodePNG converts the readback buffer to an image and writes it.
func (r *Renderer) encodePNG(path string) error {
	n := int(r.width * r.height * 4)
	if !r.readback.Valid() || len(r.pixels) < n {
		return fmt.Errorf("no frame was captured (the target may not allow readback)")
	}
	copy(r.pixels, unsafe.Slice((*byte)(r.readback.Ptr), n))

	img := image.NewRGBA(image.Rect(0, 0, int(r.width), int(r.height)))
	copy(img.Pix, r.pixels[:n])
	// A swapchain is commonly BGRA while image.RGBA is, unsurprisingly, RGBA — without
	// this the sky comes out orange.
	if r.color == gpu.FormatBGRA8Unorm || r.color == gpu.FormatBGRA8Srgb {
		for i := 0; i+3 < len(img.Pix); i += 4 {
			img.Pix[i], img.Pix[i+2] = img.Pix[i+2], img.Pix[i]
		}
	}
	// The frame is opaque; whatever alpha the target ended up with is not meaningful
	// here, and a stray 0 would make the PNG transparent.
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xFF
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
