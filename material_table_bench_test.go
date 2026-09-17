package pix

import (
	"fmt"
	"slices"
	"testing"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
)

// BenchmarkSyncDrawListPerDrawable reconstructs the pre-Materials-table shape of
// syncDrawList — resolve a pipeline for every drawable, then compare a per-drawable id
// list — so the flat result below has something to be flat against. It is a baseline,
// not live code: nothing in the renderer walks drawables this way any more.
func BenchmarkSyncDrawListPerDrawable(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			r, err := NewOffscreenRenderer(64, 64)
			if err != nil {
				b.Fatal(err)
			}
			defer r.Destroy()
			scene := r.NewScene()
			defer scene.Destroy()

			geo := r.GeometryStore.Create(BoxGeometry(1, 1, 1))
			defer geo.Release()
			xforms := make([]glm.Mat4f, n)
			for i := range xforms {
				xforms[i] = glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatIdentityf, glm.Vec3f{float32(i), 0, 0})
			}
			scene.Add(scene.NewInstancedMesh(geo, r.NewBasicMaterial(), xforms))
			r.syncDrawList(scene)

			// What collectDrawables used to hand back: one material entry per drawable.
			perDrawable := make([]materials.Material, len(scene.drawables))
			for i := range perDrawable {
				perDrawable[i] = scene.drawMaterials[scene.drawMatIndex[i]]
			}
			pipes := make([]uint32, 0, len(perDrawable))
			// Deliberately unequal, so the comparison runs its full length every
			// iteration — the worst case the old code paid on a frame that changed
			// a pipeline, and the case that had to scan all n entries to find out.
			batched := make([]uint32, len(perDrawable))
			for i := range batched {
				batched[i] = pipelineUnresolved
			}
			var same bool

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				pipes = pipes[:0]
				for _, m := range perDrawable {
					pipes = append(pipes, r.pipelineForMaterial(m))
				}
				same = slices.Equal(pipes, batched)
			}
			b.StopTimer()
			if same {
				b.Fatal("baseline comparison unexpectedly matched")
			}
		})
	}
}

// BenchmarkSyncDrawListStatic measures the per-frame cost of a scene that changed
// nothing: the path a steady-state frame actually walks. It runs at several object
// counts with the material count held at one, so the shape of the result is the claim
// being tested — the work is proportional to distinct materials, not to drawables, and
// the numbers should stay flat as the scene grows by two orders of magnitude.
func BenchmarkSyncDrawListStatic(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			r, err := NewOffscreenRenderer(64, 64)
			if err != nil {
				b.Fatal(err)
			}
			defer r.Destroy()
			scene := r.NewScene()
			defer scene.Destroy()

			geo := r.GeometryStore.Create(BoxGeometry(1, 1, 1))
			defer geo.Release()

			xforms := make([]glm.Mat4f, n)
			for i := range xforms {
				xforms[i] = glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatIdentityf, glm.Vec3f{float32(i), 0, 0})
			}
			scene.Add(scene.NewInstancedMesh(geo, r.NewBasicMaterial(), xforms))

			r.syncDrawList(scene) // warm: build the batch layout once
			before := scene.drawList.rebuilds

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				r.syncDrawList(scene)
			}
			b.StopTimer()

			if scene.drawList.rebuilds != before {
				b.Fatalf("benchmark rebuilt the batch layout %d times; it is not measuring the static path",
					scene.drawList.rebuilds-before)
			}
		})
	}
}
