package mem

import (
	"errors"
	"math/bits"
)

var (
	ErrNoSpace       = errors.New("out of memory")
	ErrInvalidBuffer = errors.New("invalid buffer")
)

const (
	unusedNode       = nodeId(0xffffffff)
	topBinIndexShift = 3
	leafBinIndexMask = 0x7

	mantissaBits     = 3
	mantissaMaxValue = 1 << mantissaBits
	mantissaMask     = mantissaMaxValue - 1
)

type nodeId int

type node struct {
	offset uint32
	size   uint32

	binPrev nodeId
	binNext nodeId
	prev    nodeId
	next    nodeId

	used bool
}

type Allocation struct {
	id     nodeId
	size   uint32
	offset uint32
}

func (a Allocation) Offset() uint32 {
	return a.offset
}

func (a Allocation) Size() uint32 {
	return a.size
}

type TLSF struct {
	capacity   uint32
	freeSpace  uint32
	freeOffset uint32

	usedTopBins uint32
	usedBins    [32]uint32
	bins        [32 * 8]nodeId

	nodes     []node
	freeNodes []nodeId
}

func (t *TLSF) Alloc(size uint32) (Allocation, error) {
	minBinIndex := t.roundUp(size)

	minTopBinIndex := minBinIndex >> topBinIndexShift
	minLeafBinIndex := minBinIndex & leafBinIndexMask

	topBinIndex := nodeId(minTopBinIndex)
	leafBinIndex := nodeId(unusedNode)

	if t.usedTopBins&(1<<topBinIndex) != 0 {
		leafBinIndex = t.findLowestSetBitAfter(t.usedBins[topBinIndex], minLeafBinIndex)
	}

	if leafBinIndex == unusedNode {
		topBinIndex = t.findLowestSetBitAfter(t.usedTopBins, minTopBinIndex+1)

		if topBinIndex == unusedNode {
			return Allocation{}, ErrNoSpace
		}

		leafBinIndex = nodeId(bits.TrailingZeros32(uint32(t.usedBins[topBinIndex])))
	}

	binIndex := (topBinIndex << topBinIndexShift) | leafBinIndex

	nodeIndex := t.bins[binIndex]

	nodeTotalSize := t.nodes[nodeIndex].size
	nodeOffset := t.nodes[nodeIndex].offset
	nodeNext := t.nodes[nodeIndex].next
	nodeBinNext := t.nodes[nodeIndex].binNext

	t.nodes[nodeIndex].size = size
	t.nodes[nodeIndex].used = true

	t.bins[binIndex] = nodeBinNext

	if nodeBinNext != unusedNode {
		t.nodes[nodeBinNext].binPrev = unusedNode
	}

	t.freeSpace -= nodeTotalSize

	if t.bins[binIndex] == unusedNode {
		t.usedBins[topBinIndex] &^= (1 << leafBinIndex)

		if t.usedBins[topBinIndex] == 0 {
			t.usedTopBins &^= (1 << topBinIndex)
		}
	}

	remainderSize := nodeTotalSize - size
	if remainderSize > 0 {
		newNodeIndex := t.insertNode(remainderSize, nodeOffset+size)

		if nodeNext != unusedNode {
			t.nodes[nodeNext].prev = newNodeIndex
		}
		t.nodes[newNodeIndex].prev = nodeIndex
		t.nodes[newNodeIndex].next = nodeNext
		t.nodes[nodeIndex].next = newNodeIndex
	}

	return Allocation{id: nodeIndex, size: size, offset: nodeOffset}, nil
}

func (t *TLSF) Free(alloc Allocation) error {
	n := t.nodes[alloc.id]

	if !n.used {
		return ErrInvalidBuffer
	}

	offset := n.offset
	size := n.size

	if (n.prev != unusedNode) && !t.nodes[n.prev].used {
		prevNode := t.nodes[n.prev]
		offset = prevNode.offset
		size += prevNode.size

		t.removeNode(n.prev)
		n.prev = prevNode.prev
	}

	if (n.next != unusedNode) && !t.nodes[n.next].used {
		nextNode := t.nodes[n.next]
		size += nextNode.size
		t.removeNode(n.next)
		n.next = nextNode.next
	}

	next := n.next
	prev := n.prev

	t.freeNodes = append(t.freeNodes, alloc.id)
	combinedNodeIndex := t.insertNode(size, offset)

	if next != unusedNode {
		t.nodes[combinedNodeIndex].next = next
		t.nodes[next].prev = combinedNodeIndex
	}

	if prev != unusedNode {
		t.nodes[combinedNodeIndex].prev = prev
		t.nodes[prev].next = combinedNodeIndex
	}

	return nil
}

func (t *TLSF) Capacity() uint32 {
	return t.capacity
}

func (t *TLSF) FreeSpace() uint32 {
	return t.freeSpace
}

