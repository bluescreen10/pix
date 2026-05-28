package loaders

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/glm"
)

// GLTFResult holds the scene graph and animation clips loaded from a GLTF file.
type GLTFResult struct {
	Scene      *pix.Scene
	Animations []*pix.AnimationClip
}

// GLTF loads .gltf and .glb files into pix scenes.
type GLTF struct {
	r *pix.Renderer
}

// NewGLTF creates a GLTF loader backed by the given renderer.
func NewGLTF(r *pix.Renderer) *GLTF { return &GLTF{r: r} }

// Load reads a .gltf or .glb file from disk.
func (g *GLTF) Load(path string) (*GLTFResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gltf: read %q: %w", path, err)
	}
	if strings.ToLower(filepath.Ext(path)) == ".glb" {
		return g.parseGLB(data)
	}
	return g.parseGLTFBytes(data, filepath.Dir(path))
}

// LoadText parses a GLTF JSON document. All buffers must be embedded as data URIs.
func (g *GLTF) LoadText(jsonText string) (*GLTFResult, error) {
	return g.parseGLTFBytes([]byte(jsonText), "")
}

func (g *GLTF) parseGLB(data []byte) (*GLTFResult, error) {
	jsonData, binData, err := splitGLBChunks(data)
	if err != nil {
		return nil, err
	}
	l := &gltfLoader{r: g.r}
	if err := json.Unmarshal(jsonData, &l.doc); err != nil {
		return nil, fmt.Errorf("gltf: JSON: %w", err)
	}
	if err := l.loadBuffers("", binData); err != nil {
		return nil, err
	}
	return l.build()
}

func (g *GLTF) parseGLTFBytes(data []byte, baseDir string) (*GLTFResult, error) {
	l := &gltfLoader{r: g.r}
	if err := json.Unmarshal(data, &l.doc); err != nil {
		return nil, fmt.Errorf("gltf: JSON: %w", err)
	}
	if err := l.loadBuffers(baseDir, nil); err != nil {
		return nil, err
	}
	return l.build()
}

// ---- GLB binary container ----

func splitGLBChunks(data []byte) (jsonChunk, binChunk []byte, err error) {
	if len(data) < 12 {
		return nil, nil, fmt.Errorf("gltf: GLB too short")
	}
	if binary.LittleEndian.Uint32(data[0:4]) != 0x46546C67 {
		return nil, nil, fmt.Errorf("gltf: not a GLB file")
	}
	if binary.LittleEndian.Uint32(data[4:8]) != 2 {
		return nil, nil, fmt.Errorf("gltf: unsupported GLB version")
	}
	pos := 12
	for pos+8 <= len(data) {
		chunkLen := int(binary.LittleEndian.Uint32(data[pos:]))
		chunkType := binary.LittleEndian.Uint32(data[pos+4:])
		pos += 8
		if pos+chunkLen > len(data) {
			return nil, nil, fmt.Errorf("gltf: GLB chunk exceeds file")
		}
		chunk := data[pos : pos+chunkLen]
		pos += chunkLen
		switch chunkType {
		case 0x4E4F534A: // JSON
			jsonChunk = chunk
		case 0x004E4942: // BIN
			binChunk = chunk
		}
	}
	if jsonChunk == nil {
		return nil, nil, fmt.Errorf("gltf: no JSON chunk in GLB")
	}
	return jsonChunk, binChunk, nil
}

// ---- GLTF JSON document types ----

type gltfDoc struct {
	Scene       int              `json:"scene"`
	Scenes      []gltfScene      `json:"scenes"`
	Nodes       []gltfNode       `json:"nodes"`
	Meshes      []gltfMesh       `json:"meshes"`
	Materials   []gltfMaterial   `json:"materials"`
	Textures    []gltfTexture    `json:"textures"`
	Images      []gltfImage      `json:"images"`
	Samplers    []gltfSampler    `json:"samplers"`
	Accessors   []gltfAccessor   `json:"accessors"`
	BufferViews []gltfBufferView `json:"bufferViews"`
	Buffers     []gltfBuffer     `json:"buffers"`
	Skins       []gltfSkin       `json:"skins"`
	Animations  []gltfAnimation  `json:"animations"`
}

