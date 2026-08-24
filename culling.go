package pix

import (
	"unsafe"

	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
)

// VisibleBinding is the compacted visible-instances buffer in the pull pipeline's
// instance set (alongside ModelsBinding=0, DrawablesBinding=1).
const VisibleBinding = 2

// shadowInstanceSet is the instance set's group index in a (pull) shadow pipeline
// layout: [material(0), instance(1), geometry(2)].
const shadowInstanceSet = 1

// regionAlign is the per-batch region granularity in the visible buffer, in
// entries. 64 u32 = 256 bytes = minStorageBufferOffsetAlignment.
const regionAlign = 64

// Cull views: the main camera, one per directional/spot shadow, plus a single
// reusable slot for point-shadow cube faces. Each point light renders its 6
// faces serially (own encoder + submit per face), so one slot — reused per face
// — suffices; the faces never overlap in flight.
const (
	// mainView is the camera view index.
	mainView = 0
	// pointShadowView is the shared slot reused by every point-light cube face.
	pointShadowView = 1 + MaxDirectionalLights + MaxSpotLights
	cullViewCount   = pointShadowView + 1
)

// drawIndexedIndirectArgs mirrors a WebGPU indexed indirect draw. indexCount and
// firstIndex come from the geometry (a batch is one geometry); instanceCount is
// written by the cull pass. baseVertex/firstInstance stay 0 (the shader adds the
// per-attribute base, and visible[] is bound at the batch's region).
type drawIndexedIndirectArgs struct {
	indexCount    uint32
	instanceCount uint32
	firstIndex    uint32
	baseVertex    int32
	firstInstance uint32
}

type renderBatch struct {
	materialID uint32
	geometryID uint32
	regionBase uint32
	regionCap  uint32
}

// cullParams is the compute uniform: the 6 frustum planes, drawable count, and a
// casters-only flag (set for shadow views). 112 bytes.
type cullParams struct {
	planes      [6]glm.Vec4f
	count       uint32
	castersOnly uint32
	_           [2]uint32
}

// cullView is one frustum's cull output: its own params, indirect args, compacted
// visible buffer and the bind groups referencing them. Batches (and their region
// layout) are shared across views.
type cullView struct {
	paramsBuf   gpu.Buffer
	indirectBuf gpu.Buffer
	visibleBuf  gpu.Buffer
	cullBG      *wgpu.BindGroup
	batchBGs    []*wgpu.BindGroup
}

type gpuCuller struct {
	r *Renderer

	// Shared batch layout (structural).
	batches       []renderBatch
	regionBases   []uint32
	indirectTmpl  []drawIndexedIndirectArgs
	regionBaseBuf gpu.Buffer
	visibleCap    uint32

	pipeline       *wgpu.ComputePipeline
	bgLayout       *wgpu.BindGroupLayout
	instanceLayout *wgpu.BindGroupLayout

	views [cullViewCount]cullView
	dirty bool
}

func newGPUCuller(r *Renderer) *gpuCuller {
	c := &gpuCuller{r: r}

	c.bgLayout = r.gpu.CreateBindGroupLayout(wgpu.BindGroupLayoutDescriptor{
		Label:   "Cull BGL",
		Entries: cullBindGroupLayoutEntries(),
	})
	c.instanceLayout = r.gpu.CreateBindGroupLayout(wgpu.BindGroupLayoutDescriptor{
		Label: "Cull Instance BGL",
		Entries: []wgpu.BindGroupLayoutEntry{
			storageEntry(ModelsBinding, wgpu.ShaderStageVertex, uint64(unsafe.Sizeof(glm.Mat4f{}))),
			storageEntry(DrawablesBinding, wgpu.ShaderStageVertex, uint64(unsafe.Sizeof(drawable{}))),
			storageEntry(VisibleBinding, wgpu.ShaderStageVertex, 4),
		},
	})

	for i := range c.views {
		c.views[i].paramsBuf = r.gpu.CreateBuffer(gpu.BufferDescriptor{
			Label: "Cull Params",
			Size:  uint64(unsafe.Sizeof(cullParams{})),
			Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		})
	}

	module := r.compileShader(r.gpu.Device(), "cull.wesl", nil)
	layout := r.gpu.CreatePipelineLayout(wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{c.bgLayout},
	})
	defer layout.Release()
	c.pipeline = r.gpu.CreateComputePipeline(wgpu.ComputePipelineDescriptor{
		Layout:  layout,
		Compute: wgpu.ComputeState{Module: module, EntryPoint: "main"},
	})
	return c
}

