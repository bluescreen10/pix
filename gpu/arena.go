package gpu

import "errors"

var (
	ErrNoSpace       = errors.New("no memory available")
	ErrInvalidBuffer = errors.New("invalid buffer")
)

type Arena interface {
	Alloc(size uint32) (Buffer, error)
	Free(Buffer) error
}
