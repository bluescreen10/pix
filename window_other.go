//go:build !darwin

package pix

// Window is a placeholder on platforms without a wired windowing path. Add the
// platform handles (e.g. HWND/HInstance on Windows, connection/window on xcb) here.
type Window struct{}

func (r *Renderer) attachWindow(w *Window, width, height uint32) {
	panic("pix: window presentation is not supported on this platform yet")
}
