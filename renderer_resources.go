package pix

import (
	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/internal/mem"
)

type deferredFreeEntry struct {
	// destroy releases the GPU resource captured at schedule time. The slot is
	// recycled immediately by Free, so we can't look the resource up by id at
	// drain time — we hold onto its destructor instead.
	destroy func()
	frame   uint32
}

// Disposer implementations — one per resource kind.

type geoDisposer struct{ r *Renderer }

func (d geoDisposer) dispose(id uint32)           { d.r.scheduleGeoFree(id) }
func (d geoDisposer) generation(id uint32) uint32 { return d.r.geometries.Generation(id) }

type matDisposer struct{ r *Renderer }

func (d matDisposer) dispose(id uint32)           { d.r.scheduleMatFree(id) }
func (d matDisposer) generation(id uint32) uint32 { return d.r.materials.Generation(id) }

type texDisposer struct{ r *Renderer }

func (d texDisposer) dispose(id uint32)           { d.r.scheduleTexFree(id) }
func (d texDisposer) generation(id uint32) uint32 { return d.r.textures.Generation(id) }

type skelDisposer struct{ r *Renderer }

func (d skelDisposer) dispose(id uint32)           { d.r.scheduleSkeletonFree(id) }
func (d skelDisposer) generation(id uint32) uint32 { return d.r.skeletons.Generation(id) }

// initResources initialises the resource slabs and creates the default white texture.
func (r *Renderer) initResources() {
	r.geometries = mem.NewSlab[GeometryData]()
	r.materials = mem.NewSlab[MaterialData]()
	r.textures = mem.NewSlab[TextureData]()
	r.skeletons = mem.NewSlab[SkeletonData]()
	r.samplerCache = make(map[Sampler]*wgpu.Sampler)

	td := NewDataTexture([]byte{255, 255, 255, 255}, 1, 1, TextureFormatRGBA8Unorm)
	tex := r.NewTexture(td)
	r.defaultTexRef = tex.ref

	r.halfQuad = r.NewHalfQuad()
	r.quad = r.NewQuad()
}

// destroyResources releases all GPU memory synchronously at shutdown.
func (r *Renderer) destroyResources() {
	r.geometries.Range(func(g *GeometryData) { g.Destroy() })
	r.materials.Range(func(m *MaterialData) { m.Destroy() })
	r.textures.Range(func(t *TextureData) { t.Destroy() })
	r.skeletons.Range(func(s *SkeletonData) { s.Destroy() })
	for _, s := range r.samplerCache {
		s.Release()
	}
	r.samplerCache = nil
	r.deferredFree = nil
}

// Geometry

func (r *Renderer) allocGeometrySlot(data *GeometryData) Geometry {
	rc := new(int32)
	*rc = 1
	//bounds := data.BoundingSphere()
	idx, gen := r.geometries.Alloc(*data)
	ref := Ref[Geometry]{id: idx, gen: gen, refCount: rc, owner: geoDisposer{r}}
	return Geometry{renderer: r, ref: ref}
}

func (r *Renderer) scheduleGeoFree(id uint32) {
	res := *r.geometries.Get(id) // copy GPU handles out before the slot is recycled
	r.geometries.Free(id)
	r.deferredFree = append(r.deferredFree, deferredFreeEntry{destroy: res.Destroy, frame: r.frameCount})
}