func storageEntry(binding uint32, vis wgpu.ShaderStage, minSize uint64) wgpu.BindGroupLayoutEntry {
	return wgpu.BindGroupLayoutEntry{
		Binding:    binding,
		Visibility: vis,
		Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage, MinBindingSize: minSize},
	}
}

func cullBindGroupLayoutEntries() []wgpu.BindGroupLayoutEntry {
	rw := func(binding uint32) wgpu.BindGroupLayoutEntry {
		return wgpu.BindGroupLayoutEntry{
			Binding:    binding,
			Visibility: wgpu.ShaderStageCompute,
			Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeStorage},
		}
	}
	return []wgpu.BindGroupLayoutEntry{
		storageEntry(0, wgpu.ShaderStageCompute, uint64(unsafe.Sizeof(drawable{}))),
		storageEntry(1, wgpu.ShaderStageCompute, uint64(unsafe.Sizeof(glm.Mat4f{}))),
		{Binding: 2, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform}},
		rw(3),
		storageEntry(4, wgpu.ShaderStageCompute, 4),
		rw(5),
	}
}

// rebuildBatches groups drawables by (material, geometry), assigns batchIDs, lays
// out per-batch regions, and (re)sizes every view's buffers. Called on structural
// change.
func (c *gpuCuller) rebuildBatches(scene *Scene) {
	type key struct{ mat, geo uint32 }
	index := map[key]uint32{}
	c.batches = c.batches[:0]
	counts := []uint32{}

	for i := range scene.drawables {
		d := &scene.drawables[i]
		k := key{d.materialID, d.geometryID}
		bid, ok := index[k]
		if !ok {
			bid = uint32(len(c.batches))
			index[k] = bid
			c.batches = append(c.batches, renderBatch{materialID: d.materialID, geometryID: d.geometryID})
			counts = append(counts, 0)
		}
		d.batchID = bid
		counts[bid]++
	}

	c.regionBases = c.regionBases[:0]
	c.indirectTmpl = c.indirectTmpl[:0]
	var base uint32
	for bid := range c.batches {
		b := &c.batches[bid]
		padded := (counts[bid] + regionAlign - 1) &^ (regionAlign - 1)
		b.regionBase = base
		b.regionCap = padded
		c.regionBases = append(c.regionBases, base)
		c.indirectTmpl = append(c.indirectTmpl, drawIndexedIndirectArgs{indexCount: c.r.geometries.indexCount(b.geometryID), firstIndex: c.r.geometries.indexBase(b.geometryID)})
		base += padded
	}
	c.visibleCap = base
	if c.visibleCap == 0 {
		c.visibleCap = regionAlign
	}

	c.ensureBuffers()
	if len(c.regionBases) > 0 {
		c.r.gpu.WriteBuffer(c.regionBaseBuf, 0, wgpu.ToBytes(c.regionBases))
	}
	c.dirty = true
}

