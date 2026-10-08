package gltf

// glTF 2.0 JSON document types — the subset the loader consumes: static meshes,
// morph targets, PBR base color + texture, node hierarchy, skins, and animations.

type doc struct {
	Scene       int          `json:"scene"`
	Scenes      []scene      `json:"scenes"`
	Nodes       []node       `json:"nodes"`
	Meshes      []mesh       `json:"meshes"`
	Materials   []material   `json:"materials"`
	Textures    []texture    `json:"textures"`
	Images      []gltfImage  `json:"images"`
	Samplers    []sampler    `json:"samplers"`
	Accessors   []accessor   `json:"accessors"`
	BufferViews []bufferView `json:"bufferViews"`
	Buffers     []gltfBuffer `json:"buffers"`
	Skins       []skin       `json:"skins"`
	Animations  []animation  `json:"animations"`
}

type scene struct {
	Nodes []int `json:"nodes"`
}

type node struct {
	Name        string    `json:"name"`
	Children    []int     `json:"children"`
	Mesh        *int      `json:"mesh"`
	Skin        *int      `json:"skin"`
	Matrix      []float32 `json:"matrix"`
	Translation []float32 `json:"translation"`
	Rotation    []float32 `json:"rotation"`
	Scale       []float32 `json:"scale"`
	// Weights overrides the mesh's default morph target weights for this node.
	Weights    []float32       `json:"weights"`
	Extensions *nodeExtensions `json:"extensions"`
	Extras     *nodeExtras     `json:"extras"`
}

// nodeExtensions carries the MSFT_lod extension (see
// https://github.com/KhronosGroup/glTF/tree/main/extensions/2.0/Vendor/MSFT_lod):
// a node with this set is LOD level 0, and Ids lists the node indices of
// progressively lower-detail alternates (levels 1, 2, ...). Those alternate nodes
// are not expected to appear in the scene's own node list — they exist only as LOD
// data sources, referenced here, not as part of the visible hierarchy.
type nodeExtensions struct {
	MSFTLod *msftLod `json:"MSFT_lod"`
}

type msftLod struct {
	Ids []int `json:"ids"`
}

// nodeExtras carries MSFT_lod's companion screen-coverage hints — the same MSFT_lod
// node's own extras, one value per level (level 0..len-1), each the lower bound of
// the fraction of screen area that level is meant to cover before the next one takes
// over. See buildNode's MSFT_lod handling for how these are turned into pix's
// distance-based AddLOD thresholds (screen coverage and world-space distance aren't
// the same unit — see that comment for the conversion used).
//
// TargetNames names the node's mesh's morph targets. The usual place for them is the
// mesh's own extras (see meshExtras), but some exporters put them on the node.
type nodeExtras struct {
	MSFTScreenCoverage []float32 `json:"MSFT_screencoverage"`
	TargetNames        []string  `json:"targetNames"`
}

// skin is a glTF skin: joints[i] is a node index, and inverseBindMatrices[i] (a
// MAT4 FLOAT accessor, one per joint) maps a bind-pose vertex into joint i's local
// space. skeleton, if present, names the joint hierarchy's common root node.
type skin struct {
	Name                string `json:"name"`
	InverseBindMatrices *int   `json:"inverseBindMatrices"`
	Skeleton            *int   `json:"skeleton"`
	Joints              []int  `json:"joints"`
}

type animation struct {
	Name     string        `json:"name"`
	Channels []animChannel `json:"channels"`
	Samplers []animSampler `json:"samplers"`
}

type animChannel struct {
	Sampler int        `json:"sampler"`
	Target  animTarget `json:"target"`
}

type animTarget struct {
	Node *int   `json:"node"`
	Path string `json:"path"` // "translation" | "rotation" | "scale" | "weights"
}

// animSampler's input/output are accessor indices: input is a SCALAR FLOAT
// keyframe-time accessor, output is VEC3 (translation/scale) or VEC4
// (rotation, xyzw) matching the channel(s) that reference this sampler.
type animSampler struct {
	Input         int    `json:"input"`
	Output        int    `json:"output"`
	Interpolation string `json:"interpolation"` // "LINEAR" | "STEP" | "CUBICSPLINE"
}

// mesh is a glTF mesh. Weights are its morph targets' default weights, shared by
// every primitive: each primitive has the same number of targets, in the same order.
type mesh struct {
	Name       string      `json:"name"`
	Primitives []primitive `json:"primitives"`
	Weights    []float32   `json:"weights"`
	Extras     *meshExtras `json:"extras"`
}

// meshExtras carries the morph target names: not part of the glTF core, but the
// convention exporters (Blender among them) follow.
type meshExtras struct {
	TargetNames []string `json:"targetNames"`
}

