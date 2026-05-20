package loaders

import (
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/glm"
)

// FBX loads binary FBX 7.x files.
type FBX struct {
	r *pix.Renderer
}

// FBXResult holds the scene graph and animation clips loaded from an FBX file.
type FBXResult struct {
	Scene      *pix.Scene
	Animations []*pix.AnimationClip
}

// NewFBX creates an FBX loader backed by the given renderer.
func NewFBX(r *pix.Renderer) *FBX { return &FBX{r: r} }

// Load reads a binary .fbx file from disk.
func (f *FBX) Load(path string) (*FBXResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fbx: read %q: %w", path, err)
	}
	return f.parse(data)
}

// LoadText parses FBX content from a string — useful when the file is embedded
// via //go:embed. Binary FBX magic is detected automatically; ASCII FBX is not supported.
func (f *FBX) LoadText(text string) (*FBXResult, error) {
	if strings.HasPrefix(text, fbxMagic) {
		return f.parse([]byte(text))
	}
	return nil, fmt.Errorf("fbx: ASCII FBX is not supported; use binary .fbx")
}

func (f *FBX) parse(data []byte) (*FBXResult, error) {
	file, err := parseFBXBinary(data)
	if err != nil {
		return nil, err
	}
	b := &fbxBuilder{r: f.r, file: file}
	return b.build()
}

// ---- Binary FBX parser ----

const (
	fbxMagic     = "Kaydara FBX Binary  \x00\x1a\x00"
	fbxTimeTicks = 46186158000.0 // ticks per second
)

type fbxFile struct {
	version uint32
	root    []*fbxRecord
}

type fbxRecord struct {
	name     string
	props    []fbxProp
	children []*fbxRecord
}

type fbxProp struct {
	tag byte
	val any // int16|int32|int64|float32|float64|bool|string|[]byte|[]float32|[]float64|[]int32|[]int64
}

func (p fbxProp) Int64() int64 {
	switch v := p.val.(type) {
	case int64:
		return v
	case int32:
		return int64(v)
	case int16:
		return int64(v)
	}
	return 0
}

