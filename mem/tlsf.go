package mem

import (
	"errors"
	"math/bits"
)

var (
	// ErrNoSpace indicates that an allocator has no free region large enough to
	// satisfy a request.
	ErrNoSpace = errors.New("mem: insufficient free space")
	// ErrInvalidAllocation indicates that an allocation has already been freed.
	ErrInvalidAllocation = errors.New("mem: invalid allocation")
)

const (
	unusedNode       = nodeID(0xffffffff)
	topBinIndexShift = 3
	leafBinIndexMask = 0x7

	mantissaBits     = 3
	mantissaMaxValue = 1 << mantissaBits
	mantissaMask     = mantissaMaxValue - 1
)

type nodeID int

type node struct {
	offset uint32
	size   uint32

	binPrev nodeID
	binNext nodeID
	prev    nodeID
	next    nodeID

	used bool
}

// Allocation identifies a byte range reserved by a TLSF allocator. Allocation
// values must come from Alloc and must only be freed once.
type Allocation struct {
	id     nodeID
	size   uint32
	offset uint32
}

// Offset returns the allocation's byte offset from the start of its allocator.
func (a Allocation) Offset() uint32 {
	return a.offset
}

// Size returns the allocation's size in bytes.
func (a Allocation) Size() uint32 {
	return a.size
}

// TLSF manages byte ranges using the two-level segregated fit algorithm.
// A TLSF must be initialized with NewTLSF before use.
type TLSF struct {
	capacity  uint32
	freeSpace uint32

	usedTopBins uint32
	usedBins    [32]uint32
	bins        [32 * 8]nodeID

	nodes     []node
	freeNodes []nodeID
}

// Alloc reserves a contiguous range of size bytes.
func (t *TLSF) Alloc(size uint32) (Allocation, error) {
	minBinIndex := t.roundUp(size)

	minTopBinIndex := minBinIndex >> topBinIndexShift
	minLeafBinIndex := minBinIndex & leafBinIndexMask

	topBinIndex := nodeID(minTopBinIndex)
	leafBinIndex := nodeID(unusedNode)

	if t.usedTopBins&(1<<topBinIndex) != 0 {
		leafBinIndex = t.findLowestSetBitAfter(t.usedBins[topBinIndex], minLeafBinIndex)
	}

	if leafBinIndex == unusedNode {
		topBinIndex = t.findLowestSetBitAfter(t.usedTopBins, minTopBinIndex+1)

		if topBinIndex == unusedNode {
			return Allocation{}, ErrNoSpace
		}

		leafBinIndex = nodeID(bits.TrailingZeros32(uint32(t.usedBins[topBinIndex])))
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

// Free releases allocation. It returns ErrInvalidAllocation when allocation has
// already been freed.
func (t *TLSF) Free(allocation Allocation) error {
	node := t.nodes[allocation.id]

	if !node.used {
		return ErrInvalidAllocation
	}

	offset := node.offset
	size := node.size

	if node.prev != unusedNode && !t.nodes[node.prev].used {
		previousNode := t.nodes[node.prev]
		offset = previousNode.offset
		size += previousNode.size

		t.removeNode(node.prev)
		node.prev = previousNode.prev
	}

	if node.next != unusedNode && !t.nodes[node.next].used {
		nextNode := t.nodes[node.next]
		size += nextNode.size
		t.removeNode(node.next)
		node.next = nextNode.next
	}

	next := node.next
	previous := node.prev

	t.freeNodes = append(t.freeNodes, allocation.id)
	combinedNodeIndex := t.insertNode(size, offset)

	if next != unusedNode {
		t.nodes[combinedNodeIndex].next = next
		t.nodes[next].prev = combinedNodeIndex
	}

	if previous != unusedNode {
		t.nodes[combinedNodeIndex].prev = previous
		t.nodes[previous].next = combinedNodeIndex
	}

	return nil
}

// Capacity returns the number of bytes managed by t.
func (t *TLSF) Capacity() uint32 {
	return t.capacity
}

// FreeSpace returns the total number of unallocated bytes.
func (t *TLSF) FreeSpace() uint32 {
	return t.freeSpace
}

func (t *TLSF) findLowestSetBitAfter(mask uint32, start uint32) nodeID {
	maskBeforeStart := uint32(1<<start) - 1
	maskAfterStart := ^maskBeforeStart
	bitsAfter := mask & maskAfterStart

	if bitsAfter == 0 {
		return unusedNode
	}

	return nodeID(bits.TrailingZeros32(bitsAfter))
}

func (t *TLSF) reset() {
	t.usedTopBins = 0
	t.freeSpace = 0

	for i := range t.usedBins {
		t.usedBins[i] = 0
	}

	for i := range t.bins {
		t.bins[i] = unusedNode
	}

	t.nodes = t.nodes[:0]
	t.freeNodes = t.freeNodes[:0]

	t.insertNode(t.capacity, 0)
}

func (t *TLSF) insertNode(size, offset uint32) nodeID {
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
	t.nodes[nodeIndex].offset = offset
	t.nodes[nodeIndex].binNext = firstNodeIndex

	if firstNodeIndex != unusedNode {
		t.nodes[firstNodeIndex].binPrev = nodeIndex
	}

	t.bins[index] = nodeIndex
	t.freeSpace += size
	return nodeIndex
}

func (t *TLSF) removeNode(nodeIndex nodeID) {
	node := t.nodes[nodeIndex]

	if node.binPrev != unusedNode {
		t.nodes[node.binPrev].binNext = node.binNext
		if node.binNext != unusedNode {
			t.nodes[node.binNext].binPrev = node.binPrev
		}
	} else {
		binIndex := t.roundDown(node.size)

		topBinIndex := binIndex >> topBinIndexShift
		leafBinIndex := binIndex & leafBinIndexMask

		t.bins[binIndex] = node.binNext
		if node.binNext != unusedNode {
			t.nodes[node.binNext].binPrev = unusedNode
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
	t.freeSpace -= node.size
}

func (t *TLSF) getFreeNodeIndex() nodeID {
	var index nodeID
	if last := len(t.freeNodes); last > 0 {
		index = t.freeNodes[last-1]
		t.freeNodes = t.freeNodes[:last-1]
	} else {
		index = nodeID(len(t.nodes))
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

// binSize returns the lower-bound byte size represented by binIndex.
func (t *TLSF) binSize(binIndex uint32) uint32 {
	exponent := binIndex >> mantissaBits
	mantissa := binIndex & mantissaMask
	if exponent == 0 {
		return mantissa
	}
	return (mantissa | mantissaMaxValue) << (exponent - 1)
}

// StorageReport returns the total free space and the size of the largest free
// region (a lower bound taken from the highest non-empty bin).
func (t *TLSF) StorageReport() (totalFreeSpace, largestFreeRegion uint32) {
	totalFreeSpace = t.freeSpace
	if t.usedTopBins == 0 {
		return totalFreeSpace, 0
	}
	topBinIndex := uint32(31 - bits.LeadingZeros32(t.usedTopBins))
	leafBinIndex := uint32(31 - bits.LeadingZeros32(t.usedBins[topBinIndex]))
	largestFreeRegion = t.binSize((topBinIndex << topBinIndexShift) | leafBinIndex)
	return totalFreeSpace, largestFreeRegion
}

// NewTLSF returns an allocator that manages capacity bytes.
func NewTLSF(capacity uint32) *TLSF {
	t := &TLSF{capacity: capacity}
	t.reset()
	return t
}
