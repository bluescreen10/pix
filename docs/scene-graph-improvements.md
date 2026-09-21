# Scene graph improvements — transform update and topology

Status: **items 1 and 2 are implemented** (`scene.go`, `node.go`,
`skeleton.go` — see `scene_topology_test.go` for the equivalence/regression
suite). Items 3-5 remain proposed. This is a design record for making
`Scene`'s per-frame transform update cheaper, written after noticing that
decals becoming scene nodes (see `docs/decal-system.md`'s "Storage") made
structural churn a per-frame event for spawn-heavy workloads, which the
topology rebuild wasn't shaped for at the time (decals are mesh-based again
now, but the underlying cost is real for any spawn-heavy workload —
particles-as-nodes, bullet holes, procedural content — so items 1 and 2 were
worth doing regardless).

Ideas are ordered by value-for-effort, not by size. Items 1 and 2 are the
recommended starting point: both are contained, low-risk, and target costs
the engine pays today. Item 3 is worth doing only with profiling evidence.
Item 5 is recorded to be dismissed, not built.

## What it costs today

Two functions in `scene.go`, both called from `Scene.Sync` once per frame:

`flushTopoIfDirty` (`scene.go:441`), when `topoDirty` is set:

- Clears `flagAttached` across **every** node — an O(N) loop over `s.flags`,
  regardless of how localized the change was.
- Rebuilds `topoOrder` from scratch with a BFS from the root, allocating a
  fresh queue every call (`queue := []uint32{s.root.index}`) and then
  re-slicing it forward (`queue = queue[1:]`), which prevents any reuse and
  makes `append` repeatedly grow new backing arrays.
- `topoDirty` is set unconditionally by `reparent`, `detachFromParent`, and
  `destroyNode` — so *any* structural change, however local, pays the full
  rebuild.

`updateTransforms` (`scene.go:471`):

- Iterates **all** of `topoOrder` every frame, even when nothing is dirty,
  testing one flag per node. Cheap per node; pure waste at scale when the
  scene is static.
- For each dirty node, computes `local`, then `world`, then
  `worldInv = world.Inv()` — a general 4×4 inverse, by a wide margin the most
  expensive operation in the loop (more than the `Mul4x4` preceding it).

The cost profile that falls out: a mostly-static scene still pays an O(N)
scan per frame; a scene with any structural churn pays an O(N) rebuild on top
of that; and every node that *does* move pays roughly 2-3× the arithmetic it
needs to, for an inverse almost nothing reads.

## 1. O(1) attach for the common case

The overwhelmingly common structural change is attaching a **leaf** node to
an **already-attached** parent: `scene.Add(mesh)`, `scene.Add(decal)`,
spawning anything at all. That case needs no topology rebuild whatsoever.

Appending such a node to the end of `topoOrder` can never violate the
parent-before-child invariant, because its parent is already somewhere
earlier in the array. So `reparent(child, parent)` where `child` has no
children and `parent` already has `flagAttached` reduces to: append to
`topoOrder`, set `flagAttached`, done — no walk, no invalidation, no
`topoDirty`.

Detaching a leaf is similarly cheap if `topoOrder` is allowed to carry holes:
clear `flagAttached` and leave the entry in place, skipping non-attached
entries when iterating (`updateTransforms` already skips on a flag test), and
compact lazily — on the next genuine full rebuild, or when the hole count
crosses some fraction of the array.

Only genuine mid-tree reparenting of a subtree with children still needs the
full rebuild, which is rare and usually not in a hot loop.

This matters more since decals became nodes: a bullet-hole-style workload now
sets `topoDirty` every frame, which today means a full O(N) rebuild every
frame. This item turns that back into an O(1) append per spawn.

Care required: the "is a leaf" and "parent is attached" conditions must be
checked, not assumed, with a fallback to the existing full rebuild otherwise.
A node that already has children (e.g. a loaded glTF subtree being added) is
not eligible — though a subtree whose *root* is being appended could be
handled by appending the subtree in its own BFS order, which is a natural
follow-on if measurement says it's worth it.