type gltfScene struct {
	Name  string `json:"name"`
	Nodes []int  `json:"nodes"`
}

type gltfNode struct {
	Name        string    `json:"name"`
	Children    []int     `json:"children"`
	Mesh        *int      `json:"mesh"`
	Skin        *int      `json:"skin"`
	Matrix      []float32 `json:"matrix"`
	Translation []float32 `json:"translation"`
	Rotation    []float32 `json:"rotation"`
	Scale       []float32 `json:"scale"`
}

type gltfMesh struct {
	Name       string          `json:"name"`
	Primitives []gltfPrimitive `json:"primitives"`
}

type gltfPrimitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    *int           `json:"indices"`
	Material   *int           `json:"material"`
	Mode       *int           `json:"mode"` // default 4 = TRIANGLES
}

type gltfMaterial struct {
	Name                 string   `json:"name"`
	PbrMetallicRoughness *gltfPbr `json:"pbrMetallicRoughness"`
	DoubleSided          bool     `json:"doubleSided"`
	AlphaMode            string   `json:"alphaMode"`
}

type gltfPbr struct {
	BaseColorFactor  []float32       `json:"baseColorFactor"`
	BaseColorTexture *gltfTextureRef `json:"baseColorTexture"`
}

type gltfTextureRef struct {
	Index    int `json:"index"`
	TexCoord int `json:"texCoord"`
}

type gltfTexture struct {
	Source  *int `json:"source"`
	Sampler *int `json:"sampler"`
}

type gltfImage struct {
	URI        string `json:"uri"`
	MimeType   string `json:"mimeType"`
	BufferView *int   `json:"bufferView"`
}

type gltfSampler struct {
	MagFilter int `json:"magFilter"`
	MinFilter int `json:"minFilter"`
	WrapS     int `json:"wrapS"`
	WrapT     int `json:"wrapT"`
}

type gltfAccessor struct {
	BufferView    *int   `json:"bufferView"`
	ByteOffset    int    `json:"byteOffset"`
	ComponentType int    `json:"componentType"`
	Count         int    `json:"count"`
	Type          string `json:"type"`
}

type gltfBufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	ByteStride int `json:"byteStride"`
}

type gltfBuffer struct {
	URI        string `json:"uri"`
	ByteLength int    `json:"byteLength"`
}

type gltfSkin struct {
	Name                string `json:"name"`
	Joints              []int  `json:"joints"`
	InverseBindMatrices *int   `json:"inverseBindMatrices"`
}

type gltfAnimation struct {
	Name     string            `json:"name"`
	Channels []gltfAnimChannel `json:"channels"`
	Samplers []gltfAnimSampler `json:"samplers"`
}

type gltfAnimChannel struct {
	Sampler int                   `json:"sampler"`
	Target  gltfAnimChannelTarget `json:"target"`
}

type gltfAnimChannelTarget struct {
	Node *int   `json:"node"`
	Path string `json:"path"`
}

type gltfAnimSampler struct {
	Input         int    `json:"input"`
	Output        int    `json:"output"`
	Interpolation string `json:"interpolation"`
}

// ---- Loader state ----

// vec4u32 is a [4]uint32 used for JOINTS_0 vertex attributes (Uint32x4).
type vec4u32 [4]uint32

type gltfLoader struct {
	r       *pix.Renderer
	doc     gltfDoc
	buffers [][]byte // raw buffer bytes, indexed by gltf buffer index
	baseDir string

	textures   []pix.Texture  // indexed by gltf texture index
	materials  []pix.Material // index 0 = default; gltf material i → index i+1
	nodes      []pix.Node     // indexed by gltf node index
	boneByNode map[int]pix.Bone
	skeletons  []pix.Skeleton // indexed by gltf skin index
}

func (l *gltfLoader) build() (*GLTFResult, error) {
	if err := l.loadTextures(); err != nil {
		return nil, err
	}
	l.loadMaterials()

	scene, err := l.buildScene()
	if err != nil {
		l.cleanup()
		return nil, err
	}

	clips := l.buildAnimations()
	l.cleanup()
	return &GLTFResult{Scene: scene, Animations: clips}, nil
}

