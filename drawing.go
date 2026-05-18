package pix

import "github.com/bluescreen10/dawn-go/wgpu"

type drawing struct {
	instanceId    uint32
	instanceCount uint32 // 1 for regular meshes, N for instanced meshes
	geo           *GeometryData
	mat           *MaterialData
	bounds        Sphere

	// For instanced meshes: ownerNode identifies the private GPU resource and
	// isInstanced selects the private bind group over the shared objectsBuf.
	ownerNode   uint32
	isInstanced bool

	// Pointer into meshData.pipelines or instancedMeshData.pipelines.
	// Allows render loops to cache and reuse compiled pipelines across frames
	// without recomputing the pipeline key on each draw call.
	pipelines *[numPipelineTypes]*wgpu.RenderPipeline
}
