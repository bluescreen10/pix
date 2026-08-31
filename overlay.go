package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/shaders"
)

// overlayQuad is one solid screen-space rectangle (scalar, 32 bytes); matches Quad
// in overlay.vert. rect = x,y (top-left px), z,w (w,h px).
type overlayQuad struct {
	rect  glm.Vec4f
	color glm.RGBA32F
}

// overlayRoot matches Root in overlay.vert. Scalar layout: vec2 viewport occupies
// offset 0..8, and quads (a 64-bit reference) sits at offset 8 (already aligned) —
// no interior padding.
type overlayRoot struct {
	viewport glm.Vec2f
	quads    uint64
}

// overlay renders the debug-text HUD as instanced solid quads — one instance per
// lit bitmap-font pixel — over the frame. No atlas/texture: the font is 1-bit, so a
// per-pixel quad is crisp and fits the bindless/BDA model.
type overlay struct {
	backend gpu.Backend
	pipe    gpu.Pipeline
	quads   []overlayQuad
	quadBuf gpu.Buffer
	quadCap int
	rootBuf gpu.Buffer
}

func newOverlay(b gpu.Backend, colorFormat, depthFormat gpu.Format) *overlay {
	o := &overlay{backend: b}
	o.pipe = b.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader: shaders.OverlayVert, FragmentShader: shaders.OverlayFrag,
		Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{colorFormat},
		DepthFormat: depthFormat, DepthTest: false, DepthWrite: false, CullMode: gpu.CullNone,
	})
	o.rootBuf = b.Alloc(uint64(unsafe.Sizeof(overlayRoot{})), gpu.MemoryHost, "overlay-root")
	return o
}

// reset clears the accumulated quads (call once per frame before text calls).
func (o *overlay) reset() { o.quads = o.quads[:0] }

// text rasterizes s at (x,y) top-left in pixels, with glyph height `size` px, into
// solid quads of the given color.
func (o *overlay) text(s string, x, y, size float32, color glm.RGBA32F) {
	scale := size / 16
	cx := x
	for _, ch := range s {
		g, ok := font[int(ch)]
		if !ok {
			cx += 8 * scale // advance past spaces / unknown glyphs
			continue
		}
		for row := 0; row < 16 && row < len(g.data); row++ {
			bits := uint16(g.data[row])
			for col := 0; col < 16; col++ {
				if bits>>uint(col)&1 != 0 {
					o.quads = append(o.quads, overlayQuad{
						rect:  glm.Vec4f{cx + float32(col)*scale, y + float32(row)*scale, scale, scale},
						color: color,
					})
				}
			}
		}
		cx += float32(g.width) * scale
	}
}

// draw records the overlay draw into cl for a viewport of vpW×vpH pixels.
func (o *overlay) draw(cmd gpu.CommandBuffer, vpW, vpH float32) {
	n := len(o.quads)
	if n == 0 {
		return
	}
	if n > o.quadCap {
		if o.quadBuf.Valid() {
			o.backend.Free(o.quadBuf)
		}
		o.quadCap = n * 2
		o.quadBuf = o.backend.Alloc(uint64(o.quadCap)*uint64(unsafe.Sizeof(overlayQuad{})), gpu.MemoryHost, "overlay-quads")
	}
	copy(unsafe.Slice((*overlayQuad)(o.quadBuf.Ptr), n), o.quads)
	*(*overlayRoot)(o.rootBuf.Ptr) = overlayRoot{viewport: glm.Vec2f{vpW, vpH}, quads: o.quadBuf.Addr}

	cmd.SetPipeline(o.pipe)
	cmd.Root(o.rootBuf.Addr)
	cmd.Draw(6, uint32(n), 0, 0)
}

func (o *overlay) destroy() {
	if o.quadBuf.Valid() {
		o.backend.Free(o.quadBuf)
	}
	if o.rootBuf.Valid() {
		o.backend.Free(o.rootBuf)
	}
	o.backend.DestroyPipeline(o.pipe)
}
