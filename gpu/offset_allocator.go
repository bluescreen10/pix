package gpu

import (
	"math/bits"

	"github.com/bluescreen10/dawn-go/wgpu"
)

const (
	unusedNode       = 0xffffffff
	topBinIndexShift = 3
	leafBinIndexMask = 0x7

	mantissaBits     = 3
	mantissaMaxValue = 1 << mantissaBits
	mantissaMask     = mantissaMaxValue - 1
)

type offsetAllocatorNode struct {
	offset uint32
	size   uint32

	binPrev int
	binNext int
	prev    int
	next    int

	used bool
}

var _ Arena = &OffsetAllocator{}

type OffsetAllocator struct {
	instance  *Instance
	rawBuffer *wgpu.Buffer

	maxSize    uint32
	freeSize   uint32
	freeOffset uint32

	usedTopBins uint32
	usedBins    [32]uint32
	bins        [32 * 8]int

	nodes     []offsetAllocatorNode
	freeNodes []int
}

func (a *OffsetAllocator) Alloc(size uint32) (Buffer, error) {
	minBinIndex := a.roundUp(size)

	minTopBinIndex := minBinIndex >> topBinIndexShift
	minLeafBinIndex := minBinIndex & leafBinIndexMask

	topBinIndex := uint32(minTopBinIndex)
	leafBinIndex := uint32(unusedNode)

	if a.usedTopBins&(1<<topBinIndex) != 0 {
		leafBinIndex = a.findLowestSetBitAfter(a.usedBins[topBinIndex], minLeafBinIndex)
	}

	if leafBinIndex == unusedNode {
		topBinIndex = a.findLowestSetBitAfter(a.usedTopBins, minTopBinIndex+1)

		if topBinIndex == unusedNode {
			return Buffer{}, ErrNoSpace
		}

		leafBinIndex = uint32(bits.TrailingZeros32(uint32(a.usedBins[topBinIndex])))
	}

	binIndex := (topBinIndex << topBinIndexShift) | leafBinIndex

	nodeIndex := a.bins[binIndex]

	nodeTotalSize := a.nodes[nodeIndex].size
	nodeOffset := a.nodes[nodeIndex].offset
	nodeNext := a.nodes[nodeIndex].next
	nodeBinNext := a.nodes[nodeIndex].binNext

	a.nodes[nodeIndex].size = size
	a.nodes[nodeIndex].used = true

	a.bins[binIndex] = nodeBinNext

	if nodeBinNext != unusedNode {
		a.nodes[nodeBinNext].binPrev = unusedNode
	}

	a.freeSize -= nodeTotalSize

	if a.bins[binIndex] == unusedNode {
		a.usedBins[topBinIndex] &^= (1 << leafBinIndex)

		if a.usedBins[topBinIndex] == 0 {
			a.usedTopBins &^= (1 << topBinIndex)
		}
	}

	reminderSize := nodeTotalSize - size
	if reminderSize > 0 {
		newNodeIndex := a.insertNode(reminderSize, nodeOffset+size)

		if nodeNext != unusedNode {
			a.nodes[nodeNext].prev = newNodeIndex
		}
		a.nodes[newNodeIndex].prev = nodeIndex
		a.nodes[newNodeIndex].next = nodeNext
		a.nodes[nodeIndex].next = newNodeIndex
	}

	id, gen := a.instance.buffers.Alloc(bufferData{
		raw:      a.rawBuffer,
		size:     uint64(size),
		offset:   uint64(nodeOffset),
		arena:    a,
		arenaIdx: nodeIndex,
	})
	return Buffer{idx: id, gen: gen}, nil
}

func (a *OffsetAllocator) Free(buf Buffer) error {
	data := a.instance.buffers.Get(buf.idx)

	if data == nil || data.raw != a.rawBuffer || data.arena != a {
		return ErrInvalidBuffer
	}

	nodeIndex := data.arenaIdx
	node := a.nodes[nodeIndex]

	if !node.used {
		return ErrInvalidBuffer
	}

	offset := node.offset
	size := node.size

	if (node.prev != unusedNode) && !a.nodes[node.prev].used {
		prevNode := a.nodes[node.prev]
		offset = prevNode.offset
		size += prevNode.size

		a.removeNode(node.prev)
		node.prev = prevNode.prev
	}

	if (node.next != unusedNode) && !a.nodes[node.next].used {
		nextNode := a.nodes[node.next]
		size += nextNode.size
		a.removeNode(node.next)
		node.next = nextNode.next
	}

	next := node.next
	prev := node.prev

	a.freeNodes = append(a.freeNodes, nodeIndex)
	combinedNodeIndex := a.insertNode(size, offset)

	if next != unusedNode {
		a.nodes[combinedNodeIndex].next = next
		a.nodes[next].prev = combinedNodeIndex
	}

	if prev != unusedNode {
		a.nodes[combinedNodeIndex].prev = prev
		a.nodes[prev].next = combinedNodeIndex
	}

	return nil
}

func (a *OffsetAllocator) findLowestSetBitAfter(mask uint32, start uint32) uint32 {
	maskBeforeStart := uint32(1<<start) - 1
	maskAfterStart := ^maskBeforeStart
	bitsAfter := mask & maskAfterStart

	if bitsAfter == 0 {
		return unusedNode
	}

	return uint32(bits.TrailingZeros32(bitsAfter))
}

