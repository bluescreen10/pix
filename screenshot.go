package pix

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
