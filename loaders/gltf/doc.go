package gltf

// glTF 2.0 JSON document types — the subset the loader consumes: static meshes,
// PBR base color + texture, node hierarchy, skins, and animations.

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

type mesh struct {
	Name       string      `json:"name"`
	Primitives []primitive `json:"primitives"`
}

type primitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    *int           `json:"indices"`
	Material   *int           `json:"material"`
	Mode       *int           `json:"mode"`
}

type material struct {
	Name                 string              `json:"name"`
	PbrMetallicRoughness *pbr                `json:"pbrMetallicRoughness"`
	NormalTexture        *textureRef         `json:"normalTexture"`
	DoubleSided          bool                `json:"doubleSided"`
	AlphaMode            string              `json:"alphaMode"`
	Extensions           *materialExtensions `json:"extensions"`
}

type materialExtensions struct {
	Transmission *transmissionExt `json:"KHR_materials_transmission"`
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

type accessor struct {
	BufferView    *int   `json:"bufferView"`
	ByteOffset    int    `json:"byteOffset"`
	ComponentType int    `json:"componentType"`
	Count         int    `json:"count"`
	Type          string `json:"type"`
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
