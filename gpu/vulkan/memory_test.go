package vulkan

import (
	"testing"
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
)

func TestAlloc(t *testing.T) {
	b := New()
	err := b.Init()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()

	// Host buffer: mapped + device address; round-trip through the pointer.
	buf := b.Alloc(256, gpu.MemoryHost, "test-host")
	if buf.Addr == 0 {
		t.Fatal("host buffer has no device address")
	}
	if buf.Ptr == nil {
		t.Fatal("host buffer not mapped")
	}
	p := (*uint32)(buf.Ptr)
	*p = 0xDEADBEEF
	*(*uint32)(unsafe.Add(buf.Ptr, 4)) = 0x12345678
	if *p != 0xDEADBEEF || *(*uint32)(unsafe.Add(buf.Ptr, 4)) != 0x12345678 {
		t.Fatal("mapped read-back mismatch")
	}
	t.Logf("host   addr=%#x ptr=%p size=%d", buf.Addr, buf.Ptr, buf.Size)

	// Device buffer: address but no CPU mapping.
	dev := b.Alloc(4096, gpu.MemoryDevice, "test-device")
	if dev.Addr == 0 {
		t.Fatal("device buffer has no device address")
	}
	if dev.Ptr != nil {
		t.Fatal("device buffer should not be mapped")
	}
	t.Logf("device addr=%#x size=%d", dev.Addr, dev.Size)

	b.Free(buf)
	b.Free(dev)
}