**Implemented** as described above, plus one correctness gap the original
writeup elided: a leaf that was *previously* attached (moved, or
destroyed-and-its-slot-later-recycled) can't just get a fresh append — its old
`topoOrder` entry is still sitting there holding the same node-slot index, and
left alone it would eventually be misread as live again if that slot got
reused by an unrelated new node. Fixed with a reverse index, `topoPos
[]uint32` (a node's own position in `topoOrder`, or `invalidIdx`), maintained
alongside the other per-node-slot arrays. `detachFromParent`'s fast path
writes an `invalidIdx` tombstone into the leaf's old slot before
`reparent`'s fast path appends a fresh one — so a node is never live in
`topoOrder` twice. Tombstones are bounded to under half of `len(topoOrder)`
(a 64-entry floor before the check kicks in, to avoid rebuilding tiny scenes
on every churn); crossing it forces `topoDirty = true`, letting the existing
full rebuild reclaim the dead entries — otherwise a scene that only ever
attaches/detaches leaves (continuous bullet-hole-style spawning) would grow
`topoOrder` without bound. `updateTransforms` skips tombstones on read.
`destroyNode`'s own redundant unconditional `topoDirty = true` was also
removed: it only ever runs via `destroySubtree`, bottom-up, so the node being
destroyed always has zero children by the time it runs — every single-node
destroy is fast-path eligible for free once `detachFromParent`'s fast path
exists, without `destroyNode` needing its own logic. See
`scene_topology_test.go` for the equivalence test (world matrices recomputed
independently by walking real parent pointers, checked after a long
randomized sequence of ops) plus targeted tests for each of the above.

## 2. Stop computing `worldInv` for everyone

`scene.go:485` already carries this as a TODO. Every dirty node pays a full
4×4 inverse, but the only readers are skeleton roots (`Scene.updateSkinning`)
and the public `Node.WorldTransformInv` accessor. In a scene of 10k moving
boxes, that is 10k inversions per pass thrown away unread.

Two shapes, either acceptable:

- **Compute where it's needed.** `s.skeletons` already knows exactly which
  nodes are skeleton roots, so `updateSkinning` can invert just those few
  directly, and `Node.WorldTransformInv` can compute on demand. Simplest;
  makes the accessor O(1) work per call rather than free, which is fine for
  something called rarely.
- **Cache behind a dirty bit.** Add a `flagWorldInvDirty`, set it in
  `updateTransforms` instead of doing the inverse, and have both readers
  compute-and-clear on first read. Keeps repeat reads free at the cost of one
  more flag.

Either way this is a straight deletion from the hot path, and the most
expensive single operation in it.

**Implemented** via the first shape ("compute where it's needed"): the
`worldInv []glm.Mat4f` field is gone entirely: `Node.WorldTransformInv()`
computes `n.scene.world[n.slot()].Inv()` on demand, and `updateSkinning`
computes each skeleton root's inverse directly instead of reading a
precomputed array. Simpler than the dirty-bit cache, and the two call sites
are rare enough (once per `WorldTransformInv()` call; once per skeleton per
`Sync`, not once per scene node) that the repeat-read cost the cache would
have avoided doesn't come up in practice.

## 3. A dirty frontier instead of a full scan

Rather than scanning all of `topoOrder` and testing `flagTransformDirty` per node,
maintain an explicit `s.dirtyRoots []uint32`. The setters push to it on the
0→1 transition only — the existing `flagTransformDirty` check already gives that test
for free, so the push costs nothing extra when a node is dirtied repeatedly
in one frame. `updateTransforms` then walks only those subtrees depth-first;
parent-before-child ordering falls out of descending, exactly as the current
code's lazy "mark my children dirty as I go" already relies on.

Five moved nodes in a 10k-node scene goes from 10k iterations to five short
walks.

Two things to get right:

- **Every** site that ORs in `flagTransformDirty` today must push too — all of
  `node.go`'s many setters (`SetPosition`, `SetRotationQuat`, `SetScale`,
  every `Move*`/`Rotate*` delta), plus `reparent` and `resetSlot`. A missed
  site is a silently stale transform, which is exactly the kind of bug that
  survives review. This wants a test asserting equivalence against the
  current full-scan behavior over a randomized mutation sequence.
- Dedupe: if a node and one of its ancestors are both dirty, processing the
  ancestor's subtree already covers the descendant. Either sort the frontier
  by depth and skip nodes already cleaned this pass, or simply let the
  descendant's own walk re-run (idempotent, just wasted work) and measure
  whether it matters.

Because `updateTransforms` returns `anyDirty` and `Sync` skips the GPU upload
when it's false, the cost this removes is the *scan*, not upload work — so
the win scales with node count, not with how much actually moved.

## 4. Free cleanups in `flushTopoIfDirty`

Worth doing whenever that function is touched, independent of everything
above:

- Reuse a scratch queue with a read cursor instead of `queue = queue[1:]`
  plus a fresh allocation per call.
- Replace the separate O(N) `flagAttached` clear with a generation stamp: an
  `attachedStamp []uint32` compared against a scene-level counter, so the
  pass only ever *sets* the current stamp rather than clearing every node
  first and then setting the visited ones. Halves the work of a full rebuild
  and removes a full array write.

## 5. Recorded and dismissed: physical topological ordering

The DOTS-style "flat hierarchy" layout — store nodes physically so a parent's
index is always less than its children's, making the update a straight linear
scan with no `topoOrder` indirection and much better cache behavior — is the
obvious next escalation, and it is the wrong fit here.

It requires relocating nodes on restructure, which breaks the stable-slot-index
contract that `gpuDrawable.transformID`, a decal's `targetID`, and every
payload's `ownerNode` all depend on. That contract is load-bearing across the
renderer, the decal system, and skinning. The change is far deeper than the
win justifies; items 1-4 recover most of the same cost without touching it.

## Relationship to the FramePacket work

`docs/frame-packet.md` proposes handing the renderer borrowed, **versioned**
tables rather than letting it read `Scene` directly, with the explicit goal
that "neither rebuilds unchanged data just because a new frame begins." Items
1 and 3 point the same direction: both are about knowing precisely what
changed instead of rediscovering it by scanning. A dirty frontier (item 3) is
close to the per-table revision counter that design wants, and an
incrementally maintained `topoOrder` (item 1) is what lets a world-matrix
table claim "unchanged since revision N" honestly.

Worth sequencing deliberately: items 1, 2, and 4 are local and safe to land
before that rework. Item 3's bookkeeping overlaps with whatever change
tracking FramePacket introduces, so doing it twice would be wasteful — either
land it first and let FramePacket build on it, or defer it into that work
rather than inventing a second, parallel mechanism.

## Verification

Any of these changes needs, at minimum:

- An equivalence test: build a scene with a mixed hierarchy (nested groups,
  meshes, skeleton-bone chains, decals), apply a randomized sequence of
  transform mutations and structural changes, and assert every node's `world`
  matrix matches what the current full-rebuild/full-scan implementation
  produces. This is the test that catches a missed `flagTransformDirty` push site.
- Attachment correctness specifically: nodes created but never added must
  stay unattached and undrawn; detached subtrees must stop updating; a
  reattached subtree must pick up its parent's current transform.
- A benchmark with a large, mostly-static scene (to show the idle-scan win)
  and a spawn-heavy one (to show the O(1)-attach win), since the two items
  target different costs and one can regress while the other improves.
