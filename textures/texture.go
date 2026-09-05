package textures

import "github.com/bluescreen10/pix/internal/ref"

// Texture is a ref-counted handle to a renderer-owned texture. Clone with Copy();
// surrender ownership with Release() (the texture is destroyed at refcount 0). It
// caches the bindless heap index so materials can write it into a record without
// reaching back into the store.
type Texture struct {
	ref   ref.Ref
	index uint32
}

// Copy returns another handle to the same texture, bumping the refcount.
func (t Texture) Copy() Texture {
	return Texture{ref: t.ref.Copy(), index: t.index}
}

// Release drops this handle's reference; the texture is destroyed when the last is released.
func (t Texture) Release() {
	t.ref.Release()
}

// Valid reports whether the underlying texture is still alive.
func (t Texture) Valid() bool {
	return t.ref.Valid()
}

// Index returns the bindless heap index used by materials.
func (t Texture) Index() uint32 {
	return t.index
}
