package geometries

import "unsafe"

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