func (r *Renderer) uploadGeometry(geo *GeometryData) {
	geo.Destroy()

	if len(geo.indices) > 0 {
		buf := r.runtime.Device().CreateBufferInit(wgpu.BufferInitDescriptor{
			Label:    "index buffer",
			Contents: wgpu.ToBytes(geo.indices),
			Usage:    wgpu.BufferUsageIndex | wgpu.BufferUsageCopyDst,
		})
		geo.gpuCount = len(geo.indices)
		geo.gpuIndex = buf

		// Build a line-list index buffer for wireframe: each triangle emits 3 edges.
		wireIdx := make([]uint32, 0, len(geo.indices)*2)
		for i := 0; i+2 < len(geo.indices); i += 3 {
			i0, i1, i2 := geo.indices[i], geo.indices[i+1], geo.indices[i+2]
			wireIdx = append(wireIdx, i0, i1, i1, i2, i2, i0)
		}
		geo.gpuWireframeCount = len(wireIdx)
		geo.gpuWireframeIndex = r.runtime.Device().CreateBufferInit(wgpu.BufferInitDescriptor{
			Label:    "wireframe index buffer",
			Contents: wgpu.ToBytes(wireIdx),
			Usage:    wgpu.BufferUsageIndex | wgpu.BufferUsageCopyDst,
		})
	} else if len(geo.attrs) > 0 {
		geo.gpuCount = geo.attrs[0].len
	}

	geo.gpuBufs = make([]GeometryBuffer, len(geo.attrs))
	for i, a := range geo.attrs {
		buf := r.runtime.Device().CreateBufferInit(wgpu.BufferInitDescriptor{
			Label:    a.name + " buffer",
			Contents: a.data,
			Usage:    wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
		})
		// Slot must be the sequential buffer index (position in the Buffers array),
		// not the shader @location. For non-skinned meshes loc==i, but skin
		// attributes live at loc=5,6 while occupying pipeline buffer slots 3,4.
		geo.gpuBufs[i] = GeometryBuffer{loc: i, buf: buf, version: a.version}
	}

	geo.gpuVersion = geo.version
}

// NewGeometry registers geometry data with the renderer and returns a Geometry handle.
func (r *Renderer) NewGeometry(data *GeometryData) Geometry {
	return r.allocGeometrySlot(data)
}

// NewBoxGeometry creates a box geometry resource owned by the renderer.
func (r *Renderer) NewBoxGeometry(width, height, depth float32) Geometry {
	return r.allocGeometrySlot(NewBoxGeometry(width, height, depth))
}

func (r *Renderer) NewSphereGeometry(radius float32, heightSegments, widthSegments int) Geometry {
	return r.allocGeometrySlot(NewSphereGeometry(radius, heightSegments, widthSegments))
}

// NewPlaneGeometry creates a plane geometry resource owned by the renderer.
func (r *Renderer) NewPlaneGeometry(width, height float32, widthSegments, heightSegments int) Geometry {
	return r.allocGeometrySlot(NewPlaneGeometry(width, height, widthSegments, heightSegments))
}

// Material

func (r *Renderer) allocMaterialSlot(data *MaterialData) Material {
	rc := new(int32)
	*rc = 1
	idx, gen := r.materials.Alloc(*data)
	ref := Ref[Material]{id: idx, gen: gen, refCount: rc, owner: matDisposer{r}}
	return Material{renderer: r, ref: ref}
}

func (r *Renderer) scheduleMatFree(id uint32) {
	data := r.materials.Get(id)
	for i := range data.textures {
		data.textures[i].Release()
		data.textures[i] = Ref[Texture]{}
	}
	res := *data // copy before the slot is recycled
	r.materials.Free(id)
	r.deferredFree = append(r.deferredFree, deferredFreeEntry{destroy: res.Destroy, frame: r.frameCount})
}

// Texture

func (r *Renderer) allocTextureSlot(data *TextureData) Texture {
	rc := new(int32)
	*rc = 1
	idx, gen := r.textures.Alloc(*data)
	ref := Ref[Texture]{id: idx, gen: gen, refCount: rc, owner: texDisposer{r}}
	return Texture{renderer: r, ref: ref}
}

func (r *Renderer) scheduleTexFree(id uint32) {
	res := *r.textures.Get(id)
	r.textures.Free(id)
	r.deferredFree = append(r.deferredFree, deferredFreeEntry{destroy: res.Destroy, frame: r.frameCount})
}

func (r *Renderer) getOrCreateSampler(s Sampler) *wgpu.Sampler {
	if sampler, ok := r.samplerCache[s]; ok {
		return sampler
	}
	sampler := r.runtime.CreateSampler(&wgpu.SamplerDescriptor{
		AddressModeU:  s.AddressModeU,
		AddressModeV:  s.AddressModeV,
		AddressModeW:  s.AddressModeW,
		MagFilter:     s.MagFilter,
		MinFilter:     s.MinFilter,
		MipmapFilter:  s.MipmapFilter,
		LodMinClamp:   s.LodMinClamp,
		LodMaxClamp:   s.LodMaxClamp,
		Compare:       s.Compare,
		MaxAnisotropy: s.MaxAnisotropy,
	})
	r.samplerCache[s] = sampler
	return sampler
}

