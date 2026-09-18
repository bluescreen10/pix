package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
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

// fakeProducer is a scenes.Producer backed by nothing but a hand-filled FramePacket —
// exactly the "ECS, an editor, a test fixture with no world behind it at all" case
// scenes.Producer's doc comment calls out. It proves the renderer consumes the packet
// contract itself, not anything specific to *scenes.Scene.
type fakeProducer struct {
	id     scenes.SourceID
	packet scenes.FramePacket
}

func (f *fakeProducer) ID() scenes.SourceID {
	return f.id
}

func (f *fakeProducer) Extract(p *scenes.FramePacket) {
	*p = f.packet
}

func (f *fakeProducer) Rendered() {}

// TestHandAuthoredPacketNeedsNoScene is the other half of the claim: a FramePacket is
// plain data anyone can fill in, and the renderer draws from it without ever seeing a
// Scene. Three transforms reference one mesh entry, so a correct expansion must draw
// three separate boxes; rendering them spread along X and checking the lit silhouette
// spans that whole width is the public-API proxy for "three records were produced, not
// one" — the renderer holds no other way to observe the packet's internal expansion.
func TestHandAuthoredPacketNeedsNoScene(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(160, 40)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New() // only used to allocate real geometry/material resources
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{1, 1, 1})

	geo := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	defer geo.Release()
	mat := r.NewBasicMaterial()
	mat.SetColor(colors.RGBA32F{0, 1, 0, 1})
	defer mat.Release()

	at := func(x float32) glm.Mat4f {
		return glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatfIdentity, glm.Vec3f{x, 0, 0})
	}
	p := scenes.FramePacket{Source: scenes.NewSourceID(), Frame: 1}
	// Slot 0 stands in for a root nothing draws at; the mesh below is drawn at 1..3.
	p.Transforms.Data = []glm.Mat4f{glm.Mat4Identity[float32](), at(-6), at(0), at(6)}
	p.Materials.Data = []materials.ID{mat.ID()}
	p.Meshes.Data = []scenes.MeshPacket{{
		ID:         scenes.ObjectID{Index: 1, Gen: 1},
		Transforms: scenes.IndexRange{First: 1, Count: 3}, // drawn at three transforms
		Geometry:   geo.ID(),
		Material:   0,
		Bounds:     geo.BoundingSphere(),
	}}
	p.Meshes.Revision = 1

	producer := &fakeProducer{id: p.Source, packet: p}

	cam := cameras.NewPerspectiveCamera(60, 4, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 6})
	r.Render(producer, cam)

	px := r.Pixels()
	const w, h = 160, 40
	litCol := make([]bool, w)
	for y := range h {
		for x := range w {
			i := (y*w + x) * 4
			if px[i+1] > 100 && px[i] < 60 {
				litCol[x] = true
			}
		}
	}
	first, last := -1, -1
	for x, lit := range litCol {
		if lit {
			if first < 0 {
				first = x
			}
			last = x
		}
	}
	if first < 0 {
		t.Fatal("hand-authored packet produced no visible geometry")
	}
	// Three boxes 4 units apart, spread across a 4:1 aspect frame, should light up a
	// third or more of the frame's width; a single box (only one drawable record
	// actually produced) would cover a small fraction of it.
	if span := last - first; span < w/3 {
		t.Fatalf("lit span = %d px (columns %d..%d), want > %d — looks like fewer than 3 boxes rendered", span, first, last, w/3)
	}
}
