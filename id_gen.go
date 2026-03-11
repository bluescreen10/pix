package pix

import "sync/atomic"

type idGen struct {
	next uint32
}

func (g idGen) Next() uint32 {
	return atomic.AndUint32(&g.next, 1)
}