func (p fbxProp) Float64() float64 {
	switch v := p.val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

func (p fbxProp) String() string {
	if s, ok := p.val.(string); ok {
		return s
	}
	return ""
}

func (p fbxProp) Float64Slice() []float64 {
	switch v := p.val.(type) {
	case []float64:
		return v
	case []float32:
		out := make([]float64, len(v))
		for i, f := range v {
			out[i] = float64(f)
		}
		return out
	}
	return nil
}

func (p fbxProp) Int32Slice() []int32 {
	if v, ok := p.val.([]int32); ok {
		return v
	}
	return nil
}

func (p fbxProp) Float32Slice() []float32 {
	if v, ok := p.val.([]float32); ok {
		return v
	}
	return nil
}

// parseFBXBinary parses the binary FBX file header and record tree.
func parseFBXBinary(data []byte) (*fbxFile, error) {
	if len(data) < 27 || string(data[:23]) != fbxMagic {
		return nil, fmt.Errorf("fbx: not a binary FBX file")
	}
	version := binary.LittleEndian.Uint32(data[23:27])
	is64 := version >= 7500

	r := &fbxReader{data: data, pos: 27, is64: is64}
	file := &fbxFile{version: version}

	for {
		rec, err := r.readRecord()
		if err != nil {
			return nil, err
		}
		if rec == nil {
			break
		}
		file.root = append(file.root, rec)
	}
	return file, nil
}

type fbxReader struct {
	data []byte
	pos  int
	is64 bool
}

func (r *fbxReader) remaining() int { return len(r.data) - r.pos }

func (r *fbxReader) readUint8() uint8 {
	v := r.data[r.pos]
	r.pos++
	return v
}

func (r *fbxReader) readUint32() uint32 {
	v := binary.LittleEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v
}

func (r *fbxReader) readUint64() uint64 {
	v := binary.LittleEndian.Uint64(r.data[r.pos:])
	r.pos += 8
	return v
}

func (r *fbxReader) readInt16() int16 { return int16(r.readUint16()) }
func (r *fbxReader) readUint16() uint16 {
	v := binary.LittleEndian.Uint16(r.data[r.pos:])
	r.pos += 2
	return v
}

func (r *fbxReader) readInt32() int32  { return int32(r.readUint32()) }
func (r *fbxReader) readInt64() int64  { return int64(r.readUint64()) }
func (r *fbxReader) readFloat32() float32 {
	return math.Float32frombits(r.readUint32())
}
func (r *fbxReader) readFloat64() float64 {
	return math.Float64frombits(r.readUint64())
}
func (r *fbxReader) readBytes(n int) []byte {
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b
}

func (r *fbxReader) readOffset() uint64 {
	if r.is64 {
		return r.readUint64()
	}
	return uint64(r.readUint32())
}

func (r *fbxReader) readRecord() (*fbxRecord, error) {
	if r.remaining() < 13 {
		return nil, nil
	}
	endOffset := r.readOffset()
	numProps := r.readOffset()
	_ = r.readOffset() // propsListLen

	nameLen := int(r.readUint8())
	name := string(r.readBytes(nameLen))

	// Null record: all zeros → end of list.
	if endOffset == 0 && numProps == 0 && nameLen == 0 {
		return nil, nil
	}

	rec := &fbxRecord{name: name}

	for i := uint64(0); i < numProps; i++ {
		p, err := r.readProp()
		if err != nil {
			return nil, fmt.Errorf("fbx: record %q prop %d: %w", name, i, err)
		}
		rec.props = append(rec.props, p)
	}

	// Nested children until endOffset.
	for uint64(r.pos) < endOffset {
		child, err := r.readRecord()
		if err != nil {
			return nil, err
		}
		if child == nil {
			break
		}
		rec.children = append(rec.children, child)
	}

	r.pos = int(endOffset)
	return rec, nil
}

func (r *fbxReader) readProp() (fbxProp, error) {
	tag := r.readUint8()
	switch tag {
	case 'Y':
		return fbxProp{tag: tag, val: r.readInt16()}, nil
	case 'C':
		return fbxProp{tag: tag, val: r.readUint8() != 0}, nil
	case 'I':
		return fbxProp{tag: tag, val: r.readInt32()}, nil
	case 'F':
		return fbxProp{tag: tag, val: r.readFloat32()}, nil
	case 'D':
		return fbxProp{tag: tag, val: r.readFloat64()}, nil
	case 'L':
		return fbxProp{tag: tag, val: r.readInt64()}, nil
	case 'S':
		n := int(r.readUint32())
		return fbxProp{tag: tag, val: string(r.readBytes(n))}, nil
	case 'R':
		n := int(r.readUint32())
		return fbxProp{tag: tag, val: r.readBytes(n)}, nil
	case 'f', 'd', 'l', 'i', 'b':
		return r.readArrayProp(tag)
	}
	return fbxProp{}, fmt.Errorf("unknown property type %q", tag)
}

func (r *fbxReader) readArrayProp(tag byte) (fbxProp, error) {
	count := int(r.readUint32())
	encoding := r.readUint32()
	compLen := int(r.readUint32())

	var raw []byte
	if encoding == 1 {
		zr, err := zlib.NewReader(io.LimitReader(
			&sliceReader{data: r.data, pos: r.pos}, int64(compLen)))
		if err != nil {
			return fbxProp{}, err
		}
		raw, err = io.ReadAll(zr)
		zr.Close()
		if err != nil {
			return fbxProp{}, err
		}
		r.pos += compLen
	} else {
		raw = r.readBytes(compLen)
	}

	switch tag {
	case 'f':
		v := make([]float32, count)
		for i := range v {
			v[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
		return fbxProp{tag: tag, val: v}, nil
	case 'd':
		v := make([]float64, count)
		for i := range v {
			v[i] = math.Float64frombits(binary.LittleEndian.Uint64(raw[i*8:]))
		}
		return fbxProp{tag: tag, val: v}, nil
	case 'i':
		v := make([]int32, count)
		for i := range v {
			v[i] = int32(binary.LittleEndian.Uint32(raw[i*4:]))
		}
		return fbxProp{tag: tag, val: v}, nil
	case 'l':
		v := make([]int64, count)
		for i := range v {
			v[i] = int64(binary.LittleEndian.Uint64(raw[i*8:]))
		}
		return fbxProp{tag: tag, val: v}, nil
	}
	return fbxProp{tag: tag, val: raw}, nil
}

type sliceReader struct {
	data []byte
	pos  int
}

func (s *sliceReader) Read(p []byte) (int, error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	n := copy(p, s.data[s.pos:])
	s.pos += n
	return n, nil
}

// ---- Record navigation helpers ----

func (rec *fbxRecord) child(name string) *fbxRecord {
	for _, c := range rec.children {
		if c.name == name {
			return c
		}
	}
	return nil
}

func (rec *fbxRecord) children_(name string) []*fbxRecord {
	var out []*fbxRecord
	for _, c := range rec.children {
		if c.name == name {
			out = append(out, c)
		}
	}
	return out
}

func rootNode(file *fbxFile, name string) *fbxRecord {
	for _, r := range file.root {
		if r.name == name {
			return r
		}
	}
	return nil
}

// ---- Scene builder ----

type fbxObject struct {
	id    int64
	typ   string // "Geometry", "Model", "Material", "Texture", "Video", "Deformer", "AnimationStack", etc.
	class string // "Mesh", "LimbNode", "Null", "Skin", "Cluster", etc.
	rec   *fbxRecord
}

type fbxConn struct {
	kind     string // "OO" or "OP"
	childID  int64
	parentID int64
	prop     string
}

type fbxBuilder struct {
	r    *pix.Renderer
	file *fbxFile

	objects          map[int64]*fbxObject
	childToParents   map[int64][]fbxConn
	parentToChildren map[int64][]fbxConn

	nodeByID       map[int64]pix.Node   // populated by buildModels
	preRotByID     map[int64]glm.Quatf  // PreRotation per node
	postRotInvByID map[int64]glm.Quatf  // PostRotation^-1 per node
	rotOrderByID   map[int64]int        // RotationOrder per node (0=XYZ, 4=ZXY, etc.)
	deferredAnims  []deferredAnim       // resolved after buildModels
}

func (b *fbxBuilder) build() (*FBXResult, error) {
	b.objects = map[int64]*fbxObject{}
	b.childToParents = map[int64][]fbxConn{}
	b.parentToChildren = map[int64][]fbxConn{}
	b.preRotByID = map[int64]glm.Quatf{}
	b.postRotInvByID = map[int64]glm.Quatf{}
	b.rotOrderByID = map[int64]int{}

	b.indexObjects()
	b.indexConnections()

	scene := pix.NewScene()
	clips := b.buildAnimations(scene)
	if err := b.buildModels(scene); err != nil {
		return nil, err
	}
	b.resolveAnims() // wire deferred tracks now that nodeByID is populated
	return &FBXResult{Scene: scene, Animations: clips}, nil
}

func (b *fbxBuilder) indexObjects() {
	objs := rootNode(b.file, "Objects")
	if objs == nil {
		return
	}
	for _, rec := range objs.children {
		if len(rec.props) < 3 {
			continue
		}
		id := rec.props[0].Int64()
		nameClass := rec.props[1].String() // "Type::Name"
		class := rec.props[2].String()

		name := nameClass
		if i := strings.Index(nameClass, "::"); i >= 0 {
			name = nameClass[i+2:]
		}
		_ = name

		b.objects[id] = &fbxObject{id: id, typ: rec.name, class: class, rec: rec}
	}
}

func (b *fbxBuilder) indexConnections() {
	conns := rootNode(b.file, "Connections")
	if conns == nil {
		return
	}
	for _, c := range conns.children_("C") {
		if len(c.props) < 3 {
			continue
		}
		kind := c.props[0].String()
		childID := c.props[1].Int64()
		parentID := c.props[2].Int64()
		prop := ""
		if len(c.props) > 3 {
			prop = c.props[3].String()
		}
		conn := fbxConn{kind: kind, childID: childID, parentID: parentID, prop: prop}
		b.childToParents[childID] = append(b.childToParents[childID], conn)
		b.parentToChildren[parentID] = append(b.parentToChildren[parentID], conn)
	}
}

func (b *fbxBuilder) childrenOfType(parentID int64, typ string) []*fbxObject {
	var out []*fbxObject
	for _, conn := range b.parentToChildren[parentID] {
		if obj, ok := b.objects[conn.childID]; ok && obj.typ == typ {
			out = append(out, obj)
		}
	}
	return out
}

func (b *fbxBuilder) parentOfType(childID int64, typ string) *fbxObject {
	for _, conn := range b.childToParents[childID] {
		if obj, ok := b.objects[conn.parentID]; ok && obj.typ == typ {
			return obj
		}
	}
	return nil
}

// ---- Model hierarchy ----

func (b *fbxBuilder) buildModels(scene *pix.Scene) error {
	// Gather all model objects.
	var models []*fbxObject
	for _, obj := range b.objects {
		if obj.typ == "Model" {
			models = append(models, obj)
		}
	}

	// Determine which models are bones (linked to a cluster).
	boneIDs := map[int64]bool{}
	for _, obj := range b.objects {
		if obj.typ == "Deformer" && obj.class == "Cluster" {
			for _, conn := range b.parentToChildren[obj.id] {
				if o, ok := b.objects[conn.childID]; ok && o.typ == "Model" {
					boneIDs[o.id] = true
				}
			}
		}
	}
	// Also treat LimbNode models as bones.
	for _, obj := range models {
		if obj.class == "LimbNode" {
			boneIDs[obj.id] = true
		}
	}

	// Create pix nodes for every model.
	nodeByID := map[int64]pix.Node{}
	boneByID := map[int64]pix.Bone{}

	for _, obj := range models {
		if boneIDs[obj.id] {
			bone := scene.NewBone()
			boneByID[obj.id] = bone
			nodeByID[obj.id] = bone.Node
		} else {
			nodeByID[obj.id] = scene.NewGroup().Node
		}
	}

	// Apply transforms and store pre/post rotations for animation.
	for _, obj := range models {
		n := nodeByID[obj.id]
		t, r, s, preRot, postRotInv, rotOrd := extractTransform(obj.rec)
		n.SetPosition(t)
		n.SetRotationQuat(r)
		n.SetScale(s)
		b.preRotByID[obj.id] = preRot
		b.postRotInvByID[obj.id] = postRotInv
		b.rotOrderByID[obj.id] = rotOrd
	}

	// Wire up parent-child hierarchy.
	rootIDs := []int64{}
	for _, obj := range models {
		parentModel := b.parentOfType(obj.id, "Model")
		if parentModel == nil {
			rootIDs = append(rootIDs, obj.id)
		} else {
			nodeByID[parentModel.id].Add(nodeByID[obj.id])
		}
	}

	// Build a map from bone model ID → the cluster that controls it (for TransformLink lookup).
	// A cluster links to exactly one bone model via C: "OO", boneModelID, clusterID.
	clusterByBoneModelID := map[int64]*fbxObject{}
	for _, obj := range b.objects {
		if obj.typ != "Deformer" || obj.class != "Cluster" {
			continue
		}
		for _, conn := range b.parentToChildren[obj.id] {
			if o, ok := b.objects[conn.childID]; ok && o.typ == "Model" {
				clusterByBoneModelID[o.id] = obj
				break
			}
		}
	}

	// Build meshes for each model that owns a geometry.
	for _, obj := range models {
		geos := b.childrenOfType(obj.id, "Geometry")
		for _, geoObj := range geos {
			if geoObj.class != "Mesh" {
				continue
			}
			gd, clusterOrder, err := b.buildGeometry(geoObj)
			if err != nil {
				return fmt.Errorf("fbx: geometry for model %d: %w", obj.id, err)
			}
			pixGeo := b.r.NewGeometry(gd)

			// Material.
			mat := b.r.NewBlinnPhongMaterial()
			if mats := b.childrenOfType(obj.id, "Material"); len(mats) > 0 {
				b.applyMaterial(mat, mats[0])
			}
			pixMat := mat.Ref()
			mat.Release()

			// Find skin deformer for this geometry (if any).
			// In FBX: C: "OO", skinID, geoID — skin is child, geometry is parent.
			var skin *fbxObject
			for _, conn := range b.parentToChildren[geoObj.id] {
				if o, ok := b.objects[conn.childID]; ok && o.typ == "Deformer" && o.class == "Skin" {
					skin = o
					break
				}
			}

			groupNode := nodeByID[obj.id]
			if skin != nil && len(clusterOrder) > 0 {
				// Build the skeleton in the exact order clusterOrder specifies —
				// that is the order bone indices were embedded into the vertex data.
				var bones []pix.Bone
				var invBindMats []glm.Mat4f
				for _, boneModelID := range clusterOrder {
					bone, ok := boneByID[boneModelID]
					if !ok {
						continue
					}
					cluster, ok := clusterByBoneModelID[boneModelID]
					if !ok {
						continue
					}
					ibm := extractMat4(cluster.rec, "TransformLink").Inv()
					bones = append(bones, bone)
					invBindMats = append(invBindMats, ibm)
				}
				if len(bones) > 0 {
					sk := pix.NewSkeletonWithInvBindMats(bones, invBindMats)
					sm := scene.NewSkinnedMesh(pixGeo, pixMat, sk)
					groupNode.Add(sm)
				} else {
					m := scene.NewMesh(pixGeo, pixMat)
					groupNode.Add(m)
				}
			} else {
				m := scene.NewMesh(pixGeo, pixMat)
				groupNode.Add(m)
			}

			pixGeo.Release()
			pixMat.Release()
		}
	}

	// Add root-level nodes to scene.
	for _, id := range rootIDs {
		scene.Add(nodeByID[id])
	}

	b.nodeByID = nodeByID
	_ = boneByID

	return nil
}

// ---- Geometry ----

type fbxVertKey struct{ cp, ni, ui int32 }

func (b *fbxBuilder) buildGeometry(obj *fbxObject) (gd *pix.GeometryData, clusterBoneOrder []int64, err error) {
	rec := obj.rec

	// Control points.
	cpProp := childPropFloat64(rec, "Vertices")
	if cpProp == nil {
		return nil, nil, fmt.Errorf("no Vertices")
	}
	rawCP := cpProp
	numCP := len(rawCP) / 3
	controlPoints := make([]glm.Vec3f, numCP)
	for i := range controlPoints {
		controlPoints[i] = glm.Vec3f{float32(rawCP[i*3]), float32(rawCP[i*3+1]), float32(rawCP[i*3+2])}
	}

	// Polygon vertex index.
	pvRaw := childPropInt32(rec, "PolygonVertexIndex")
	if pvRaw == nil {
		return nil, nil, fmt.Errorf("no PolygonVertexIndex")
	}

	// Normals.
	var normals []glm.Vec3f
	var normMapping, normReference string
	var normIndex []int32
	if norElem := rec.child("LayerElementNormal"); norElem != nil {
		normMapping, _ = childString(norElem, "MappingInformationType")
		normReference, _ = childString(norElem, "ReferenceInformationType")
		if nd := childPropFloat64(norElem, "Normals"); nd != nil {
			normals = make([]glm.Vec3f, len(nd)/3)
			for i := range normals {
				normals[i] = glm.Vec3f{float32(nd[i*3]), float32(nd[i*3+1]), float32(nd[i*3+2])}
			}
		}
		normIndex = childPropInt32(norElem, "NormalsIndex")
	}

	// UVs.
	var uvs []glm.Vec2f
	var uvMapping, uvReference string
	var uvIndex []int32
	if uvElem := rec.child("LayerElementUV"); uvElem != nil {
		uvMapping, _ = childString(uvElem, "MappingInformationType")
		uvReference, _ = childString(uvElem, "ReferenceInformationType")
		if ud := childPropFloat64(uvElem, "UV"); ud != nil {
			uvs = make([]glm.Vec2f, len(ud)/2)
			for i := range uvs {
				uvs[i] = glm.Vec2f{float32(ud[i*2]), float32(ud[i*2+1])}
			}
		}
		uvIndex = childPropInt32(uvElem, "UVIndex")
	}

	// Skin deformer cluster data for this geometry (vertex weights).
	type clusterInfluence struct {
		boneOrderIdx int // index into clusterBoneOrder
		weight       float32
	}
	influences := make([][]clusterInfluence, numCP)

	// Find skin deformer connected to this geometry.
	// In FBX: C: "OO", skinID, geoID — skin is child of geometry in the connection graph.
	for _, conn := range b.parentToChildren[obj.id] {
		if skinObj, ok := b.objects[conn.childID]; ok && skinObj.typ == "Deformer" && skinObj.class == "Skin" {
			clusters := b.childrenOfType(skinObj.id, "Deformer")
			for ci, cluster := range clusters {
				if cluster.class != "Cluster" {
					continue
				}
				// Register the bone.
				var boneModelID int64
				for _, c2 := range b.parentToChildren[cluster.id] {
					if o, ok := b.objects[c2.childID]; ok && o.typ == "Model" {
						boneModelID = o.id
						break
					}
				}
				clusterBoneOrder = append(clusterBoneOrder, boneModelID)

				idxs := childPropInt32(cluster.rec, "Indexes")
				wgts := childPropFloat64(cluster.rec, "Weights")
				for k, cpIdx := range idxs {
					if k >= len(wgts) {
						break
					}
					influences[cpIdx] = append(influences[cpIdx], clusterInfluence{ci, float32(wgts[k])})
				}
			}
			break
		}
	}
	hasSkin := len(clusterBoneOrder) > 0

	// Walk polygons, triangulate, build unique vertex buffer.
	type finalVert struct {
		pos    glm.Vec3f
		nor    glm.Vec3f
		uv     glm.Vec2f
		joints [4]uint32
		wts    [4]float32
	}

	var finalVerts []finalVert
	var finalIdx []uint32
	vertMap := map[fbxVertKey]uint32{}
	cpForVert := []int32{} // finalVert → cpIdx (for skin)

	pvIdx := 0
	polyStart := 0

	lookupNormal := func(pvI, cpI int32) glm.Vec3f {
		if len(normals) == 0 {
			return glm.Vec3f{0, 1, 0}
		}
		switch normMapping {
		case "ByPolygonVertex":
			ni := pvI
			if normReference == "IndexToDirect" && int(pvI) < len(normIndex) {
				ni = normIndex[pvI]
			}
			if int(ni) < len(normals) {
				return normals[ni]
			}
		case "ByVertex", "ByVertice":
			if int(cpI) < len(normals) {
				return normals[cpI]
			}
		}
		return glm.Vec3f{0, 1, 0}
	}

	lookupUV := func(pvI, cpI int32) glm.Vec2f {
		if len(uvs) == 0 {
			return glm.Vec2f{}
		}
		switch uvMapping {
		case "ByPolygonVertex":
			ui := pvI
			if uvReference == "IndexToDirect" && int(pvI) < len(uvIndex) {
				ui = uvIndex[pvI]
			}
			if int(ui) < len(uvs) {
				return uvs[ui]
			}
		case "ByVertex", "ByVertice":
			if int(cpI) < len(uvs) {
				return uvs[cpI]
			}
		}
		return glm.Vec2f{}
	}

	addVert := func(pvI int32) uint32 {
		rawIdx := pvRaw[pvI]
		var cpI int32
		if rawIdx < 0 {
			cpI = ^rawIdx
		} else {
			cpI = rawIdx
		}
		nor := lookupNormal(pvI, cpI)
		uv := lookupUV(pvI, cpI)

		// Use pvI as the normal and UV disambiguator when ByPolygonVertex.
		ni := pvI
		ui := pvI
		if normMapping == "ByVertex" || normMapping == "ByVertice" {
			ni = cpI
		}
		if uvMapping == "ByVertex" || uvMapping == "ByVertice" {
			ui = cpI
		}

		key := fbxVertKey{cp: cpI, ni: ni, ui: ui}
		if idx, ok := vertMap[key]; ok {
			return idx
		}
		idx := uint32(len(finalVerts))
		vertMap[key] = idx

		fv := finalVert{pos: controlPoints[cpI], nor: nor, uv: uv}
		if hasSkin {
			infl := influences[cpI]
			sort.Slice(infl, func(a, b int) bool { return infl[a].weight > infl[b].weight })
			total := float32(0)
			for k := 0; k < 4 && k < len(infl); k++ {
				total += infl[k].weight
			}
			for k := 0; k < 4 && k < len(infl); k++ {
				fv.joints[k] = uint32(infl[k].boneOrderIdx)
				if total > 0 {
					fv.wts[k] = infl[k].weight / total
				}
			}
		}
		finalVerts = append(finalVerts, fv)
		cpForVert = append(cpForVert, cpI)
		return idx
	}

	for pvIdx < len(pvRaw) {
		polyStart = pvIdx
		for pvIdx < len(pvRaw) {
			if pvRaw[pvIdx] < 0 {
				pvIdx++
				break
			}
			pvIdx++
		}
		polyLen := pvIdx - polyStart
		if polyLen < 3 {
			continue
		}
		// Fan triangulation.
		v0 := addVert(int32(polyStart))
		for i := 1; i < polyLen-1; i++ {
			v1 := addVert(int32(polyStart + i))
			v2 := addVert(int32(polyStart + i + 1))
			finalIdx = append(finalIdx, v0, v1, v2)
		}
	}
	_ = cpForVert

	// Build GeometryData.
	gd = &pix.GeometryData{}
	gd.SetIndices(finalIdx)

	positions := make([]glm.Vec3f, len(finalVerts))
	normArr := make([]glm.Vec3f, len(finalVerts))
	uvArr := make([]glm.Vec2f, len(finalVerts))
	for i, fv := range finalVerts {
		positions[i] = fv.pos
		normArr[i] = fv.nor
		uvArr[i] = fv.uv
	}
	gd.AddAttribute(pix.NewAttribute(pix.PositionAttrName, pix.PositionLocation, pix.Float32x3, positions))
	gd.AddAttribute(pix.NewAttribute(pix.NormalAttrName, pix.NormalLocation, pix.Float32x3, normArr))
	gd.AddAttribute(pix.NewAttribute(pix.UVAttrName, pix.UVLocation, pix.Float32x2, uvArr))

	if hasSkin {
		type vec4u32 = [4]uint32
		joints := make([]vec4u32, len(finalVerts))
		weights := make([][4]float32, len(finalVerts))
		for i, fv := range finalVerts {
			joints[i] = fv.joints
			weights[i] = fv.wts
		}
		gd.AddAttribute(pix.NewAttribute(pix.SkinIndexAttrName, pix.SkinIndexLocation, pix.Uint32x4, joints))
		gd.AddAttribute(pix.NewAttribute(pix.SkinWeightAttrName, pix.SkinWeightLocation, pix.Float32x4, weights))
	}

	return gd, clusterBoneOrder, nil
}

// ---- Material ----

func (b *fbxBuilder) applyMaterial(m *pix.BlinnPhongMaterial, obj *fbxObject) {
	// Extract diffuse color from Properties70.
	props70 := obj.rec.child("Properties70")
	if props70 == nil {
		return
	}
	for _, p := range props70.children_("P") {
		if len(p.props) < 2 {
			continue
		}
		name := p.props[0].String()
		if name == "DiffuseColor" || name == "Diffuse" {
			if len(p.props) >= 7 {
				r := float32(p.props[4].Float64())
				g := float32(p.props[5].Float64())
				bv := float32(p.props[6].Float64())
				m.SetColor(glm.Color3f{r, g, bv})
			}
		}
	}
	// Texture is connected via Connections; skip for now.
}

// ---- Animations ----

func (b *fbxBuilder) buildAnimations(scene *pix.Scene) []*pix.AnimationClip {
	// Find all AnimationStack objects.
	var clips []*pix.AnimationClip

	for _, stack := range b.objects {
		if stack.typ != "AnimationStack" {
			continue
		}

		clip := &pix.AnimationClip{Name: objectName(stack)}

		// Find AnimationLayer(s) in this stack.
		for _, conn := range b.parentToChildren[stack.id] {
			layer, ok := b.objects[conn.childID]
			if !ok || layer.typ != "AnimationLayer" {
				continue
			}

			// Find AnimationCurveNode(s) in this layer.
			for _, lconn := range b.parentToChildren[layer.id] {
				curveNode, ok := b.objects[lconn.childID]
				if !ok || curveNode.typ != "AnimationCurveNode" {
					continue
				}

				// Find which model this curveNode targets.
				var targetID int64
				var targetProp string
				for _, cconn := range b.childToParents[curveNode.id] {
					if cconn.kind == "OP" {
						if tgt, ok := b.objects[cconn.parentID]; ok && tgt.typ == "Model" {
							targetID = tgt.id
							targetProp = cconn.prop
							break
						}
					}
				}
				if targetID == 0 {
					continue
				}

				// Extract X, Y, Z curves.
				xTimes, xVals := b.extractCurveForProp(curveNode.id, "d|X")
				yTimes, yVals := b.extractCurveForProp(curveNode.id, "d|Y")
				zTimes, zVals := b.extractCurveForProp(curveNode.id, "d|Z")

				times := mergeTimeSamples(xTimes, yTimes, zTimes)
				if len(times) == 0 {
					continue
				}

				// Update clip duration.
				if last := times[len(times)-1]; last > clip.Duration {
					clip.Duration = last
				}

				// We need the pix.Node for targetID, but those are built in buildModels
				// which runs after buildAnimations. Store placeholder and fix up later.
				// Instead, build a thin wrapper that defers the lookup.
				// For now, we attach a zero Node and the caller can re-associate.
				// A simpler approach: build a deferred list and apply after buildModels.
				_ = targetProp
				_ = times

				b.deferAnim(clip, targetID, targetProp, times,
					xTimes, xVals, yTimes, yVals, zTimes, zVals)
			}
		}
		clips = append(clips, clip)
	}
	return clips
}

// deferredAnim is resolved in resolveAnimTargets after nodes are created.
type deferredAnim struct {
	clip       *pix.AnimationClip
	targetID   int64
	targetProp string
	times      []float32
	xT, xV    []float32
	yT, yV    []float32
	zT, zV    []float32
}

func (b *fbxBuilder) deferAnim(clip *pix.AnimationClip, targetID int64, prop string,
	times, xT, xV, yT, yV, zT, zV []float32) {
	b.deferredAnims = append(b.deferredAnims, deferredAnim{
		clip: clip, targetID: targetID, targetProp: prop,
		times: times,
		xT: xT, xV: xV,
		yT: yT, yV: yV,
		zT: zT, zV: zV,
	})
}

func (b *fbxBuilder) resolveAnims() {
	const deg2rad = math.Pi / 180

	for _, da := range b.deferredAnims {
		node, ok := b.nodeByID[da.targetID]
		if !ok {
			continue
		}

		switch {
		case strings.Contains(da.targetProp, "Translation") || strings.Contains(da.targetProp, "T"):
			values := make([]glm.Vec3f, len(da.times))
			for i, t := range da.times {
				values[i] = glm.Vec3f{
					sampleLinear(da.xT, da.xV, t),
					sampleLinear(da.yT, da.yV, t),
					sampleLinear(da.zT, da.zV, t),
				}
			}
			da.clip.Positions = append(da.clip.Positions, pix.PositionTrack{
				Target: node, Times: da.times, Values: values,
			})
		case strings.Contains(da.targetProp, "Rotation") || strings.Contains(da.targetProp, "R"):
			preRot := b.preRotByID[da.targetID]
			postRotInv := b.postRotInvByID[da.targetID]
			rotOrd := b.rotOrderByID[da.targetID]
			values := make([]glm.Quatf, len(da.times))
			for i, t := range da.times {
				rx := sampleLinear(da.xT, da.xV, t) * deg2rad
				ry := sampleLinear(da.yT, da.yV, t) * deg2rad
				rz := sampleLinear(da.zT, da.zV, t) * deg2rad
				lclRot := fbxEulerToQuat(rx, ry, rz, rotOrd)
				values[i] = preRot.Mul(lclRot).Mul(postRotInv)
			}
			da.clip.Rotations = append(da.clip.Rotations, pix.RotationTrack{
				Target: node, Times: da.times, Values: values,
			})
		case strings.Contains(da.targetProp, "Scaling") || strings.Contains(da.targetProp, "S"):
			values := make([]glm.Vec3f, len(da.times))
			for i, t := range da.times {
				values[i] = glm.Vec3f{
					sampleLinear(da.xT, da.xV, t),
					sampleLinear(da.yT, da.yV, t),
					sampleLinear(da.zT, da.zV, t),
				}
			}
			da.clip.Scales = append(da.clip.Scales, pix.ScaleTrack{
				Target: node, Times: da.times, Values: values,
			})
		}
	}
	b.deferredAnims = b.deferredAnims[:0]
}

func (b *fbxBuilder) extractCurveForProp(curveNodeID int64, propName string) (times, values []float32) {
	for _, conn := range b.parentToChildren[curveNodeID] {
		if conn.kind != "OP" || conn.prop != propName {
			continue
		}
		curveObj, ok := b.objects[conn.childID]
		if !ok || curveObj.typ != "AnimationCurve" {
			continue
		}
		keyTimes := childPropInt64(curveObj.rec, "KeyTime")
		keyVals := curveObj.rec.child("KeyValueFloat")
		if keyVals == nil || len(keyTimes) == 0 {
			continue
		}
		var kvf []float32
		if len(keyVals.props) > 0 {
			kvf = keyVals.props[0].Float32Slice()
		}
		if len(kvf) == 0 {
			continue
		}
		times = make([]float32, len(keyTimes))
		for i, kt := range keyTimes {
			times[i] = float32(float64(kt) / fbxTimeTicks)
		}
		values = kvf
		return
	}
	return nil, nil
}

// ---- Transform extraction ----

const deg2radFBX = math.Pi / 180

// extractTransform reads Lcl Translation/Rotation/Scaling plus PreRotation/PostRotation
// from a node's Properties70. The returned rot already includes the pre/post bake so it
// can be set directly as the node's bind-pose local rotation.
// preRot and postRotInv are returned separately so animation tracks can apply the same bake.
// fbxEulerToQuat converts FBX Euler angles (full angles, radians) to a quaternion
// for OpenGL (column-vector) convention. FBX uses Maya's row-vector convention where
// "XYZ" order means v*Rx*Ry*Rz, which in column-vector form is Rz*Ry*Rx — reversed.
// FBX RotationOrder: 0=XYZ, 1=XZY, 2=YZX, 3=YXZ, 4=ZXY (Maya default), 5=ZYX.
func fbxEulerToQuat(rx, ry, rz float32, order int) glm.Quatf {
	sinX, cosX := math.Sincos(float64(rx) / 2)
	sinY, cosY := math.Sincos(float64(ry) / 2)
	sinZ, cosZ := math.Sincos(float64(rz) / 2)
	qx := glm.Quatf{float32(sinX), 0, 0, float32(cosX)}
	qy := glm.Quatf{0, float32(sinY), 0, float32(cosY)}
	qz := glm.Quatf{0, 0, float32(sinZ), float32(cosZ)}
	// FBX uses Maya's row-vector convention: "XYZ" means v*Rx*Ry*Rz.
	// Converting to OpenGL column-vector (v'=M*v) reverses the order: q = qZ*qY*qX.
	switch order {
	case 0: // FBX "XYZ" → OpenGL q = qZ * qY * qX
		return qz.Mul(qy).Mul(qx)
	case 1: // FBX "XZY" → OpenGL q = qY * qZ * qX
		return qy.Mul(qz).Mul(qx)
	case 2: // FBX "YZX" → OpenGL q = qX * qZ * qY
		return qx.Mul(qz).Mul(qy)
	case 3: // FBX "YXZ" → OpenGL q = qZ * qX * qY
		return qz.Mul(qx).Mul(qy)
	case 4: // FBX "ZXY" (Maya default) → OpenGL q = qY * qX * qZ
		return qy.Mul(qx).Mul(qz)
	case 5: // FBX "ZYX" → OpenGL q = qX * qY * qZ
		return qx.Mul(qy).Mul(qz)
	default:
		return qz.Mul(qy).Mul(qx)
	}
}

func extractTransform(rec *fbxRecord) (pos glm.Vec3f, rot glm.Quatf, scale glm.Vec3f, preRot, postRotInv glm.Quatf, rotOrder int) {
	scale = glm.Vec3f{1, 1, 1}
	rot = glm.QuatIdentityf
	preRot = glm.QuatIdentityf
	postRotInv = glm.QuatIdentityf
	var lclRot glm.Quatf = glm.QuatIdentityf
	var postRot glm.Quatf = glm.QuatIdentityf
	rotOrder = 0 // default XYZ

	props70 := rec.child("Properties70")
	if props70 == nil {
		return
	}
	// First pass: read RotationOrder so we can apply it to Lcl Rotation
	for _, p := range props70.children_("P") {
		if len(p.props) < 5 {
			continue
		}
		if p.props[0].String() == "RotationOrder" {
			rotOrder = int(p.props[4].Int64())
			break
		}
	}
	for _, p := range props70.children_("P") {
		if len(p.props) < 5 {
			continue
		}
		name := p.props[0].String()
		switch name {
		case "Lcl Translation":
			pos = glm.Vec3f{
				float32(p.props[4].Float64()),
				float32(p.props[5].Float64()),
				float32(p.props[6].Float64()),
			}
		case "Lcl Rotation":
			rx := float32(p.props[4].Float64()) * deg2radFBX
			ry := float32(p.props[5].Float64()) * deg2radFBX
			rz := float32(p.props[6].Float64()) * deg2radFBX
			lclRot = fbxEulerToQuat(rx, ry, rz, rotOrder)
		case "Lcl Scaling":
			scale = glm.Vec3f{
				float32(p.props[4].Float64()),
				float32(p.props[5].Float64()),
				float32(p.props[6].Float64()),
			}
		case "PreRotation":
			rx := float32(p.props[4].Float64()) * deg2radFBX
			ry := float32(p.props[5].Float64()) * deg2radFBX
			rz := float32(p.props[6].Float64()) * deg2radFBX
			// PreRotation always uses XYZ intrinsic regardless of RotationOrder.
			preRot = fbxEulerToQuat(rx, ry, rz, 0)
		case "PostRotation":
			rx := float32(p.props[4].Float64()) * deg2radFBX
			ry := float32(p.props[5].Float64()) * deg2radFBX
			rz := float32(p.props[6].Float64()) * deg2radFBX
			// PostRotation always uses XYZ intrinsic.
			postRot = fbxEulerToQuat(rx, ry, rz, 0)
		}
	}

	// PostRotation is applied inverted: final = PreRotation * LclRotation * PostRotation^-1
	postRotInv = postRot.Conjugate()
	rot = preRot.Mul(lclRot).Mul(postRotInv)
	return
}

func extractMat4(rec *fbxRecord, childName string) glm.Mat4f {
	c := rec.child(childName)
	if c == nil || len(c.props) == 0 {
		return glm.Mat4fIndentity
	}
	d := c.props[0].Float64Slice()
	if len(d) < 16 {
		return glm.Mat4fIndentity
	}
	var m glm.Mat4f
	for i := range m {
		m[i] = float32(d[i])
	}
	// FBX stores matrices row-major in row-vector (Maya/DirectX) convention where
	// R_fbx = Q^T. Copying directly into column-major gives: m[col*4+row] = d[col*4+row]
	// = R_fbx[col][row] = Q[row][col], which is exactly the correct column-major element.
	// No rotation swap is needed; translation ends up in column 3 (m[12..14]) correctly.
	return m
}

// ---- Curve sampling ----

func sampleLinear(times, values []float32, t float32) float32 {
	if len(times) == 0 {
		return 0
	}
	if t <= times[0] {
		return values[0]
	}
	last := len(times) - 1
	if t >= times[last] {
		return values[last]
	}
	lo, hi := 0, last
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if times[mid] <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	alpha := (t - times[lo]) / (times[hi] - times[lo])
	return values[lo] + alpha*(values[hi]-values[lo])
}

func mergeTimeSamples(slices ...[]float32) []float32 {
	seen := map[float32]bool{}
	for _, s := range slices {
		for _, t := range s {
			seen[t] = true
		}
	}
	out := make([]float32, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ---- FBX record helpers ----

func childPropFloat64(rec *fbxRecord, name string) []float64 {
	c := rec.child(name)
	if c == nil || len(c.props) == 0 {
		return nil
	}
	return c.props[0].Float64Slice()
}

func childPropInt32(rec *fbxRecord, name string) []int32 {
	c := rec.child(name)
	if c == nil || len(c.props) == 0 {
		return nil
	}
	return c.props[0].Int32Slice()
}

func childPropInt64(rec *fbxRecord, name string) []int64 {
	c := rec.child(name)
	if c == nil || len(c.props) == 0 {
		return nil
	}
	if v, ok := c.props[0].val.([]int64); ok {
		return v
	}
	return nil
}

func childString(rec *fbxRecord, name string) (string, bool) {
	c := rec.child(name)
	if c == nil || len(c.props) == 0 {
		return "", false
	}
	s := c.props[0].String()
	return s, s != ""
}

func objectName(obj *fbxObject) string {
	if len(obj.rec.props) < 2 {
		return ""
	}
	s := obj.rec.props[1].String()
	if i := strings.Index(s, "::"); i >= 0 {
		return s[i+2:]
	}
	return s
}
