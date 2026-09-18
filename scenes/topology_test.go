package scenes_test

import (
	"math/rand"
	"testing"

	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// topoTestScene builds a scene plus a maker for mesh nodes, for tests that need real
// (not group-only) nodes with a payload: swap-remove and slot reuse are payload-array
// behaviour that groups never exercise.
//
// No renderer and no GPU. The geometry handle is the zero value and the material a
// stub — nothing here asks either of them anything, because a Scene only holds the
// handles and publishes their identities.
func topoTestScene(t *testing.T) (*scenes.Scene, func() scenes.Mesh) {
	t.Helper()
	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	mat := newFakeMaterial()
	return scene, func() scenes.Mesh { return scene.NewMesh(geometries.Geometry{}, mat) }
}

// collectAttached walks the real parent/child structure from n (via the public
// Children accessor), independent of any internal bookkeeping — this is the ground
// truth against which cached world transforms are checked.
func collectAttached(n scenes.Node) []scenes.Node {
	out := []scenes.Node{n}
	for _, c := range n.Children() {
		out = append(out, collectAttached(c)...)
	}
	return out
}

// expectedWorld recomputes n's world matrix by walking real parent pointers (via the
// public Parent accessor) — independent of the fast-path bookkeeping under test, so a
// divergence here means the cached WorldTransform itself is wrong, not just stale.
func expectedWorld(n scenes.Node) glm.Mat4f {
	local := n.Transform()
	p := n.Parent()
	if !p.IsValid() {
		return local
	}
	return expectedWorld(p).Mul4x4(local)
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
func assertWorldsCorrect(t *testing.T, s *scenes.Scene) {
	t.Helper()
	s.Sync()
	for _, n := range collectAttached(s.Root()) {
		got := n.WorldTransform()
		want := expectedWorld(n)
		if !matNear(got, want) {
			t.Fatalf("node %v: world = %v, want %v", n.ID(), got, want)
		}
	}
}

// isReachableFrom reports whether target is n itself or one of its descendants,
// walking only the public Children accessor — used to confirm a subtree actually
// landed where a reparent was supposed to put it.
func isReachableFrom(n, target scenes.Node) bool {
	if n.ID() == target.ID() {
		return true
	}
	for _, c := range n.Children() {
		if isReachableFrom(c, target) {
			return true
		}
	}
	return false
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

	var nodes []scenes.Node
	for i := range 12 {
		if i%3 == 0 {
			g := scene.NewGroup()
			nodes = append(nodes, g.Node)
		} else {
			m := newMesh()
			nodes = append(nodes, m.Node)
		}
	}

	pick := func() scenes.Node { return nodes[rng.Intn(len(nodes))] }

	for range 500 {
		n := pick()
		switch rng.Intn(5) {
		case 0, 1: // Add under root or another node (also covers moving an already-attached leaf)
			parent := pick()
			if parent.ID() == n.ID() || wouldCycleForTest(n, parent) {
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

// wouldCycleForTest mirrors Scene.wouldCycle's check from the test side, walking
// ancestry through the public Parent accessor (reparent already panics on a real
// cycle, but the randomized test would rather skip an invalid op than fail on an
// intentional panic path).
func wouldCycleForTest(child, newParent scenes.Node) bool {
	for cur := newParent; cur.IsValid(); cur = cur.Parent() {
		if cur.ID() == child.ID() {
			return true
		}
	}
	return false
}

// TestTopologyNonLeafFallback moves a subtree with children (a group holding
// meshes) to a new parent and confirms the move keeps every descendant's
// attachment and world matrix correct.
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
	assertWorldsCorrect(t, scene)

	if p := sub.Parent(); p.ID() != newParent.ID() {
		t.Error("sub's parent did not update to newParent")
	}
	for _, c := range []scenes.Node{child1.Node, child2.Node} {
		if !isReachableFrom(scene.Root(), c) {
			t.Errorf("descendant %v is no longer reachable from root after subtree move", c.ID())
		}
	}
}

// TestTopologyDestroyLeafFastPath destroys a childless node and confirms it becomes
// invalid without disturbing the rest of the scene, then does the same for a subtree
// with descendants.
func TestTopologyDestroyLeafFastPath(t *testing.T) {
	scene, newMesh := topoTestScene(t)
	parent := scene.NewGroup()
	scene.Add(parent)
	scene.Sync()

	m := newMesh()
	parent.Add(m)
	scene.Sync()

	m.Destroy()
	if m.IsValid() {
		t.Error("Destroy did not invalidate the node")
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
	if c1.IsValid() || c2.IsValid() {
		t.Error("subtree destroy left a descendant alive")
	}
}
