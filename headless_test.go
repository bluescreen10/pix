package pix

import (
	"testing"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// TestSceneWorksWithoutARenderer is the acceptance criterion the whole separation was
// for: a Scene must be usable — built, transformed, animated, extracted — with no
// renderer, no backend, and no GPU anywhere in the test.
//
// Note what is NOT here: no NewOffscreenRenderer, no Destroy, no device at all. Before
// the split this file could not have compiled, because NewScene required a gpu.Backend
// and Sync wrote straight into device memory.
func TestSceneWorksWithoutARenderer(t *testing.T) {
	scene := scenes.New()

	group := scene.NewGroup()
	scene.Add(group)
	group.SetPosition(glm.Vec3f{10, 0, 0})

	child := scene.NewGroup()
	group.Add(child)
	child.SetPosition(glm.Vec3f{0, 5, 0})

	light := scene.AddDirectionalLight(glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 2)
	light.SetCastShadow(true)
	light.Shadow().SetSize(2048)
	scene.SetAmbient(colors.RGB32F{0.1, 0.1, 0.1})

	scene.Sync()

	// Hierarchy composes without anything GPU-side having happened.
	w := child.WorldTransform()
	if got := (glm.Vec3f{w[12], w[13], w[14]}); got != (glm.Vec3f{10, 5, 0}) {
		t.Errorf("child world position = %v, want {10 5 0}", got)
	}

	var p scenes.FramePacket
	scene.Extract(&p)

	if p.Source != scene.ID() {
		t.Errorf("packet source = %d, want %d", p.Source, scene.ID())
	}
	if len(p.Transforms.Data) == 0 {
		t.Error("packet carries no transforms")
	}
	if len(p.Lights.Data) != 1 {
		t.Fatalf("packet has %d lights, want 1", len(p.Lights.Data))
	}
	// The light's SETTINGS cross the boundary; its shadow map does not exist yet and
	// will not until a renderer allocates one.
	lp := p.Lights.Data[0]
	if lp.Kind != scenes.LightDirectional || !lp.CastsShadow || lp.ShadowSize != 2048 {
		t.Errorf("light packet = %+v, want a directional caster at 2048", lp)
	}
	if p.Environment.Ambient != (colors.RGB32F{0.1, 0.1, 0.1}) {
		t.Errorf("environment ambient = %v", p.Environment.Ambient)
	}
}

// TestHandAuthoredPacketNeedsNoScene is the other half of the claim: a FramePacket is
// plain data anyone can fill in, and the renderer's own expansion consumes it without
// ever seeing a Scene. This is the shape a custom ECS producer would emit.
func TestHandAuthoredPacketNeedsNoScene(t *testing.T) {
	p := scenes.FramePacket{Source: scenes.NewSourceID(), Frame: 1, Time: 0.5}
	at := func(x float32) glm.Mat4f {
		return glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatIdentityf, glm.Vec3f{x, 0, 0})
	}
	// Slot 0 stands in for a root nothing draws at; the mesh below is drawn at 1..3.
	p.Transforms.Data = []glm.Mat4f{glm.Mat4Identity[float32](), at(2), at(3), at(4)}
	p.Materials.Data = []materials.ID{{Pool: 0, Slot: 7, Gen: 1}}
	p.Meshes.Data = []scenes.MeshPacket{{
		ID:         scenes.ObjectID{Index: 1, Gen: 1},
		Transforms: scenes.IndexRange{First: 1, Count: 3}, // drawn at three transforms
		Geometry:   geometries.ID{Slot: 4, Gen: 1},
		Material:   0,
		Bounds:     glm.Sphere{Radius: 1},
		Flags:      scenes.RenderCastsShadow,
	}}
	p.Meshes.Revision = 1

	var dl drawList
	dl.matPipe = []uint32{3} // one pipeline for the one material
	dl.expand(&p)

	if len(dl.drawables) != 3 {
		t.Fatalf("expanded to %d draw records, want 3", len(dl.drawables))
	}
	for i, d := range dl.drawables {
		if d.geometryID != 4 || d.materialID != 7 {
			t.Errorf("record %d: geometry %d material %d, want 4 and 7", i, d.geometryID, d.materialID)
		}
		if d.flags&DrawableCastsShadow == 0 {
			t.Errorf("record %d lost its caster flag", i)
		}
		if want := uint32(1 + i); d.transformID != want {
			t.Errorf("record %d: transform %d, want %d", i, d.transformID, want)
		}
	}
	if dl.pipeBuf[0] != 3 {
		t.Errorf("record pipeline = %d, want 3", dl.pipeBuf[0])
	}

	// Shadow fitting derives its bounds from the packet too, with no producer to ask.
	// Three unit spheres at x=2,3,4 span x in [1,5]: centre 3, and a radius that
	// reaches the corner of that box.
	center, radius := casterBounds(&p)
	if center != (glm.Vec3f{3, 0, 0}) || radius < 2 {
		t.Errorf("caster bounds = %v r=%v, want centre {3 0 0} and a radius of at least 2", center, radius)
	}
}
