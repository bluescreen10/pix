package scenes_test

import "github.com/bluescreen10/pix/materials"

// fakeMaterial is a materials.Material that owns nothing. Scene tests need mesh nodes
// with a real payload — swap-remove and slot reuse are payload-array behaviour, not
// group behaviour — but a real material needs a pool, and a pool needs a GPU backend.
// Nothing in this package ever asks a material anything: Scene only holds the handle
// and publishes its ID, so a stub is enough to exercise every path here.
type fakeMaterial struct{ id materials.ID }

func (m *fakeMaterial) Copy() materials.Material {
	return m
}

func (m *fakeMaterial) Release() {}

func (m *fakeMaterial) IsValid() bool {
	return true
}

func (m *fakeMaterial) ID() materials.ID {
	return m.id
}

func (m *fakeMaterial) Pool() *materials.Pool {
	return nil
}

var nextFakeSlot uint32

func newFakeMaterial() *fakeMaterial {
	nextFakeSlot++
	return &fakeMaterial{id: materials.ID{Slot: nextFakeSlot, Gen: 1}}
}