// primitive is one draw of a mesh. Each of Targets is one morph target, mapping
// "POSITION", "NORMAL" and "TANGENT" to accessors of per-vertex deltas.
type primitive struct {
	Attributes map[string]int   `json:"attributes"`
	Indices    *int             `json:"indices"`
	Material   *int             `json:"material"`
	Mode       *int             `json:"mode"`
	Targets    []map[string]int `json:"targets"`
}

type material struct {
	Name                 string              `json:"name"`
	PbrMetallicRoughness *pbr                `json:"pbrMetallicRoughness"`
	NormalTexture        *textureRef         `json:"normalTexture"`
	OcclusionTexture     *occlusionRef       `json:"occlusionTexture"`
	DoubleSided          bool                `json:"doubleSided"`
	AlphaMode            string              `json:"alphaMode"`
	AlphaCutoff          *float32            `json:"alphaCutoff"`    // MASK only; default 0.5
	EmissiveFactor       []float32           `json:"emissiveFactor"` // default black
	EmissiveTexture      *textureRef         `json:"emissiveTexture"`
	Extensions           *materialExtensions `json:"extensions"`
}

type materialExtensions struct {
	Transmission *transmissionExt `json:"KHR_materials_transmission"`
	IOR          *iorExt          `json:"KHR_materials_ior"`
	Volume       *volumeExt       `json:"KHR_materials_volume"`
	// EmissiveStrength scales the emissive factor past 1, which the core spec clamps
	// it to.
	EmissiveStrength *emissiveStrengthExt `json:"KHR_materials_emissive_strength"`
}

type emissiveStrengthExt struct {
	EmissiveStrength float32 `json:"emissiveStrength"`
}

type iorExt struct {
	IOR *float32 `json:"ior"` // default 1.5
}

// volumeExt is the volume behind a transmissive surface. Its thicknessTexture is not
// read: the factor alone gives the whole surface one thickness.
type volumeExt struct {
	ThicknessFactor     float32   `json:"thicknessFactor"`     // default 0: a thin wall
	AttenuationDistance *float32  `json:"attenuationDistance"` // default infinite: nothing absorbed
	AttenuationColor    []float32 `json:"attenuationColor"`    // default white
}

type transmissionExt struct {
	TransmissionFactor *float32 `json:"transmissionFactor"` // default 0
	// TransmissionTexture's RED channel scales the factor per texel. Assets use it
	// for surfaces that are only partly glass (a cabinet's panes, a sign's window);
	// ignoring it makes the whole object transparent.
	TransmissionTexture *textureRef `json:"transmissionTexture"`
}

type pbr struct {
	BaseColorFactor          []float32   `json:"baseColorFactor"`
	BaseColorTexture         *textureRef `json:"baseColorTexture"`
	MetallicFactor           *float32    `json:"metallicFactor"`
	RoughnessFactor          *float32    `json:"roughnessFactor"`
	MetallicRoughnessTexture *textureRef `json:"metallicRoughnessTexture"`
}

type textureRef struct {
	Index    int `json:"index"`
	TexCoord int `json:"texCoord"`
}

// occlusionRef is a textureRef with how far the occlusion it holds applies.
type occlusionRef struct {
	textureRef
	Strength *float32 `json:"strength"` // default 1
}

type texture struct {
	Source  *int `json:"source"`
	Sampler *int `json:"sampler"`
}

type gltfImage struct {
	URI        string `json:"uri"`
	MimeType   string `json:"mimeType"`
	BufferView *int   `json:"bufferView"`
}

type sampler struct {
	MagFilter int `json:"magFilter"`
	MinFilter int `json:"minFilter"`
	WrapS     int `json:"wrapS"`
	WrapT     int `json:"wrapT"`
}

// accessor is a typed view of buffer data. Normalized integer components map to
// [0,1] (unsigned) or [-1,1] (signed). Sparse, if present, overrides some elements
// of the view, or of zeros when there is no buffer view.
type accessor struct {
	BufferView    *int            `json:"bufferView"`
	ByteOffset    int             `json:"byteOffset"`
	ComponentType int             `json:"componentType"`
	Normalized    bool            `json:"normalized"`
	Count         int             `json:"count"`
	Type          string          `json:"type"`
	Sparse        *accessorSparse `json:"sparse"`
}

// accessorSparse lists Count elements replaced in an accessor: Indices gives which
// (as UNSIGNED_BYTE, UNSIGNED_SHORT or UNSIGNED_INT), Values their new values, in the
// accessor's own component type.
type accessorSparse struct {
	Count   int `json:"count"`
	Indices struct {
		BufferView    int `json:"bufferView"`
		ByteOffset    int `json:"byteOffset"`
		ComponentType int `json:"componentType"`
	} `json:"indices"`
	Values struct {
		BufferView int `json:"bufferView"`
		ByteOffset int `json:"byteOffset"`
	} `json:"values"`
}

type bufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	ByteStride int `json:"byteStride"`
}

type gltfBuffer struct {
	URI        string `json:"uri"`
	ByteLength int    `json:"byteLength"`
}
