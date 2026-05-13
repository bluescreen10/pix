package pix

import (
	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/glm"
)

const (
	deferredGeo uint8 = iota
	deferredMat
	deferredTex
)

type deferredFreeEntry struct {
	kind  uint8
	id    uint32
	frame uint32
}

// Disposer implementations — one per resource kind.

type geoDisposer struct{ r *Renderer }

func (d geoDisposer) dispose(id uint32)           { d.r.scheduleGeoFree(id) }
func (d geoDisposer) generation(id uint32) uint32 { return d.r.geometries.generation(id) }

type matDisposer struct{ r *Renderer }

func (d matDisposer) dispose(id uint32)           { d.r.scheduleMatFree(id) }
func (d matDisposer) generation(id uint32) uint32 { return d.r.materials.generation(id) }

type texDisposer struct{ r *Renderer }

func (d texDisposer) dispose(id uint32)           { d.r.scheduleTexFree(id) }
func (d texDisposer) generation(id uint32) uint32 { return d.r.textures.generation(id) }

// initResources initialises the resource slabs and creates the default white texture.
func (r *Renderer) initResources() {
	r.geometries = newSlab[GeometryData]()
	r.materials = newSlab[MaterialData]()
	r.textures = newSlab[TextureData]()
	r.samplerCache = make(map[Sampler]*wgpu.Sampler)

	td := NewDataTexture([]byte{255, 255, 255, 255}, 1, 1, wgpu.TextureFormatRGBA8Unorm)
	tex := r.NewTexture(td)
	r.defaultTexRef = tex.ref
}

// destroyResources releases all GPU memory synchronously at shutdown.
func (r *Renderer) destroyResources() {
	for i := range r.geometries.entries {
		if r.geometries.entries[i].alive {
			r.geometries.entries[i].val.Destroy()
		}
	}
	for i := range r.materials.entries {
		if r.materials.entries[i].alive {
			r.materials.entries[i].val.Destroy()
		}
	}
	for i := range r.textures.entries {
		if r.textures.entries[i].alive {
			r.textures.entries[i].val.Destroy()
		}
	}
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
	idx, gen := r.geometries.alloc(*data)
	ref := Ref[Geometry]{id: idx, gen: gen, refCount: rc, owner: geoDisposer{r}}
	return Geometry{renderer: r, ref: ref}
}

func (r *Renderer) scheduleGeoFree(id uint32) {
	r.geometries.free(id)
	r.deferredFree = append(r.deferredFree, deferredFreeEntry{kind: deferredGeo, id: id, frame: r.frameCount})
}