func (t *TLSF) findLowestSetBitAfter(mask uint32, start uint32) nodeId {
	maskBeforeStart := uint32(1<<start) - 1
	maskAfterStart := ^maskBeforeStart
	bitsAfter := mask & maskAfterStart

	if bitsAfter == 0 {
		return unusedNode
	}

	return nodeId(bits.TrailingZeros32(bitsAfter))
}

func (t *TLSF) reset() {
	t.usedTopBins = 0
	t.freeOffset = 0
	t.freeSpace = 0

	for i := range len(t.usedBins) {
		t.usedBins[i] = 0
	}

	for i := range len(t.bins) {
		t.bins[i] = unusedNode
	}

	t.nodes = t.nodes[0:0]
	t.freeNodes = t.freeNodes[0:0]

	t.insertNode(t.capacity, 0)
}

func (t *TLSF) insertNode(size uint32, dataOffset uint32) nodeId {
	index := t.roundDown(size)

	topBinIndex := index >> topBinIndexShift
	leafBinIndex := index & leafBinIndexMask

	if t.bins[index] == unusedNode {
		t.usedBins[topBinIndex] |= 1 << leafBinIndex
		t.usedTopBins |= 1 << topBinIndex
	}

	firstNodeIndex := t.bins[index]
	nodeIndex := t.getFreeNodeIndex()

	t.nodes[nodeIndex].size = size
	t.nodes[nodeIndex].offset = dataOffset
	t.nodes[nodeIndex].binNext = firstNodeIndex

	if firstNodeIndex != unusedNode {
		t.nodes[firstNodeIndex].binPrev = nodeIndex
	}

	t.bins[index] = nodeIndex
	t.freeSpace += size
	return nodeIndex
}

func (t *TLSF) removeNode(nodeIndex nodeId) {
	n := t.nodes[nodeIndex]

	if n.binPrev != unusedNode {
		t.nodes[n.binPrev].binNext = n.binNext
		if n.binNext != unusedNode {
			t.nodes[n.binNext].binPrev = n.binPrev
		}
	} else {
		binIndex := t.roundDown(n.size)

		topBinIndex := binIndex >> topBinIndexShift
		leafBinIndex := binIndex & leafBinIndexMask

		t.bins[binIndex] = n.binNext
		if n.binNext != unusedNode {
			t.nodes[n.binNext].binPrev = unusedNode
		}

		if t.bins[binIndex] == unusedNode {
			// Remove a leaf bin mask bit
			t.usedBins[topBinIndex] &^= (1 << leafBinIndex)

			if t.usedBins[topBinIndex] == 0 {
				t.usedTopBins &^= (1 << topBinIndex)
			}
		}
	}

	t.freeNodes = append(t.freeNodes, nodeIndex)
	t.freeSpace -= n.size
}

func (t *TLSF) getFreeNodeIndex() nodeId {
	var index nodeId
	if last := len(t.freeNodes); last > 0 {
		index = t.freeNodes[last-1]
		t.freeNodes = t.freeNodes[0 : last-1]
	} else {
		index = nodeId(len(t.nodes))
		t.nodes = append(t.nodes, node{})
	}

	t.nodes[index].binPrev = unusedNode
	t.nodes[index].binNext = unusedNode
	t.nodes[index].prev = unusedNode
	t.nodes[index].next = unusedNode
	// Clear the used flag: a recycled slot may have been an allocated node whose
	// stale used==true would break neighbour coalescing.
	t.nodes[index].used = false
	return index
}

func (t *TLSF) roundDown(value uint32) uint32 {
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

func (t *TLSF) roundUp(value uint32) uint32 {
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

// floatToUint converts a bin index back to the (lower-bound) byte size it
// represents.
func (t *TLSF) floatToUint(floatValue uint32) uint32 {
	exponent := floatValue >> mantissaBits
	mantissa := floatValue & mantissaMask
	if exponent == 0 {
		return mantissa
	}
	return (mantissa | mantissaMaxValue) << (exponent - 1)
}

// StorageReport returns the total free space and the size of the largest free
// region (a lower bound taken from the highest non-empty bin).
func (t *TLSF) StorageReport() (totalFreeSpace, largestFreeRegion uint32) {
	totalFreeSpace = t.freeSpace
	if t.usedTopBins != 0 {
		topBinIndex := uint32(31 - bits.LeadingZeros32(t.usedTopBins))
		leafBinIndex := uint32(31 - bits.LeadingZeros32(t.usedBins[topBinIndex]))
		largestFreeRegion = t.floatToUint((topBinIndex << topBinIndexShift) | leafBinIndex)
	}
	return totalFreeSpace, largestFreeRegion
}

func NewTLSF(size uint32) *TLSF {
	t := &TLSF{capacity: size}
	t.reset()
	return t
}
