// Package gltf loads .gltf / .glb assets into a pix.Scene. It depends only on
// the renderer (pix) and math (glm) — never the gpu backend — so the same
// loader works against any backend the renderer runs on. Static meshes only for
// now: node hierarchy transforms are flattened to world matrices at load time;
// skinning and animation (which need a live node hierarchy) are not yet wired.
package gltf

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"math"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/glm"
)

// Load reads a .gltf or .glb file, creates its geometries/materials/textures on the
// renderer, and builds its node hierarchy (with Mesh nodes) into the scene. Returns
// the number of mesh nodes added.
func Load(r *pix.Renderer, scene *pix.Scene, path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("gltf: read %q: %w", path, err)
	}
	l := &loader{renderer: r, scene: scene}
	if strings.ToLower(filepath.Ext(path)) == ".glb" {
		jsonData, binData, err := splitGLB(data)
		if err != nil {
			return 0, err
		}
		if err := json.Unmarshal(jsonData, &l.doc); err != nil {
			return 0, fmt.Errorf("gltf: JSON: %w", err)
		}
		if err := l.loadBuffers("", binData); err != nil {
			return 0, err
		}
	} else {
		if err := json.Unmarshal(data, &l.doc); err != nil {
			return 0, fmt.Errorf("gltf: JSON: %w", err)
		}
		if err := l.loadBuffers(filepath.Dir(path), nil); err != nil {
			return 0, err
		}
	}
	return l.build()
}

type loader struct {
	renderer *pix.Renderer
	scene    *pix.Scene
	doc      doc
	buffers  [][]byte
	baseDir  string

	textures  []pix.Texture  // per gltf texture (index used by materials)
	materials []*pix.PBRMaterial // [0]=default; gltf material i -> [i+1]
	nodes     []pix.Node     // per gltf node
	added     int
}

func (l *loader) build() (int, error) {
	l.loadTextures()
	l.loadMaterials()

	// Create a node per glTF node, set its local transform, then wire the hierarchy
	// and attach a Mesh child per primitive — the scene graph computes world matrices.
	l.nodes = make([]pix.Node, len(l.doc.Nodes))
	for i := range l.doc.Nodes {
		l.nodes[i] = l.scene.NewGroup().Node
		setLocal(l.nodes[i], l.doc.Nodes[i])
	}
	for i, gn := range l.doc.Nodes {
		for _, c := range gn.Children {
			l.nodes[i].Add(l.nodes[c])
		}
	}
	for i, gn := range l.doc.Nodes {
		if gn.Mesh != nil {
			l.addMesh(l.nodes[i], *gn.Mesh)
		}
	}
	for _, ri := range l.roots() {
		l.scene.Add(l.nodes[ri])
	}

	// Drop the loader's references now that ownership has transferred: each Mesh holds
	// its own material copy, and each material holds its own color-map texture copy.
	for _, m := range l.materials {
		m.Release()
	}
	for _, t := range l.textures {
		if t.Valid() {
			t.Release()
		}
	}
	return l.added, nil
}

func (l *loader) roots() []int {
	if l.doc.Scene >= 0 && l.doc.Scene < len(l.doc.Scenes) {
		return l.doc.Scenes[l.doc.Scene].Nodes
	}
	child := make([]bool, len(l.doc.Nodes))
	for _, n := range l.doc.Nodes {
		for _, c := range n.Children {
			child[c] = true
		}
	}
	var roots []int
	for i := range l.doc.Nodes {
		if !child[i] {
			roots = append(roots, i)
		}
	}
	return roots
}

func (l *loader) addMesh(parent pix.Node, meshIdx int) {
	gm := l.doc.Meshes[meshIdx]
	for _, prim := range gm.Primitives {
		mode := 4
		if prim.Mode != nil {
			mode = *prim.Mode
		}
		if mode != 4 { // triangles only
			continue
		}
		data := l.buildData(prim)
		if len(data.Positions) == 0 {
			continue
		}
		geo := l.renderer.NewGeometry(data)
		m := l.scene.NewMesh(geo, l.materialFor(prim.Material))
		geo.Release() // the mesh holds its own copy
		parent.Add(m)
		l.added++
	}
}

func (l *loader) buildData(prim primitive) pix.Data {
	var d pix.Data
	if prim.Indices != nil {
		d.Indices = l.readIndices(*prim.Indices)
	}
	for name, acc := range prim.Attributes {
		switch name {
		case "POSITION":
			d.Positions = castTo[glm.Vec3f](l.accessorBytes(acc))
		case "NORMAL":
			d.Normals = castTo[glm.Vec3f](l.accessorBytes(acc))
		case "TEXCOORD_0":
			d.UVs = castTo[glm.Vec2f](l.accessorBytes(acc))
		case "COLOR_0":
			if a := l.doc.Accessors[acc]; a.ComponentType == 5126 { // FLOAT only
				if a.Type == "VEC4" {
					d.Colors = castTo[glm.Vec4f](l.accessorBytes(acc))
				} else if a.Type == "VEC3" {
					v3 := castTo[glm.Vec3f](l.accessorBytes(acc))
					d.Colors = make([]glm.Vec4f, len(v3))
					for i, c := range v3 {
						d.Colors[i] = glm.Vec4f{c[0], c[1], c[2], 1}
					}
				}
			}
		}
	}
	return d
}