func (c *gpuCuller) ensureBuffers() {
	n := uint32(len(c.batches))
	if n == 0 {
		n = 1
	}
	c.regionBaseBuf = c.recreate(c.regionBaseBuf, uint64(n)*4, gpu.BufferUsageStorage|gpu.BufferUsageCopyDst, "Region Base")
	for i := range c.views {
		v := &c.views[i]
		v.indirectBuf = c.recreate(v.indirectBuf, uint64(n)*uint64(unsafe.Sizeof(drawIndexedIndirectArgs{})),
			gpu.BufferUsageStorage|gpu.BufferUsageIndirect|gpu.BufferUsageCopyDst, "Indirect Args")
		v.visibleBuf = c.recreate(v.visibleBuf, uint64(c.visibleCap)*4,
			gpu.BufferUsageStorage|gpu.BufferUsageCopyDst, "Visible Instances")
	}
}

func (c *gpuCuller) recreate(old gpu.Buffer, size uint64, usage gpu.BufferUsage, label string) gpu.Buffer {
	if old.Valid() {
		c.r.gpu.ReleaseBuffer(old)
	}
	return c.r.gpu.CreateBuffer(gpu.BufferDescriptor{Label: label, Size: size, Usage: usage})
}

func (c *gpuCuller) ensureBindGroups() {
	if !c.dirty {
		return
	}
	r := c.r
	for i := range c.views {
		v := &c.views[i]
		if v.cullBG != nil {
			v.cullBG.Release()
		}
		v.cullBG = r.gpu.CreateBindGroup(wgpu.BindGroupDescriptor{
			Label:  "Cull Bind Group",
			Layout: c.bgLayout,
			Entries: []wgpu.BindGroupEntry{
				entryWhole(0, r.gpu.RawBuffer(r.drawableBuf)),
				entryWhole(1, r.gpu.RawBuffer(r.modelBuf)),
				entryWhole(2, r.gpu.RawBuffer(v.paramsBuf)),
				entryWhole(3, r.gpu.RawBuffer(v.indirectBuf)),
				entryWhole(4, r.gpu.RawBuffer(c.regionBaseBuf)),
				entryWhole(5, r.gpu.RawBuffer(v.visibleBuf)),
			},
		})

		for _, bg := range v.batchBGs {
			bg.Release()
		}
		v.batchBGs = v.batchBGs[:0]
		visible := r.gpu.RawBuffer(v.visibleBuf)
		for bid := range c.batches {
			b := &c.batches[bid]
			v.batchBGs = append(v.batchBGs, r.gpu.CreateBindGroup(wgpu.BindGroupDescriptor{
				Layout: c.instanceLayout,
				Entries: []wgpu.BindGroupEntry{
					entryWhole(ModelsBinding, r.gpu.RawBuffer(r.modelBuf)),
					entryWhole(DrawablesBinding, r.gpu.RawBuffer(r.drawableBuf)),
					{Binding: VisibleBinding, Buffer: visible, Offset: uint64(b.regionBase) * 4, Size: uint64(b.regionCap) * 4},
				},
			}))
		}
	}
	c.dirty = false
}

func entryWhole(binding uint32, buf *wgpu.Buffer) wgpu.BindGroupEntry {
	return wgpu.BindGroupEntry{Binding: binding, Buffer: buf, Offset: 0, Size: wgpu.WholeSize}
}

// prep makes sure every batch's geometry and material are GPU-ready.
func (c *gpuCuller) prep() {
	for bid := range c.batches {
		b := &c.batches[bid]
		if err := prepareMaterial(c.r.gpu.Device(), c.r.materials.Get(b.materialID), c.r); err != nil {
			c.r.logger.Error("cull: prepare material failed")
		}
	}
}

