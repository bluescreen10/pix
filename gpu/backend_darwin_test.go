//go:build darwin

package gpu_test

// Register the Vulkan backend for the RHI conformance tests on macOS — the only
// platform where the backend currently builds and runs (KosmicKrisp). On other
// platforms nothing registers, so the suite skips (see testBackend). Add other
// backends' blank imports here, build-tagged to where they build, as they land.
import _ "github.com/bluescreen10/pix/gpu/vulkan"
