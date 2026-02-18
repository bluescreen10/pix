package pix

import (
	"os"

	"github.com/cogentcore/webgpu/wgpu"
)

func init() {
	switch os.Getenv("WGPU_LOG_LEVEL") {
	case "OFF":
		wgpu.SetLogLevel(wgpu.LogLevelOff)
	case "ERROR":
		wgpu.SetLogLevel(wgpu.LogLevelError)
	case "WARN":
		wgpu.SetLogLevel(wgpu.LogLevelWarn)
	case "INFO":
		wgpu.SetLogLevel(wgpu.LogLevelInfo)
	case "DEBUG":
		wgpu.SetLogLevel(wgpu.LogLevelDebug)
	case "TRACE":
		wgpu.SetLogLevel(wgpu.LogLevelTrace)
	}
}

type wgpuRuntime struct {
	Adapter *wgpu.Adapter
	Config  *wgpu.SurfaceConfiguration
	Device  *wgpu.Device
	Format  wgpu.TextureFormat
	Queue   *wgpu.Queue
	Surface *wgpu.Surface

	Features map[wgpu.FeatureName]bool
}

var forceFallbackAdapter = os.Getenv("WGPU_FORCE_FALLBACK_ADAPTER") == "1"

func (w *wgpuRuntime) init(width, height uint32, descriptor *wgpu.SurfaceDescriptor) error {
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
	for _, f := range adapter.EnumerateFeatures() {
		w.Features[f] = true
	}

	device, err := w.Adapter.RequestDevice(&wgpu.DeviceDescriptor{
		RequiredFeatures: []wgpu.FeatureName{
			wgpu.FeatureNameTimestampQuery,
		},
	})
	if err != nil {
		return err
	}

	w.Device = device
	w.Queue = w.Device.GetQueue()

	caps := w.Surface.GetCapabilities(w.Adapter)
	w.Format = caps.Formats[0]

	w.Config = &wgpu.SurfaceConfiguration{
		Usage:       wgpu.TextureUsageRenderAttachment,
		Format:      w.Format,
		Width:       width,
		Height:      height,
		PresentMode: wgpu.PresentModeFifo,
		AlphaMode:   caps.AlphaModes[0],
	}

	w.Surface.Configure(w.Adapter, w.Device, w.Config)

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
	r.Config = nil
	r.Features = make(map[wgpu.FeatureName]bool)
}
