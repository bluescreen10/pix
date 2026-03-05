package pix

import "sync/atomic"

type Handle[T any] struct {
	id       int
	refCount *int32
	destroy  func()
}

func (h Handle[T]) Clone() Handle[T] {
	atomic.AddInt32(h.refCount, 1)
	return Handle[T]{
		id:       h.id,
		refCount: h.refCount,
		destroy:  h.destroy,
	}
}

func (h Handle[T]) Release() {
	atomic.AddInt32(h.refCount, -1)
	if *h.refCount == 0 && h.destroy != nil {
		h.destroy()
	}
}

func (h Handle[T]) IsValid() bool {
	return h.id != -1
}
