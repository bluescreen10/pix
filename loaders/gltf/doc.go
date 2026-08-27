package gltf

// glTF 2.0 JSON document types — only the subset the loader consumes (static
// meshes, PBR base color + texture, node hierarchy). Skins/animations are parsed
// enough to be ignored.

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
}

type scene struct {
	Nodes []int `json:"nodes"`
}

type node struct {
	Name        string    `json:"name"`
	Children    []int     `json:"children"`
	Mesh        *int      `json:"mesh"`
	Matrix      []float32 `json:"matrix"`
	Translation []float32 `json:"translation"`
	Rotation    []float32 `json:"rotation"`
	Scale       []float32 `json:"scale"`
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
	Name                 string      `json:"name"`
	PbrMetallicRoughness *pbr        `json:"pbrMetallicRoughness"`
	NormalTexture        *textureRef `json:"normalTexture"`
	DoubleSided          bool        `json:"doubleSided"`
	AlphaMode            string      `json:"alphaMode"`
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
