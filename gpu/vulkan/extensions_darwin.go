package vulkan

// defaultInstanceExtensions enables the Metal surface extensions on macOS so a
// window can be attached after Init without reconstructing the instance.
func defaultInstanceExtensions() []string { return MetalSurfaceExtensions }