func (r *Renderer) uploadTexture(id uint32) {
	tex := r.textures.Get(id)

	tex.gpuSampler = r.getOrCreateSampler(tex.sampler)
	tex.gpuVersion = tex.version

	if tex.gpuRef != nil {
		tex.gpuRef.Destroy()
		tex.gpuRef = nil
	}

	gpuTex := r.runtime.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Texture",
		Size:          wgpu.Extent3D{Width: uint32(tex.width), Height: uint32(tex.height), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        tex.format.ToWGPU(),
		Usage:         wgpu.TextureUsageCopyDst | wgpu.TextureUsageTextureBinding,
	})

	r.runtime.Queue().WriteTexture(
		wgpu.TexelCopyTextureInfo{Texture: gpuTex, MipLevel: 0, Origin: wgpu.Origin3D{}},
		tex.pixels,
		wgpu.TexelCopyBufferLayout{
			Offset:       0,
			BytesPerRow:  uint32(tex.width) * uint32(tex.format.Size()),
			RowsPerImage: uint32(tex.height),
		},
		wgpu.Extent3D{
			Width:              uint32(tex.width),
			Height:             uint32(tex.height),
			DepthOrArrayLayers: uint32(tex.layers),
		},
	)

	tex.gpuView = gpuTex.CreateView(nil)
	tex.gpuRef = gpuTex
}

// defaultTexture returns the TextureData for the default 1×1 white texture.
func (r *Renderer) defaultTexture() *TextureData {
	id := r.defaultTexRef.ID()
	td := r.textures.Get(id)
	if td.gpuVersion < td.version {
		r.uploadTexture(id)
	}
	return td
}

// NewTexture registers texture data with the renderer and returns a Texture handle.
func (r *Renderer) NewTexture(data *TextureData) Texture {
	return r.allocTextureSlot(data)
}

// Skeleton

func (r *Renderer) allocSkeletonSlot(data SkeletonData) Skeleton {
	rc := new(int32)
	*rc = 1
	idx, gen := r.skeletons.Alloc(data)
	ref := Ref[Skeleton]{id: idx, gen: gen, refCount: rc, owner: skelDisposer{r}}
	return Skeleton{renderer: r, ref: ref}
}

func (r *Renderer) scheduleSkeletonFree(id uint32) {
	res := *r.skeletons.Get(id)
	r.skeletons.Free(id)
	r.deferredFree = append(r.deferredFree, deferredFreeEntry{destroy: res.Destroy, frame: r.frameCount})
}

// NewSkeleton registers a skeleton with the renderer.
// Pass pre-computed invBindMats (e.g. from a file), or nil to fill them via SkinnedMesh.Bind.
func (r *Renderer) NewSkeleton(bones []Bone, invBindMats []glm.Mat4f) Skeleton {
	n := len(bones)
	mats := make([]glm.Mat4f, n)
	if len(invBindMats) == n {
		copy(mats, invBindMats)
	}
	data := SkeletonData{
		version:      1,
		bones:        append([]Bone(nil), bones...),
		invBindMats:  mats,
		boneMatrices: make([]glm.Mat4f, n),
	}
	return r.allocSkeletonSlot(data)
}

// drainDeferredFree releases GPU resources whose in-flight frames have completed.
const framesInFlight = 2

func (r *Renderer) drainDeferredFree() {
	if r.frameCount < framesInFlight {
		return
	}
	safe := r.frameCount - framesInFlight
	i := 0
	for i < len(r.deferredFree) {
		d := r.deferredFree[i]
		if d.frame <= safe {
			d.destroy()
			r.deferredFree[i] = r.deferredFree[len(r.deferredFree)-1]
			r.deferredFree = r.deferredFree[:len(r.deferredFree)-1]
		} else {
			i++
		}
	}
}
