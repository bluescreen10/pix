#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// particle_sort_step — one step of a bitonic sort of a particle system's order buffer,
// farthest first. The CPU runs it once per (blockSize, partnerDistance) pair: blockSize
// doubling from 2 up to the entry count, and for each, partnerDistance halving from
// blockSize/2 down to 1, with a barrier between steps. Each step compares every entry
// with the one partnerDistance away and swaps them if they are out of order.
layout(local_size_x = 64) in;

struct SortEntry { float key; uint particle; };
layout(buffer_reference, scalar) buffer OrderBuf { SortEntry v[]; };

// Matches particleSortStepRoot in particle_gpu.go.
layout(push_constant, scalar) uniform PC {
    OrderBuf order;
    uint count;
    uint blockSize;
    uint partnerDistance;
    uint pad0;
    uint pad1;
    uint pad2;
} pc;

// comesFirst reports whether a belongs before b: the farther first, and between equal
// distances the lower index, so that equal keys come out the same every frame.
bool comesFirst(SortEntry a, SortEntry b) {
    if (a.key != b.key) {
        return a.key > b.key;
    }
    return a.particle < b.particle;
}

void main() {
    uint i = gl_GlobalInvocationID.x;
    uint partner = i ^ pc.partnerDistance;
    if (i >= pc.count || partner <= i) {
        return;
    }
    SortEntry a = pc.order.v[i];
    SortEntry b = pc.order.v[partner];
    // Alternate blocks sort in opposite directions, so that each merge of two blocks
    // is a merge of a bitonic sequence.
    bool outOfOrder = comesFirst(b, a);
    if ((i & pc.blockSize) != 0u) {
        outOfOrder = comesFirst(a, b);
    }
    if (outOfOrder) {
        pc.order.v[i] = b;
        pc.order.v[partner] = a;
    }
}