func (a *OffsetAllocator) reset() {
	a.usedTopBins = 0
	a.freeOffset = 0
	a.freeSize = 0

	for i := range len(a.usedBins) {
		a.usedBins[i] = 0
	}

	for i := range len(a.bins) {
		a.bins[i] = unusedNode
	}

	a.nodes = a.nodes[0:0]
	a.freeNodes = a.freeNodes[0:0]

	a.insertNode(a.maxSize, 0)
}

func (a *OffsetAllocator) insertNode(size uint32, dataOffset uint32) int {
	index := a.roundDown(size)

	topBinIndex := index >> topBinIndexShift
	leafBinIndex := index & leafBinIndexMask

	if a.bins[index] == unusedNode {
		a.usedBins[topBinIndex] |= 1 << leafBinIndex
		a.usedTopBins |= 1 << topBinIndex
	}

	firstNodeIndex := a.bins[index]
	nodeIndex := a.getFreeNodeIndex()

	a.nodes[nodeIndex].size = size
	a.nodes[nodeIndex].offset = dataOffset
	a.nodes[nodeIndex].binNext = firstNodeIndex

	if firstNodeIndex != unusedNode {
		a.nodes[firstNodeIndex].binPrev = nodeIndex
	}

	a.bins[index] = nodeIndex
	a.freeSize += size
	return nodeIndex
}

func (a *OffsetAllocator) removeNode(nodeIndex int) {
	node := a.nodes[nodeIndex]

	if node.binPrev != unusedNode {
		a.nodes[node.binPrev].binNext = node.binNext
		if node.binNext != unusedNode {
			a.nodes[node.binNext].binPrev = node.binPrev
		}
	} else {
		binIndex := a.roundDown(node.size)

		topBinIndex := binIndex >> topBinIndexShift
		leafBinIndex := binIndex & leafBinIndexMask

		a.bins[binIndex] = node.binNext
		if node.binNext != unusedNode {
			a.nodes[node.binNext].binPrev = unusedNode
		}

		if a.bins[binIndex] == unusedNode {
			// Remove a leaf bin mask bit
			a.usedBins[topBinIndex] &^= (1 << leafBinIndex)

			if a.usedBins[topBinIndex] == 0 {
				a.usedTopBins &^= (1 << topBinIndex)
			}
		}
	}

	a.freeNodes = append(a.freeNodes, nodeIndex)
	a.freeSize -= node.size
}

func (a *OffsetAllocator) getFreeNodeIndex() int {
	var index int
	if last := len(a.freeNodes); last > 0 {
		index = a.freeNodes[last-1]
		a.freeNodes = a.freeNodes[0:last]
	} else {
		index = len(a.nodes)
		a.nodes = append(a.nodes, offsetAllocatorNode{})
	}

	a.nodes[index].binPrev = unusedNode
	a.nodes[index].binNext = unusedNode
	a.nodes[index].prev = unusedNode
	a.nodes[index].next = unusedNode
	// Clear the used flag: a recycled slot may have been an allocated node whose
	// stale used==true would break neighbour coalescing.
	a.nodes[index].used = false
	return index
}

func (a *OffsetAllocator) roundDown(value uint32) uint32 {
	exp := uint32(0)
	mantissa := uint32(0)

	if value < mantissaMaxValue {
		mantissa = value
	} else {
		leadingZeros := bits.LeadingZeros32(value)
		highestSetBit := 31 - leadingZeros

		mantissaStartBit := highestSetBit - mantissaBits
		exp = uint32(mantissaStartBit) + 1
		mantissa = (value >> uint32(mantissaStartBit)) & mantissaMask
	}

	return (exp << mantissaBits) | mantissa
}

func (a *OffsetAllocator) roundUp(value uint32) uint32 {
	exp := uint32(0)
	mantissa := uint32(0)

	if value < mantissaMaxValue {
		mantissa = value
	} else {
		leadingZeros := bits.LeadingZeros32(value)
		highestSetBit := 31 - leadingZeros

		mantissaStartBit := highestSetBit - mantissaBits
		exp = uint32(mantissaStartBit) + 1
		mantissa = (value >> uint32(mantissaStartBit)) & mantissaMask

		lowBitsMask := uint32(1<<mantissaStartBit) - 1

		if value&lowBitsMask != 0 {
			mantissa++
		}
	}

	return (exp << mantissaBits) + mantissa
}

// NewArena creates a GPU-backed arena of the given size (bytes) and usage.
// Sub-ranges are handed out as Buffer handles by Alloc; they all share the one
// backing buffer, which the Instance destroys on shutdown.
func (i *Instance) NewArena(size uint32, usage BufferUsage) *OffsetAllocator {
	raw := i.device.CreateBuffer(wgpu.BufferDescriptor{
		Label: "arena",
		Size:  uint64(size),
		Usage: usage | wgpu.BufferUsageCopyDst,
	})
	a := &OffsetAllocator{instance: i, rawBuffer: raw, maxSize: size}
	a.reset()
	i.arenas = append(i.arenas, a)
	return a
}