func (l *gltfLoader) cleanup() {
	for _, t := range l.textures {
		t.Release()
	}
	for _, m := range l.materials {
		m.Release()
	}
}

// ---- Buffer loading ----

func (l *gltfLoader) loadBuffers(baseDir string, glbBin []byte) error {
	l.baseDir = baseDir
	l.buffers = make([][]byte, len(l.doc.Buffers))
	for i, b := range l.doc.Buffers {
		if b.URI == "" {
			l.buffers[i] = glbBin
			continue
		}
		data, err := l.resolveURI(b.URI)
		if err != nil {
			return fmt.Errorf("gltf: buffer %d: %w", i, err)
		}
		l.buffers[i] = data
	}
	return nil
}

func (l *gltfLoader) resolveURI(uri string) ([]byte, error) {
	if strings.HasPrefix(uri, "data:") {
		comma := strings.IndexByte(uri, ',')
		if comma < 0 {
			return nil, fmt.Errorf("invalid data URI")
		}
		return base64.StdEncoding.DecodeString(uri[comma+1:])
	}
	if l.baseDir == "" {
		return nil, fmt.Errorf("external URI %q requires a base directory", uri)
	}
	return os.ReadFile(filepath.Join(l.baseDir, uri))
}

// ---- Texture loading ----

func (l *gltfLoader) loadTextures() error {
	l.textures = make([]pix.Texture, len(l.doc.Textures))
	for i, gt := range l.doc.Textures {
		if gt.Source == nil {
			continue
		}
		pixels, w, h, err := l.decodeImage(*gt.Source)
		if err != nil {
			return fmt.Errorf("gltf: texture %d: %w", i, err)
		}
		td := pix.NewDataTexture(pixels, w, h, wgpu.TextureFormatRGBA8Unorm)
		if gt.Sampler != nil && *gt.Sampler < len(l.doc.Samplers) {
			s := l.doc.Samplers[*gt.Sampler]
			td.SetMagFilter(gltfFilterMode(s.MagFilter))
			td.SetMinFilter(gltfFilterMode(s.MinFilter))
			td.SetAddressModeU(gltfWrapMode(s.WrapS))
			td.SetAddressModeV(gltfWrapMode(s.WrapT))
		}
		l.textures[i] = l.r.NewTexture(td)
	}
	return nil
}

func (l *gltfLoader) decodeImage(idx int) (pixels []byte, w, h int, err error) {
	if idx >= len(l.doc.Images) {
		return nil, 0, 0, fmt.Errorf("image index %d out of range", idx)
	}
	gi := l.doc.Images[idx]

	var raw []byte
	if gi.BufferView != nil {
		bv := l.doc.BufferViews[*gi.BufferView]
		raw = l.buffers[bv.Buffer][bv.ByteOffset : bv.ByteOffset+bv.ByteLength]
	} else {
		raw, err = l.resolveURI(gi.URI)
		if err != nil {
			return nil, 0, 0, err
		}
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode: %w", err)
	}
	bounds := img.Bounds()
	w, h = bounds.Dx(), bounds.Dy()
	pixels = make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			i := (y*w + x) * 4
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = byte(r>>8), byte(g>>8), byte(b>>8), byte(a>>8)
		}
	}
	return pixels, w, h, nil
}

// ---- Material loading ----

func (l *gltfLoader) loadMaterials() {
	// index 0 = default white material; gltf material i → index i+1
	l.materials = make([]pix.Material, len(l.doc.Materials)+1)
	def := l.r.NewBlinnPhongMaterial()
	l.materials[0] = def.Ref()
	def.Release()

	for i, gm := range l.doc.Materials {
		m := l.r.NewBlinnPhongMaterial()
		if gm.PbrMetallicRoughness != nil {
			pbr := gm.PbrMetallicRoughness
			if len(pbr.BaseColorFactor) >= 3 {
				m.SetColor(glm.Color3f{pbr.BaseColorFactor[0], pbr.BaseColorFactor[1], pbr.BaseColorFactor[2]})
			}
			if pbr.BaseColorTexture != nil {
				ti := pbr.BaseColorTexture.Index
				if ti < len(l.textures) {
					m.SetColorMap(l.textures[ti])
				}
			}
		}
		l.materials[i+1] = m.Ref()
		m.Release()
	}
}

