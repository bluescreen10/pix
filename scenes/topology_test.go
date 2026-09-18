package scenes

import (
	"math/rand"
	"testing"

	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
)

// topoTestScene builds a scene plus a maker for mesh nodes, for tests that need real
// (not group-only) nodes with a payload: swap-remove and slot reuse are payload-array
// behaviour that groups never exercise.
//
// No renderer and no GPU. The geometry handle is the zero value and the material a
// stub — nothing here asks either of them anything, because a Scene only holds the
// handles and publishes their identities.
func topoTestScene(t *testing.T) (*Scene, func() Mesh) {
	t.Helper()
	scene := New()
	t.Cleanup(scene.Destroy)
	mat := newFakeMaterial()
	return scene, func() Mesh { return scene.NewMesh(geometries.Geometry{}, mat) }
}

// collectAttached walks the REAL parent/child structure from root (firstChildren/
// nextSiblings), independent of topoOrder entirely — this is the ground truth
// against which topoOrder-derived state is checked.
func collectAttached(s *Scene, idx uint32) []uint32 {
	out := []uint32{idx}
	c := s.firstChildren[idx]
	for c.isValid() {
		out = append(out, collectAttached(s, c.index)...)
		c = s.nextSiblings[c.index]
	}
	return out
}

// expectedWorld recomputes idx's world matrix by walking real parent pointers
// (s.parents), not topoOrder — independent of the fast-path bookkeeping under
// test, so a divergence here means s.world itself is wrong, not just stale.
func expectedWorld(s *Scene, idx uint32) glm.Mat4f {
	local := s.transforms[idx].Matrix()
	p := s.parents[idx]
	if !p.isValid() {
		return local
	}
	return expectedWorld(s, p.index).Mul4x4(local)
}

func matNear(a, b glm.Mat4f) bool {
	for i := range a {
		d := a[i] - b[i]
		if d < -1e-4 || d > 1e-4 {
			return false
		}
	}
	return true
}

// assertWorldsCorrect syncs the scene and checks every currently-attached node's
// cached world matrix against expectedWorld — the equivalence check the fast
// attach/detach paths must never violate.
func assertWorldsCorrect(t *testing.T, s *Scene) {
	t.Helper()
	s.Sync()
	for _, idx := range collectAttached(s, s.root.index) {
		got := s.world[idx]
		want := expectedWorld(s, idx)
		if !matNear(got, want) {
			t.Fatalf("node %d: world = %v, want %v", idx, got, want)
		}
	}
}

// TestTopologyRandomizedEquivalence applies a long randomized sequence of
// Add/Remove/reparent/SetPosition/Destroy operations across a mixed hierarchy
// (groups and meshes) and checks, after every operation, that every attached
// node's cached world matrix matches one computed by walking the real parent
// chain directly — independent of whether the fast or slow topology path
// handled any given operation.
func TestTopologyRandomizedEquivalence(t *testing.T) {
	scene, newMesh := topoTestScene(t)
	rng := rand.New(rand.NewSource(1))

	var nodes []Node
	for i := 0; i < 12; i++ {
		if i%3 == 0 {
			g := scene.NewGroup()
			nodes = append(nodes, g.Node)
		} else {
			m := newMesh()
			nodes = append(nodes, m.Node)
		}
	}

	pick := func() Node { return nodes[rng.Intn(len(nodes))] }

	for iter := 0; iter < 500; iter++ {
		n := pick()
		switch rng.Intn(5) {
		case 0, 1: // Add under root or another node (also covers moving an already-attached leaf)
			parent := pick()
			if parent.ID() == n.ID() || wouldCycleForTest(scene, n, parent) {
				continue
			}
			parent.Add(n)
		case 2: // detach
			if p := n.Parent(); p.IsValid() {
				p.Remove(n)
			}
		case 3: // move transform
			n.SetPosition(glm.Vec3f{rng.Float32()*10 - 5, rng.Float32()*10 - 5, rng.Float32()*10 - 5})
		case 4: // Add to root directly
			scene.Add(n)
		}
		assertWorldsCorrect(t, scene)
	}
}

// wouldCycleForTest mirrors Scene.wouldCycle's check from the test side (that
// method is unexported and reparent already panics on a real cycle, but the
// randomized test would rather skip an invalid op than fail on an intentional
// panic path).
func wouldCycleForTest(s *Scene, child, newParent Node) bool {
	cur := newParent.id
	for cur.isValid() {
		if cur == child.id {
			return true
		}
		cur = s.parents[cur.index]
	}
	return false
}

