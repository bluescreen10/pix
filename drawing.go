package pix

import "github.com/bluescreen10/dawn-go/wgpu"

type drawing struct {
	instanceId    uint32
	instanceCount uint32 // 1 for regular meshes, N for instanced meshes
	geo           *GeometryData
	mat           *MaterialData
	bounds        Sphere

	// ownerNode is used as a sort tiebreaker to batch draws by mesh.
	ownerNode uint32

	// Pointer into meshData.pipelines or instancedMeshData.pipelines.
	// Allows render loops to cache and reuse compiled pipelines across frames
	// without recomputing the pipeline key on each draw call.
	pipelines *[numPipelineTypes]*wgpu.RenderPipeline

	// Non-nil for skinned meshes; identifies the skeleton whose bone matrices
	// are bound at the skeleton bind group slot during rendering.
	skeleton *Skeleton
}