func (l *gltfLoader) matCopy(ptr *int) pix.Material {
	if ptr == nil || *ptr+1 >= len(l.materials) {
		return l.materials[0].Copy()
	}
	return l.materials[*ptr+1].Copy()
}

// ---- Scene building ----

func (l *gltfLoader) buildScene() (*pix.Scene, error) {
	// Identify all joint (bone) nodes across all skins.
	jointSet := make(map[int]bool)
	for _, skin := range l.doc.Skins {
		for _, j := range skin.Joints {
			jointSet[j] = true
		}
	}

	scene := pix.NewScene()
	l.boneByNode = make(map[int]pix.Bone)
	l.nodes = make([]pix.Node, len(l.doc.Nodes))

	// Pass 1: create a bare node for every gltf node (Bone or Group).
	// Mesh primitives are added as children in a later pass so that
	// animations and hierarchy always target these top-level nodes.
	for i := range l.doc.Nodes {
		if jointSet[i] {
			bone := scene.NewBone()
			l.boneByNode[i] = bone
			l.nodes[i] = bone.Node
		} else {
			l.nodes[i] = scene.NewGroup().Node
		}
	}

	// Pass 2: wire up the gltf hierarchy.
	for i, gn := range l.doc.Nodes {
		for _, childIdx := range gn.Children {
			l.nodes[i].Add(l.nodes[childIdx])
		}
	}

	// Pass 3: apply local transforms.
	for i, gn := range l.doc.Nodes {
		applyNodeTransform(l.nodes[i], gn)
	}

	// Pass 4: build skeletons from skins (bones are now available).
	l.skeletons = make([]pix.Skeleton, len(l.doc.Skins))
	for i, skin := range l.doc.Skins {
		bones := make([]pix.Bone, len(skin.Joints))
		var invBindMats []glm.Mat4f
		if skin.InverseBindMatrices != nil {
			invBindMats = l.readMat4(*skin.InverseBindMatrices)
		} else {
			invBindMats = make([]glm.Mat4f, len(bones))
			for k := range invBindMats {
				invBindMats[k] = glm.Mat4fIndentity
			}
		}
		for j, ji := range skin.Joints {
			bones[j] = l.boneByNode[ji]
		}
		l.skeletons[i] = l.r.NewSkeleton(bones, invBindMats)
	}
	defer func() {
		for i := range l.skeletons {
			l.skeletons[i].Release()
		}
	}()

	// Pass 5: create Mesh / SkinnedMesh nodes and attach under their group nodes.
	for i, gn := range l.doc.Nodes {
		if gn.Mesh == nil {
			continue
		}
		gm := l.doc.Meshes[*gn.Mesh]
		for _, prim := range gm.Primitives {
			mode := 4
			if prim.Mode != nil {
				mode = *prim.Mode
			}
			if mode != 4 { // only triangles
				continue
			}
			gd, err := l.buildGeometry(prim)
			if err != nil {
				return nil, fmt.Errorf("gltf: node %d: %w", i, err)
			}
			pixGeo := l.r.NewGeometry(gd)
			mat := l.matCopy(prim.Material)

			if gn.Skin != nil && *gn.Skin < len(l.skeletons) {
				sm := scene.NewSkinnedMesh(pixGeo, mat, l.skeletons[*gn.Skin])
				l.nodes[i].Add(sm)
			} else {
				m := scene.NewMesh(pixGeo, mat)
				l.nodes[i].Add(m)
			}
			pixGeo.Release()
			mat.Release()
		}
	}

	// Add root nodes: prefer the explicit scene list, fall back to parentless nodes.
	if l.doc.Scene >= 0 && l.doc.Scene < len(l.doc.Scenes) {
		for _, ri := range l.doc.Scenes[l.doc.Scene].Nodes {
			scene.Add(l.nodes[ri])
		}
	} else {
		for i := range l.doc.Nodes {
			if !l.nodes[i].Parent().IsValid() {
				scene.Add(l.nodes[i])
			}
		}
	}

	return scene, nil
}