func (r *Renderer) uploadGeometry(geo *GeometryData) {
	geo.Destroy()

	if len(geo.indices) > 0 {
		buf := r.runtime.Device.CreateBufferInit(wgpu.BufferInitDescriptor{
			Label:    "index buffer",
			Contents: wgpu.ToBytes(geo.indices),
			Usage:    wgpu.BufferUsageIndex | wgpu.BufferUsageCopyDst,
		})
		geo.gpuCount = len(geo.indices)
		geo.gpuIndex = buf
	} else if len(geo.attrs) > 0 {
		geo.gpuCount = geo.attrs[0].len
	}

	geo.gpuBufs = make([]GeometryBuffer, len(geo.attrs))
	for i, a := range geo.attrs {
		buf := r.runtime.Device.CreateBufferInit(wgpu.BufferInitDescriptor{
			Label:    a.name + " buffer",
			Contents: a.data,
			Usage:    wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
		})
		geo.gpuBufs[i] = GeometryBuffer{loc: a.loc, buf: buf, version: a.version}
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

// NewPlaneGeometry creates a plane geometry resource owned by the renderer.
func (r *Renderer) NewPlaneGeometry(width, height float32, widthSegments, heightSegments int) Geometry {
	return r.allocGeometrySlot(NewPlaneGeometry(width, height, widthSegments, heightSegments))
}

// Material

func (r *Renderer) allocMaterialSlot(data *MaterialData) Material {
	rc := new(int32)
	*rc = 1
	idx, gen := r.materials.alloc(*data)
	ref := Ref[Material]{id: idx, gen: gen, refCount: rc, owner: matDisposer{r}}
	return Material{renderer: r, ref: ref}
}

func (r *Renderer) scheduleMatFree(id uint32) {
	data := r.materials.get(id)
	for i := range data.textures {
		data.textures[i].Release()
		data.textures[i] = Ref[Texture]{}
	}
	r.materials.free(id)
	r.deferredFree = append(r.deferredFree, deferredFreeEntry{kind: deferredMat, id: id, frame: r.frameCount})
}

// NewBasicMaterial creates a basic (unlit) material owned by the renderer.
func (r *Renderer) NewBasicMaterial() *BasicMaterial {
	uniform := (&Uniform{}).AddVec3("color").Build()
	data := NewMaterial("Basic Material", "basic_material.wesl", []*Uniform{uniform}, 1, false)
	mat := r.allocMaterialSlot(data)
	bm := &BasicMaterial{Material: mat}
	bm.SetColor(glm.Color3f{1, 1, 1})
	return bm
}

// NewBlinnPhongMaterial creates a Blinn-Phong lit material owned by the renderer.
func (r *Renderer) NewBlinnPhongMaterial() *BlinnPhongMaterial {
	uniform := (&Uniform{}).AddVec3("color").Build()
	data := NewMaterial("Blinn-Phong Material", "blinn_phong_material.wgsl", []*Uniform{uniform}, 1, true)
	mat := r.allocMaterialSlot(data)
	bm := &BlinnPhongMaterial{Material: mat}
	bm.SetColor(glm.Color3f{1, 1, 1})
	return bm
}

// Texture

func (r *Renderer) allocTextureSlot(data *TextureData) Texture {
	rc := new(int32)
	*rc = 1
	idx, gen := r.textures.alloc(*data)
	ref := Ref[Texture]{id: idx, gen: gen, refCount: rc, owner: texDisposer{r}}
	return Texture{renderer: r, ref: ref}
}

func (r *Renderer) scheduleTexFree(id uint32) {
	r.textures.free(id)
	r.deferredFree = append(r.deferredFree, deferredFreeEntry{kind: deferredTex, id: id, frame: r.frameCount})
}

func (r *Renderer) uploadTexture(id uint32) {
	data := r.textures.get(id)

	sampler, ok := r.samplerCache[data.sampler]
	if !ok {
		sampler = r.runtime.Device.CreateSampler(&wgpu.SamplerDescriptor{
			AddressModeU:  data.sampler.AddressModeU,
			AddressModeV:  data.sampler.AddressModeV,
			AddressModeW:  data.sampler.AddressModeW,
			MagFilter:     data.sampler.MagFilter,
			MinFilter:     data.sampler.MinFilter,
			MipmapFilter:  data.sampler.MipmapFilter,
			LodMinClamp:   data.sampler.LodMinClamp,
			LodMaxClamp:   data.sampler.LodMaxClamp,
			Compare:       data.sampler.Compare,
			MaxAnisotropy: data.sampler.MaxAnisotropy,
		})
		r.samplerCache[data.sampler] = sampler
	}

	data.gpuSampler = sampler
	data.gpuVersion = data.version

	if !data.hasPendingData() {
		return
	}

	if data.gpuRef != nil {
		data.gpuRef.Destroy()
		data.gpuRef = nil
	}

	gpuTex := r.runtime.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Texture",
		Size:          wgpu.Extent3D{Width: uint32(data.width), Height: uint32(data.height), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        data.format,
		Usage:         wgpu.TextureUsageCopyDst | wgpu.TextureUsageTextureBinding,
	})

	r.runtime.Device.GetQueue().WriteTexture(
		wgpu.TexelCopyTextureInfo{Texture: gpuTex, MipLevel: 0, Origin: wgpu.Origin3D{}},
		data.flush(),
		wgpu.TexelCopyBufferLayout{
			Offset:       0,
			BytesPerRow:  uint32(data.width) * 4,
			RowsPerImage: uint32(data.height),
		},
		wgpu.Extent3D{Width: uint32(data.width), Height: uint32(data.height), DepthOrArrayLayers: 1},
	)

	data.gpuView = gpuTex.CreateView(nil)
	data.gpuRef = gpuTex
}

// defaultTexture returns the TextureData for the default 1×1 white texture.
func (r *Renderer) defaultTexture() *TextureData {
	id := r.defaultTexRef.ID()
	td := r.textures.get(id)
	if td.gpuVersion < td.version {
		r.uploadTexture(id)
	}
	return td
}

// NewTexture registers texture data with the renderer and returns a Texture handle.
func (r *Renderer) NewTexture(data *TextureData) Texture {
	return r.allocTextureSlot(data)
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
			switch d.kind {
			case deferredGeo:
				r.geometries.get(d.id).Destroy()
				r.geometries.reclaim(d.id)
			case deferredMat:
				r.materials.get(d.id).Destroy()
				r.materials.reclaim(d.id)
			case deferredTex:
				r.textures.get(d.id).Destroy()
				r.textures.reclaim(d.id)
			}
			r.deferredFree[i] = r.deferredFree[len(r.deferredFree)-1]
			r.deferredFree = r.deferredFree[:len(r.deferredFree)-1]
		} else {
			i++
		}
	}
}
