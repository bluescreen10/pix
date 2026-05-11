package pix

import (
	"fmt"
	"os"

	"github.com/bluescreen10/dawn-go/wgpu"
)

type wgpuRuntime struct {
	Adapter *wgpu.Adapter
	Config  wgpu.SurfaceConfiguration
	Device  *wgpu.Device
	Format  wgpu.TextureFormat
	Queue   *wgpu.Queue
	Surface *wgpu.Surface

	Features map[wgpu.FeatureName]bool
}

var forceFallbackAdapter = os.Getenv("WGPU_FORCE_FALLBACK_ADAPTER") == "1"

func (w *wgpuRuntime) init(width, height uint32, descriptor wgpu.SurfaceDescriptor) error {
	instance := wgpu.CreateInstance(nil)
	defer instance.Release()

	w.Surface = instance.CreateSurface(descriptor)

	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{
		ForceFallbackAdapter: forceFallbackAdapter,
		CompatibleSurface:    w.Surface,
	})

	if err != nil {
		return err
	}
	w.Adapter = adapter

	w.Features = make(map[wgpu.FeatureName]bool)
	for _, f := range adapter.GetFeatures() {
		w.Features[f] = true
	}

	var requiredFeatures []wgpu.FeatureName

	// Enable timestamp query feature if supported
	if w.Features[wgpu.FeatureNameTimestampQuery] {
		requiredFeatures = append(requiredFeatures, wgpu.FeatureNameTimestampQuery)
	}

	device := w.Adapter.RequestDevice(&wgpu.DeviceDescriptor{
		RequiredFeatures: requiredFeatures,
		UncapturedErrorCallback: wgpu.UncapturedErrorCallback(func(device *wgpu.Device, typ wgpu.ErrorType, message string) {
			panic(fmt.Sprintf("(%s): %s", typ, message))
		}),
	})

	if err != nil {
		return err
	}

	w.Device = device
	w.Queue = w.Device.GetQueue()

	caps, err := w.Surface.GetCapabilities(w.Adapter)
	if err != nil {
		return err
	}
	w.Format = caps.Formats[0]

	w.Config = wgpu.SurfaceConfiguration{
		Usage:       wgpu.TextureUsageRenderAttachment,
		Format:      w.Format,
		Width:       width,
		Height:      height,
		PresentMode: wgpu.PresentModeFifo,
		AlphaMode:   caps.AlphaModes[0],
		Device:      w.Device,
	}

	w.Surface.Configure(w.Config)

	return nil
}

func (r *wgpuRuntime) Destroy() {
	r.Surface.Release()
	r.Queue.Release()
	r.Device.Release()
	r.Adapter.Release()

	r.Surface = nil
	r.Queue = nil
	r.Device = nil
	r.Adapter = nil
	r.Config = wgpu.SurfaceConfiguration{}
	r.Features = make(map[wgpu.FeatureName]bool)
}
