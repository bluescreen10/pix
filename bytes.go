package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
)

// writeAt copies data into a host-mapped buffer at a byte offset.
func writeAt(buf gpu.Buffer, byteOffset uint32, data []byte) {
	if len(data) == 0 {
		return
	}
	dst := unsafe.Slice((*byte)(buf.Ptr), buf.Size)
	copy(dst[byteOffset:], data)
}

func toBytes[T any](s []T) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*int(unsafe.Sizeof(s[0])))
}

func fromBytes[T any](b []byte, n int) []T {
	if n == 0 || len(b) == 0 {
		return nil
	}
	return unsafe.Slice((*T)(unsafe.Pointer(&b[0])), n)
}
