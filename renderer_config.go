package pix

// PowerPreference hints which GPU to select when the backend/system exposes a
// choice (e.g. integrated vs discrete). Honored only when the backend supports it.
type PowerPreference uint8

const (
	PowerDefault PowerPreference = iota
	PowerLowPower
	PowerHighPerformance
)

// RendererConfig configures a Renderer at construction. It carries only device/
// target concerns; per-frame settings like the clear color have their own accessors
// (SetClearColor). The zero value is a headless renderer with no target yet — set
// one via SetRenderTarget, or provide Width/Height for an internally-owned one.
type RendererConfig struct {
	// Window is the platform window to present to; nil for headless. Its concrete
	// fields are platform-specific (see window_<os>.go — e.g. NSWindow on macOS,
	// HWND/HInstance on Windows).
	Window *Window

	// Width, Height size the internal offscreen target when Window is nil, and act
	// as a swapchain extent hint when a Window is set.
	Width, Height uint32

	// Backend selects a registered backend by name (e.g. "vulkan"); "" picks the
	// highest-priority registered backend.
	Backend string

	// Power hints GPU device selection (honored when the backend supports it).
	Power PowerPreference
}