func applyNodeTransform(n pix.Node, gn gltfNode) {
	if len(gn.Matrix) == 16 {
		var m glm.Mat4f
		for i, v := range gn.Matrix {
			m[i] = v
		}
		pos, rot, scale := decomposeMatrix(m)
		n.SetPosition(pos)
		n.SetRotationQuat(rot)
		n.SetScale(scale)
		return
	}
	if len(gn.Translation) >= 3 {
		n.SetPosition(glm.Vec3f{gn.Translation[0], gn.Translation[1], gn.Translation[2]})
	}
	if len(gn.Rotation) >= 4 {
		// GLTF quaternion order is [x, y, z, w] — same as pix Quatf.
		n.SetRotationQuat(glm.Quatf{gn.Rotation[0], gn.Rotation[1], gn.Rotation[2], gn.Rotation[3]})
	}
	if len(gn.Scale) >= 3 {
		n.SetScale(glm.Vec3f{gn.Scale[0], gn.Scale[1], gn.Scale[2]})
	}
}

// ---- Geometry building ----

func (l *gltfLoader) buildGeometry(prim gltfPrimitive) (*pix.GeometryData, error) {
	geo := &pix.GeometryData{}

	if prim.Indices != nil {
		geo.SetIndices(l.readIndices(*prim.Indices))
	}

	for name, accIdx := range prim.Attributes {
		switch name {
		case "POSITION":
			raw := l.accessorBytes(accIdx)
			geo.AddAttribute(pix.NewAttribute(pix.PositionAttrName, pix.PositionLocation, pix.Float32x3,
				pix.CastTo[glm.Vec3f, byte](raw)))

		case "NORMAL":
			raw := l.accessorBytes(accIdx)
			geo.AddAttribute(pix.NewAttribute(pix.NormalAttrName, pix.NormalLocation, pix.Float32x3,
				pix.CastTo[glm.Vec3f, byte](raw)))

		case "TEXCOORD_0":
			raw := l.accessorBytes(accIdx)
			geo.AddAttribute(pix.NewAttribute(pix.UVAttrName, pix.UVLocation, pix.Float32x2,
				pix.CastTo[[2]float32, byte](raw)))

		case "JOINTS_0":
			joints := l.readJoints(accIdx)
			geo.AddAttribute(pix.NewAttribute(pix.SkinIndexAttrName, pix.SkinIndexLocation, pix.Uint32x4,
				pix.CastTo[vec4u32, uint32](joints)))

		case "WEIGHTS_0":
			raw := l.accessorBytes(accIdx)
			geo.AddAttribute(pix.NewAttribute(pix.SkinWeightAttrName, pix.SkinWeightLocation, pix.Float32x4,
				pix.CastTo[[4]float32, byte](raw)))
		}
	}
	return geo, nil
}

// ---- Animation building ----

func (l *gltfLoader) buildAnimations() []*pix.AnimationClip {
	clips := make([]*pix.AnimationClip, 0, len(l.doc.Animations))
	for _, ga := range l.doc.Animations {
		clip := &pix.AnimationClip{Name: ga.Name}
		for _, ch := range ga.Channels {
			if ch.Target.Node == nil || ch.Sampler >= len(ga.Samplers) {
				continue
			}
			nodeIdx := *ch.Target.Node
			if nodeIdx >= len(l.nodes) {
				continue
			}
			samp := ga.Samplers[ch.Sampler]
			interp := gltfInterpolation(samp.Interpolation)
			times := pix.CastTo[float32](l.accessorBytes(samp.Input))

			if len(times) > 0 {
				if last := times[len(times)-1]; last > clip.Duration {
					clip.Duration = last
				}
			}

			target := l.nodes[nodeIdx]
			switch ch.Target.Path {
			case "translation":
				values := pix.CastTo[glm.Vec3f, byte](l.accessorBytes(samp.Output))
				clip.Positions = append(clip.Positions, pix.PositionTrack{
					Target: target, Times: times, Values: values, Mode: interp,
				})
			case "rotation":
				// GLTF VEC4 FLOAT quaternions: [x,y,z,w] — matches pix Quatf.
				values := pix.CastTo[glm.Quatf, byte](l.accessorBytes(samp.Output))
				clip.Rotations = append(clip.Rotations, pix.RotationTrack{
					Target: target, Times: times, Values: values, Mode: interp,
				})
			case "scale":
				values := pix.CastTo[glm.Vec3f, byte](l.accessorBytes(samp.Output))
				clip.Scales = append(clip.Scales, pix.ScaleTrack{
					Target: target, Times: times, Values: values, Mode: interp,
				})
			}
		}
		clips = append(clips, clip)
	}
	return clips
}