// TestTopologyFastPathNoRebuild proves the O(1) attach path is real: attaching
// many leaves to an already-attached parent must never set topoDirty.
func TestTopologyFastPathNoRebuild(t *testing.T) {
	scene, newMesh := topoTestScene(t)
	parent := scene.NewGroup()
	scene.Add(parent)
	scene.Sync() // settle the parent's own attach before measuring leaf attaches

	for i := 0; i < 200; i++ {
		m := newMesh()
		parent.Add(m)
		if scene.topoDirty {
			t.Fatalf("iteration %d: attaching a leaf under an already-attached parent set topoDirty", i)
		}
	}
	assertWorldsCorrect(t, scene)
}

// TestTopologyTombstoneReuse detaches and reattaches the same leaf to different
// attached parents repeatedly, and checks topoOrder never ends up with more
// than one live (non-tombstone) entry for that node's slot.
func TestTopologyTombstoneReuse(t *testing.T) {
	scene, newMesh := topoTestScene(t)
	parentA := scene.NewGroup()
	parentB := scene.NewGroup()
	scene.Add(parentA)
	scene.Add(parentB)
	scene.Sync()

	m := newMesh()
	idx := m.slot()
	for i := 0; i < 20; i++ {
		if i%2 == 0 {
			parentA.Add(m)
		} else {
			parentB.Add(m)
		}

		live := 0
		for _, e := range scene.topoOrder {
			if e == idx {
				live++
			}
		}
		if live > 1 {
			t.Fatalf("iteration %d: %d live topoOrder entries for node %d, want at most 1", i, live, idx)
		}
	}
	assertWorldsCorrect(t, scene)
}

// TestTopologyHoleBound churns leaf attach/detach far past the tombstone
// threshold and checks topoOrder stays bounded rather than growing linearly
// with the number of churn cycles — the guarantee behind calling this safe for
// continuous spawn/destroy workloads (e.g. bullet-hole decals).
func TestTopologyHoleBound(t *testing.T) {
	scene, newMesh := topoTestScene(t)
	parent := scene.NewGroup()
	scene.Add(parent)
	scene.Sync()

	const churns = 5000
	for i := 0; i < churns; i++ {
		m := newMesh()
		parent.Add(m)
		parent.Remove(m)
	}
	if got := len(scene.topoOrder); got > 300 {
		t.Errorf("topoOrder has %d entries after %d churn cycles, want it bounded (not growing linearly with churn count)", got, churns)
	}
}

// TestTopologyNonLeafFallback moves a subtree with children (a group holding
// meshes) to a new parent and confirms the full-rebuild fallback keeps every
// descendant's attachment and world matrix correct.
func TestTopologyNonLeafFallback(t *testing.T) {
	scene, newMesh := topoTestScene(t)
	oldParent := scene.NewGroup()
	newParent := scene.NewGroup()
	scene.Add(oldParent)
	scene.Add(newParent)

	sub := scene.NewGroup()
	oldParent.Add(sub)
	child1 := newMesh()
	child2 := newMesh()
	sub.Add(child1)
	sub.Add(child2)
	scene.Sync()

	newParent.Add(sub) // sub has children: must take the slow path
	if !scene.topoDirty {
		t.Error("reparenting a subtree with children did not set topoDirty")
	}
	assertWorldsCorrect(t, scene)

	if p := sub.Parent(); p.ID() != newParent.ID() {
		t.Error("sub's parent did not update to newParent")
	}
	for _, c := range []Node{child1.Node, child2.Node} {
		if scene.flags[c.slot()]&flagAttached == 0 {
			t.Errorf("descendant %d lost flagAttached after subtree move", c.slot())
		}
	}
}

// TestTopologyDestroyLeafFastPath confirms destroying a childless node takes
// the fast path (destroyNode is only ever reached bottom-up via destroySubtree,
// so every single-node destroy is a leaf destroy — see destroyNode's comment),
// and that destroying a subtree with descendants still leaves the scene correct.
func TestTopologyDestroyLeafFastPath(t *testing.T) {
	scene, newMesh := topoTestScene(t)
	parent := scene.NewGroup()
	scene.Add(parent)
	scene.Sync()

	m := newMesh()
	parent.Add(m)
	scene.Sync()

	m.Destroy()
	if scene.topoDirty {
		t.Error("destroying a leaf node set topoDirty")
	}
	assertWorldsCorrect(t, scene)

	// Subtree destroy: bottom-up, but the top-level Destroy call still starts
	// from a node with children.
	sub := scene.NewGroup()
	parent.Add(sub)
	c1 := newMesh()
	c2 := newMesh()
	sub.Add(c1)
	sub.Add(c2)
	scene.Sync()

	sub.Destroy()
	assertWorldsCorrect(t, scene)
	if scene.flags[c1.slot()]&flagAlive != 0 || scene.flags[c2.slot()]&flagAlive != 0 {
		t.Error("subtree destroy left a descendant alive")
	}
}