// cullView encodes the reset + frustum-cull compute for one view.
func (c *gpuCuller) cullView(encoder *wgpu.CommandEncoder, viewIdx int, frustum Frustum, castersOnly bool, count uint32) {
	if len(c.batches) == 0 || count == 0 {
		return
	}
	c.ensureBindGroups()
	v := &c.views[viewIdx]

	var p cullParams
	for i := 0; i < 6; i++ {
		p.planes[i] = frustum[i]
	}
	p.count = count
	if castersOnly {
		p.castersOnly = 1
	}
	c.r.gpu.WriteBuffer(v.paramsBuf, 0, wgpu.ToBytes([]cullParams{p}))
	c.r.gpu.WriteBuffer(v.indirectBuf, 0, wgpu.ToBytes(c.indirectTmpl))

	pass := encoder.BeginComputePass(nil)
	pass.SetPipeline(c.pipeline)
	pass.SetBindGroup(0, v.cullBG, nil)
	pass.DispatchWorkgroups((count+63)/64, 1, 1)
	pass.End()
	pass.Release()
}

// drawMain issues the main camera pass: one indirect draw per batch, each with
// its own material/pipeline. Caller has bound the global and geometry sets.
func (c *gpuCuller) drawMain(pass *wgpu.RenderPassEncoder, renderTarget, depthTarget *wgpu.Texture) {
	r := c.r
	v := &c.views[mainView]
	indirect := r.gpu.RawBuffer(v.indirectBuf)
	pass.SetIndexBuffer(r.geometries.IndexBuffer(), wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
	var curPipeline *wgpu.RenderPipeline
	var curMat *wgpu.BindGroup

	for bid := range c.batches {
		b := &c.batches[bid]
		geo := r.geometries.Get(b.geometryID)
		mat := r.materials.Get(b.materialID)

		if p := r.getPipeline(mat, geo, renderTarget, depthTarget); p != curPipeline {
			pass.SetPipeline(p)
			curPipeline = p
		}
		if mat.gpuBindGroup != curMat {
			pass.SetBindGroup(MaterialSet, mat.gpuBindGroup, nil)
			curMat = mat.gpuBindGroup
		}
		pass.SetBindGroup(InstanceSet, v.batchBGs[bid], nil)
		pass.DrawIndexedIndirect(indirect, uint64(bid)*uint64(unsafe.Sizeof(drawIndexedIndirectArgs{})))
	}
}

// drawShadow issues a shadow pass for viewIdx using shadowMat's pipeline for every
// batch. Caller has bound shadowMat (group 0) and the geometry set (group 2).
func (c *gpuCuller) drawShadow(pass *wgpu.RenderPassEncoder, viewIdx int, shadowMat *MaterialData, depthTarget *wgpu.Texture) {
	r := c.r
	v := &c.views[viewIdx]
	indirect := r.gpu.RawBuffer(v.indirectBuf)
	pass.SetIndexBuffer(r.geometries.IndexBuffer(), wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
	var curPipeline *wgpu.RenderPipeline

	for bid := range c.batches {
		geo := r.geometries.Get(c.batches[bid].geometryID)
		if p := r.getPipeline(shadowMat, geo, nil, depthTarget); p != curPipeline {
			pass.SetPipeline(p)
			curPipeline = p
		}
		pass.SetBindGroup(shadowInstanceSet, v.batchBGs[bid], nil)
		pass.DrawIndexedIndirect(indirect, uint64(bid)*uint64(unsafe.Sizeof(drawIndexedIndirectArgs{})))
	}
}

func (c *gpuCuller) Destroy() {
	if c.pipeline != nil {
		c.pipeline.Release()
	}
	if c.bgLayout != nil {
		c.bgLayout.Release()
	}
	if c.instanceLayout != nil {
		c.instanceLayout.Release()
	}
	if c.regionBaseBuf.Valid() {
		c.r.gpu.ReleaseBuffer(c.regionBaseBuf)
	}
	for i := range c.views {
		v := &c.views[i]
		if v.cullBG != nil {
			v.cullBG.Release()
		}
		for _, bg := range v.batchBGs {
			bg.Release()
		}
		for _, b := range []gpu.Buffer{v.paramsBuf, v.indirectBuf, v.visibleBuf} {
			if b.Valid() {
				c.r.gpu.ReleaseBuffer(b)
			}
		}
	}
}