// ---- textures & materials ----

func (l *loader) loadTextures() {
	l.textures = make([]pix.Texture, len(l.doc.Textures))
	for i, gt := range l.doc.Textures {
		if gt.Source == nil {
			continue
		}
		pixels, w, h, err := l.decodeImage(*gt.Source)
		if err != nil {
			continue // skip undecodable images; material falls back to base color
		}
		// Base-color textures are sRGB color (data maps would use pix.TextureLinear).
		l.textures[i] = l.renderer.NewTexture(pixels, w, h, pix.TextureSRGB)
	}
}

func (l *loader) loadMaterials() {
	// glTF materials are metallic-roughness PBR → load them as PBR materials.
	l.materials = make([]*pix.PBRMaterial, len(l.doc.Materials)+1)
	def := l.renderer.NewPBRMaterial()
	def.SetRoughness(1)
	l.materials[0] = def
	for i, gm := range l.doc.Materials {
		m := l.renderer.NewPBRMaterial()
		m.SetMetallic(1)
		m.SetRoughness(1)
		if pbr := gm.PbrMetallicRoughness; pbr != nil {
			if len(pbr.BaseColorFactor) >= 4 {
				m.SetColor(glm.RGBA32F{pbr.BaseColorFactor[0], pbr.BaseColorFactor[1], pbr.BaseColorFactor[2], pbr.BaseColorFactor[3]})
			}
			if pbr.MetallicFactor != nil {
				m.SetMetallic(*pbr.MetallicFactor)
			}
			if pbr.RoughnessFactor != nil {
				m.SetRoughness(*pbr.RoughnessFactor)
			}
			if pbr.BaseColorTexture != nil && pbr.BaseColorTexture.Index < len(l.textures) {
				if t := l.textures[pbr.BaseColorTexture.Index]; t.Valid() {
					m.SetColorMap(t, l.renderer.DefaultSampler())
				}
			}
		}
		l.materials[i+1] = m
	}
}

// materialFor returns the (embedded) Material for a glTF material index; NewMesh
// takes its own copy, and the loader releases these builders after building.
func (l *loader) materialFor(ptr *int) pix.Material {
	if ptr == nil || *ptr+1 >= len(l.materials) {
		return l.materials[0]
	}
	return l.materials[*ptr+1]
}