// ---- Accessor helpers ----

// accessorBytes returns a contiguous byte slice for the accessor's data,
// unpacking interleaved (strided) buffer views when necessary.
func (l *gltfLoader) accessorBytes(idx int) []byte {
	acc := l.doc.Accessors[idx]
	compSize := gltfComponentSize(acc.ComponentType)
	compCount := gltfTypeComponents(acc.Type)
	elemSize := compSize * compCount

	if acc.BufferView == nil {
		return make([]byte, acc.Count*elemSize)
	}

	bv := l.doc.BufferViews[*acc.BufferView]
	buf := l.buffers[bv.Buffer]
	base := bv.ByteOffset + acc.ByteOffset
	stride := bv.ByteStride
	if stride == 0 {
		stride = elemSize
	}

	if stride == elemSize {
		end := base + acc.Count*elemSize
		return buf[base:end:end]
	}

	// Interleaved — unpack elements.
	out := make([]byte, acc.Count*elemSize)
	for i := 0; i < acc.Count; i++ {
		copy(out[i*elemSize:], buf[base+i*stride:base+i*stride+elemSize])
	}
	return out
}

// readIndices converts any GLTF index accessor to []uint32.
func (l *gltfLoader) readIndices(idx int) []uint32 {
	acc := l.doc.Accessors[idx]
	raw := l.accessorBytes(idx)
	out := make([]uint32, acc.Count)
	switch acc.ComponentType {
	case 5121: // UNSIGNED_BYTE
		for i, b := range raw {
			out[i] = uint32(b)
		}
	case 5123: // UNSIGNED_SHORT
		for i := range out {
			out[i] = uint32(binary.LittleEndian.Uint16(raw[i*2:]))
		}
	default: // 5125 UNSIGNED_INT
		for i := range out {
			out[i] = binary.LittleEndian.Uint32(raw[i*4:])
		}
	}
	return out
}

// readJoints converts JOINTS_0 accessor (UNSIGNED_BYTE/SHORT/INT) to []uint32.
func (l *gltfLoader) readJoints(idx int) []uint32 {
	acc := l.doc.Accessors[idx]
	raw := l.accessorBytes(idx)
	n := acc.Count * 4 // 4 joints per vertex
	out := make([]uint32, n)
	switch acc.ComponentType {
	case 5121: // UNSIGNED_BYTE
		for i, b := range raw[:n] {
			out[i] = uint32(b)
		}
	case 5123: // UNSIGNED_SHORT
		for i := range out {
			out[i] = uint32(binary.LittleEndian.Uint16(raw[i*2:]))
		}
	default: // 5125 UNSIGNED_INT
		for i := range out {
			out[i] = binary.LittleEndian.Uint32(raw[i*4:])
		}
	}
	return out
}

// readMat4 reads a MAT4 FLOAT accessor as column-major glm.Mat4f values.
func (l *gltfLoader) readMat4(idx int) []glm.Mat4f {
	return pix.CastTo[glm.Mat4f, byte](l.accessorBytes(idx))
}

// ---- Matrix decomposition ----

func decomposeMatrix(m glm.Mat4f) (pos glm.Vec3f, rot glm.Quatf, scale glm.Vec3f) {
	pos = glm.Vec3f{m[12], m[13], m[14]}

	sx := float32(math.Sqrt(float64(m[0]*m[0] + m[1]*m[1] + m[2]*m[2])))
	sy := float32(math.Sqrt(float64(m[4]*m[4] + m[5]*m[5] + m[6]*m[6])))
	sz := float32(math.Sqrt(float64(m[8]*m[8] + m[9]*m[9] + m[10]*m[10])))
	scale = glm.Vec3f{sx, sy, sz}

	if sx > 0 && sy > 0 && sz > 0 {
		// Upper-left 3×3 rotation matrix (column-major, normalized).
		r := [9]float32{
			m[0] / sx, m[1] / sx, m[2] / sx, // col 0
			m[4] / sy, m[5] / sy, m[6] / sy, // col 1
			m[8] / sz, m[9] / sz, m[10] / sz, // col 2
		}
		rot = rotMatToQuat(r)
	} else {
		rot = glm.QuatIdentityf
	}
	return
}

// rotMatToQuat converts a 3×3 rotation matrix (stored column-major in [9]float32)
// to a quaternion [x,y,z,w] using Shepperd's method.
func rotMatToQuat(r [9]float32) glm.Quatf {
	// r[col*3+row]: r[0]=r00, r[1]=r10, r[2]=r20, r[3]=r01, r[4]=r11, r[5]=r21, r[6]=r02, r[7]=r12, r[8]=r22
	trace := r[0] + r[4] + r[8]
	var q glm.Quatf
	switch {
	case trace > 0:
		s := float32(0.5 / math.Sqrt(float64(trace+1)))
		q[3] = 0.25 / s
		q[0] = (r[5] - r[7]) * s // (r21-r12)*s
		q[1] = (r[6] - r[2]) * s // (r02-r20)*s
		q[2] = (r[1] - r[3]) * s // (r10-r01)*s
	case r[0] > r[4] && r[0] > r[8]:
		s := float32(2 * math.Sqrt(float64(1+r[0]-r[4]-r[8])))
		q[3] = (r[5] - r[7]) / s
		q[0] = 0.25 * s
		q[1] = (r[3] + r[1]) / s // (r01+r10)/s
		q[2] = (r[6] + r[2]) / s // (r02+r20)/s
	case r[4] > r[8]:
		s := float32(2 * math.Sqrt(float64(1+r[4]-r[0]-r[8])))
		q[3] = (r[6] - r[2]) / s
		q[0] = (r[3] + r[1]) / s
		q[1] = 0.25 * s
		q[2] = (r[7] + r[5]) / s // (r12+r21)/s
	default:
		s := float32(2 * math.Sqrt(float64(1+r[8]-r[0]-r[4])))
		q[3] = (r[1] - r[3]) / s
		q[0] = (r[6] + r[2]) / s
		q[1] = (r[7] + r[5]) / s
		q[2] = 0.25 * s
	}
	return q
}

// ---- GLTF enum conversions ----

func gltfComponentSize(ct int) int {
	switch ct {
	case 5120, 5121: // BYTE, UNSIGNED_BYTE
		return 1
	case 5122, 5123: // SHORT, UNSIGNED_SHORT
		return 2
	case 5125, 5126: // UNSIGNED_INT, FLOAT
		return 4
	}
	return 1
}

func gltfTypeComponents(t string) int {
	switch t {
	case "SCALAR":
		return 1
	case "VEC2":
		return 2
	case "VEC3":
		return 3
	case "VEC4":
		return 4
	case "MAT2":
		return 4
	case "MAT3":
		return 9
	case "MAT4":
		return 16
	}
	return 1
}

func gltfFilterMode(v int) wgpu.FilterMode {
	if v == 9728 { // NEAREST
		return wgpu.FilterModeNearest
	}
	return wgpu.FilterModeLinear // 9729 or default
}

func gltfWrapMode(v int) wgpu.AddressMode {
	switch v {
	case 33071:
		return wgpu.AddressModeClampToEdge
	case 33648:
		return wgpu.AddressModeMirrorRepeat
	default: // 10497 REPEAT or 0 (absent, default to REPEAT)
		return wgpu.AddressModeRepeat
	}
}

func gltfInterpolation(s string) pix.Interpolation {
	if s == "STEP" {
		return pix.InterpolationStep
	}
	return pix.InterpolationLinear // LINEAR and CUBICSPLINE both fall through to linear
}