func (l *loader) decodeImage(idx int) (pixels []byte, w, h int, err error) {
	if idx >= len(l.doc.Images) {
		return nil, 0, 0, fmt.Errorf("image %d out of range", idx)
	}
	gi := l.doc.Images[idx]
	var raw []byte
	if gi.BufferView != nil {
		bv := l.doc.BufferViews[*gi.BufferView]
		raw = l.buffers[bv.Buffer][bv.ByteOffset : bv.ByteOffset+bv.ByteLength]
	} else {
		if raw, err = l.resolveURI(gi.URI); err != nil {
			return nil, 0, 0, err
		}
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, err
	}
	bnd := img.Bounds()
	w, h = bnd.Dx(), bnd.Dy()
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

// ---- node transforms ----

// setLocal sets a scene node's local transform from a glTF node's TRS (or matrix,
// which is decomposed).
func setLocal(n pix.Node, gn node) {
	if len(gn.Matrix) == 16 {
		var m glm.Mat4f
		copy(m[:], gn.Matrix)
		pos, rot, scale := decomposeMatrix(m)
		n.SetPosition(pos)
		n.SetRotationQuat(rot)
		n.SetScale(scale)
		return
	}
	if len(gn.Translation) == 3 {
		n.SetPosition(glm.Vec3f{gn.Translation[0], gn.Translation[1], gn.Translation[2]})
	}
	if len(gn.Rotation) == 4 { // gltf [x,y,z,w] == glm.Quatf order
		n.SetRotationQuat(glm.Quatf{gn.Rotation[0], gn.Rotation[1], gn.Rotation[2], gn.Rotation[3]})
	}
	if len(gn.Scale) == 3 {
		n.SetScale(glm.Vec3f{gn.Scale[0], gn.Scale[1], gn.Scale[2]})
	}
}

// decomposeMatrix splits a column-major TRS matrix into translation, rotation, scale.
func decomposeMatrix(m glm.Mat4f) (pos glm.Vec3f, rot glm.Quatf, scale glm.Vec3f) {
	pos = glm.Vec3f{m[12], m[13], m[14]}
	sx := float32(math.Sqrt(float64(m[0]*m[0] + m[1]*m[1] + m[2]*m[2])))
	sy := float32(math.Sqrt(float64(m[4]*m[4] + m[5]*m[5] + m[6]*m[6])))
	sz := float32(math.Sqrt(float64(m[8]*m[8] + m[9]*m[9] + m[10]*m[10])))
	scale = glm.Vec3f{sx, sy, sz}
	if sx == 0 || sy == 0 || sz == 0 {
		return pos, glm.QuatIdentityf, scale
	}
	r := [9]float32{m[0] / sx, m[1] / sx, m[2] / sx, m[4] / sy, m[5] / sy, m[6] / sy, m[8] / sz, m[9] / sz, m[10] / sz}
	return pos, rotMatToQuat(r), scale
}

// rotMatToQuat converts a column-major 3x3 rotation matrix to a quaternion [x,y,z,w].
func rotMatToQuat(r [9]float32) glm.Quatf {
	trace := r[0] + r[4] + r[8]
	var q glm.Quatf
	switch {
	case trace > 0:
		s := float32(0.5 / math.Sqrt(float64(trace+1)))
		q[3] = 0.25 / s
		q[0] = (r[5] - r[7]) * s
		q[1] = (r[6] - r[2]) * s
		q[2] = (r[1] - r[3]) * s
	case r[0] > r[4] && r[0] > r[8]:
		s := float32(2 * math.Sqrt(float64(1+r[0]-r[4]-r[8])))
		q[3] = (r[5] - r[7]) / s
		q[0] = 0.25 * s
		q[1] = (r[3] + r[1]) / s
		q[2] = (r[6] + r[2]) / s
	case r[4] > r[8]:
		s := float32(2 * math.Sqrt(float64(1+r[4]-r[0]-r[8])))
		q[3] = (r[6] - r[2]) / s
		q[0] = (r[3] + r[1]) / s
		q[1] = 0.25 * s
		q[2] = (r[7] + r[5]) / s
	default:
		s := float32(2 * math.Sqrt(float64(1+r[8]-r[0]-r[4])))
		q[3] = (r[1] - r[3]) / s
		q[0] = (r[6] + r[2]) / s
		q[1] = (r[7] + r[5]) / s
		q[2] = 0.25 * s
	}
	return q
}

// ---- buffers & accessors ----

func (l *loader) loadBuffers(baseDir string, glbBin []byte) error {
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

func (l *loader) resolveURI(uri string) ([]byte, error) {
	if strings.HasPrefix(uri, "data:") {
		comma := strings.IndexByte(uri, ',')
		if comma < 0 {
			return nil, fmt.Errorf("invalid data URI")
		}
		return base64.StdEncoding.DecodeString(uri[comma+1:])
	}
	if l.baseDir == "" {
		return nil, fmt.Errorf("external URI %q needs a base dir", uri)
	}
	return os.ReadFile(filepath.Join(l.baseDir, uri))
}

func (l *loader) accessorBytes(idx int) []byte {
	acc := l.doc.Accessors[idx]
	elemSize := componentSize(acc.ComponentType) * typeComponents(acc.Type)
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
	out := make([]byte, acc.Count*elemSize)
	for i := 0; i < acc.Count; i++ {
		copy(out[i*elemSize:], buf[base+i*stride:base+i*stride+elemSize])
	}
	return out
}

func (l *loader) readIndices(idx int) []uint32 {
	acc := l.doc.Accessors[idx]
	raw := l.accessorBytes(idx)
	out := make([]uint32, acc.Count)
	switch acc.ComponentType {
	case 5121: // UNSIGNED_BYTE
		for i := range out {
			out[i] = uint32(raw[i])
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

// castTo reinterprets accessor bytes as a slice of T (no copy). The caller must
// ensure the element layout matches (e.g. VEC3 FLOAT -> glm.Vec3f).
func castTo[T any](b []byte) []T {
	if len(b) == 0 {
		return nil
	}
	var zero T
	n := len(b) / int(unsafe.Sizeof(zero))
	return unsafe.Slice((*T)(unsafe.Pointer(&b[0])), n)
}

func componentSize(ct int) int {
	switch ct {
	case 5120, 5121:
		return 1
	case 5122, 5123:
		return 2
	default: // 5125, 5126
		return 4
	}
}

func typeComponents(t string) int {
	switch t {
	case "SCALAR":
		return 1
	case "VEC2":
		return 2
	case "VEC3":
		return 3
	case "VEC4", "MAT2":
		return 4
	case "MAT3":
		return 9
	case "MAT4":
		return 16
	}
	return 1
}

// ---- GLB container ----

func splitGLB(data []byte) (jsonChunk, binChunk []byte, err error) {
	if len(data) < 12 || binary.LittleEndian.Uint32(data[0:4]) != 0x46546C67 {
		return nil, nil, fmt.Errorf("gltf: not a GLB file")
	}
	pos := 12
	for pos+8 <= len(data) {
		clen := int(binary.LittleEndian.Uint32(data[pos:]))
		ctype := binary.LittleEndian.Uint32(data[pos+4:])
		pos += 8
		if pos+clen > len(data) {
			return nil, nil, fmt.Errorf("gltf: GLB chunk exceeds file")
		}
		chunk := data[pos : pos+clen]
		pos += clen
		switch ctype {
		case 0x4E4F534A:
			jsonChunk = chunk
		case 0x004E4942:
			binChunk = chunk
		}
	}
	if jsonChunk == nil {
		return nil, nil, fmt.Errorf("gltf: no JSON chunk in GLB")
	}
	return jsonChunk, binChunk, nil
}
