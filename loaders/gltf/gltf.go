// Package gltf loads .gltf / .glb assets into a scenes.Scene. It depends only on
// the renderer (pix) and math (glm) — never the gpu backend — so the same
// loader works against any backend the renderer runs on. Skinning and animation
// are supported: a glTF skin becomes a scenes.Skeleton (its joint nodes become
// scenes.Bone nodes, not plain groups — see Load), a mesh referencing that skin
// becomes a scenes.SkinnedMesh, and glTF animations come back as pix.AnimationClips
// ready for a scenes.AnimationMixer. Morph targets load onto their geometries, named
// from the mesh's (or node's) extras.targetNames, starting at the mesh's (or node's)
// default weights; "weights" channels animate them. CUBICSPLINE samplers play as
// LINEAR over their value keys.
package gltf

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
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
	"github.com/bluescreen10/pix/textures"
)

// LoadResult is what Load made: how many mesh nodes it added to the scene, the
// skeletons built from the asset's skins (a SkinnedMesh already references its own —
// this is for e.g. Skeleton.Pose or attaching props to a named bone), and its animation
// clips, each ready to hand to an AnimationMixer via mixer.Action(clip).
type LoadResult struct {
	Added     int
	Skeletons []scenes.Skeleton
	Clips     []*scenes.AnimationClip
}

// Options is how Load loads a scene; a nil *Options is the zero value: the defaults.
type Options struct {
	// MipChain is which levels the scene's textures get: all of them, by default, or
	// with textures.BaseLevelOnly the image alone — which loads a large scene faster,
	// at the cost of textures that shimmer with distance.
	MipChain textures.MipChain
}

// Load adds the asset at path to scene: its meshes, its skins and animations. A glTF
// skin becomes a scenes.Skeleton (see the package doc), and every scenes.AnimationClip
// in the file comes back in the result, ready to play. options says how; nil loads with
// the defaults.
func Load(r *pix.Renderer, scene *scenes.Scene, path string, options *Options) (LoadResult, error) {
	if options == nil {
		options = &Options{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return LoadResult{}, fmt.Errorf("gltf: read %q: %w", path, err)
	}
	l := &loader{renderer: r, scene: scene, options: *options}
	if strings.ToLower(filepath.Ext(path)) == ".glb" {
		jsonData, binData, err := splitGLB(data)
		if err != nil {
			return LoadResult{}, err
		}
		if err := json.Unmarshal(jsonData, &l.doc); err != nil {
			return LoadResult{}, fmt.Errorf("gltf: JSON: %w", err)
		}
		if err := l.loadBuffers("", binData); err != nil {
			return LoadResult{}, err
		}
	} else {
		if err := json.Unmarshal(data, &l.doc); err != nil {
			return LoadResult{}, fmt.Errorf("gltf: JSON: %w", err)
		}
		if err := l.loadBuffers(filepath.Dir(path), nil); err != nil {
			return LoadResult{}, err
		}
	}
	added, err := l.build()
	if err != nil {
		return LoadResult{}, err
	}
	return LoadResult{Added: added, Skeletons: l.skeletons, Clips: l.loadAnimations()}, nil
}

// texKey dedups uploaded textures. Usage is part of the key, not just an upload
// argument: the same glTF image can legitimately be referenced as both a color map
// and a data map, and those must become different GPU textures (different format,
// different mip filtering).
type texKey struct {
	index int
	usage textures.Format
}

type loader struct {
	renderer *pix.Renderer
	scene    *scenes.Scene
	options  Options
	doc      doc
	buffers  [][]byte
	baseDir  string

	texCache  map[texKey]textures.Texture // (gltf texture, usage) -> uploaded texture
	allTex    []textures.Texture          // every uploaded texture, for release after build
	materials []*materials.PBRMaterial    // [0]=default; gltf material i -> [i+1]
	nodes     []scenes.Node               // per gltf node
	added     int

	// Skinning: parent[i] is node i's glTF parent index, or -1 (built once by
	// loadSkins, needed to walk from a joint up to its nearest joint ancestor or
	// the skin's attach node). jointOwner/jointBoneIdx map a joint's glTF node
	// index to (skin index, index within that skin's joint array). attachNode[si]
	// is the glTF node loadSkins chose to represent skin si's Skeleton root in the
	// scene graph (see loadSkins) — buildNode special-cases it and every joint
	// node instead of building them as plain groups.
	parent       []int
	skeletons    []scenes.Skeleton
	jointOwner   map[int]int
	jointBoneIdx map[int]int
	attachNode   []int
	attachSkin   map[int]int // reverse of attachNode: glTF node index -> skin index

	// morphedMeshes maps a glTF node index to the scene meshes its primitives with
	// morph targets became: what an animation of the node's weights drives.
	morphedMeshes map[int][]morphedMesh
}

// morphedMesh is a scene mesh with morph targets, plain or skinned.
type morphedMesh interface {
	scenes.SceneNode
	MorphTargetCount() int
	SetMorphTargetWeights(weights []float32)
}

func (l *loader) build() (int, error) {
	l.texCache = map[texKey]textures.Texture{}
	l.morphedMeshes = map[int][]morphedMesh{}
	l.prepareTextures(l.materialTextures())
	l.loadMaterials()
	l.loadSkins()

	// Build only the nodes reachable from the loaded scene's roots (their subtrees).
	// glTF files can hold several scenes (e.g. this asset has "Extended" + "Original"
	// with duplicate nodes) — nodes not in the loaded scene must NOT be created, or
	// they'd render at identity (origin) since nothing parents them.
	l.nodes = make([]scenes.Node, len(l.doc.Nodes)) // sparse; only reachable nodes filled
	for _, ri := range l.roots() {
		l.scene.Add(l.buildNode(ri))
	}

	// Drop the loader's references now that ownership has transferred: each Mesh holds
	// its own material copy, and each material holds its own color-map texture copy.
	for _, m := range l.materials {
		m.Release()
	}
	for _, t := range l.allTex {
		if t.IsValid() {
			t.Release()
		}
	}
	return l.added, nil
}

// buildNode creates the scene node for a glTF node and its subtree, in one of
// three ways:
//
//   - A skin's attach node (see loadSkins) becomes that skin's scenes.Skeleton — its
//     local transform is set on the Skeleton's own node, and its joint children
//     are skipped (NewSkeleton already parented them).
//   - A joint node becomes its pre-built scenes.Bone (from the same NewSkeleton
//     call); non-joint children (rare — e.g. a prop attached to a hand bone) are
//     built normally and parented under it.
//   - Anything else becomes a plain group, with a Mesh/SkinnedMesh child per
//     triangle primitive if it has one.
//
// Returns the node to be parented by the caller.
func (l *loader) buildNode(idx int) scenes.Node {
	if l.nodes[idx].IsValid() {
		return l.nodes[idx] // already built (defensive: a node with two parents)
	}
	gn := l.doc.Nodes[idx]

	if si, ok := l.attachSkin[idx]; ok {
		skel := l.skeletons[si]
		setLocal(skel.Node, gn)
		skel.Node.SetName(gn.Name)
		l.nodes[idx] = skel.Node
		for _, c := range gn.Children {
			if owner, isJoint := l.jointOwner[c]; isJoint && owner == si {
				continue // already parented by NewSkeleton
			}
			skel.Node.Add(l.buildNode(c))
		}
		return skel.Node
	}

	if si, ok := l.jointOwner[idx]; ok {
		bone := l.skeletons[si].Bone(l.jointBoneIdx[idx])
		bone.Node.SetName(gn.Name)
		l.nodes[idx] = bone.Node
		for _, c := range gn.Children {
			if owner, isJoint := l.jointOwner[c]; isJoint && owner == si {
				continue // already parented by NewSkeleton
			}
			bone.Node.Add(l.buildNode(c))
		}
		return bone.Node
	}

	node := l.scene.NewGroup().Node
	l.nodes[idx] = node
	setLocal(node, gn)
	node.SetName(gn.Name)
	if gn.Mesh != nil {
		meshes := l.addMesh(node, idx)
		if ext := gn.Extensions; ext != nil && ext.MSFTLod != nil {
			l.applyMSFTLod(meshes, ext.MSFTLod, gn.Extras)
		}
	}
	for _, c := range gn.Children {
		node.Add(l.buildNode(c))
	}
	return node
}

// applyMSFTLod wires the MSFT_lod extension (see nodeExtensions's doc comment) into
// pix's own AddLOD: lod.Ids are node indices whose own mesh becomes level 1, 2, ...
// on top of level 0 (meshes, this node's already-built mesh). Only supports a node
// whose glTF mesh has exactly one triangle primitive and isn't skinned — the same
// restriction meshGeoMat's alternates are held to, since AddLOD's per-level slots are
// one geometry/material each, not a multi-primitive sub-mesh list.
//
// MSFT_screencoverage (in extras, one value per level) is a fraction of screen area,
// not a world-space distance — pix's AddLOD wants the latter. There is no exact
// conversion without the viewer's FOV and viewport (which the extension doesn't
// carry), so this uses the standard approximation that a sphere of radius r subtends
// roughly (r/distance)^2 of the screen: distance = r / sqrt(coverage). Falls back to
// an arbitrary but monotonically increasing distance per level if coverage hints are
// absent, so a coverage-less MSFT_lod asset still loads instead of panicking.
//
// msftLodHysteresisFrac sets a real hysteresis band (a fraction of the computed
// distance) rather than leaving it at pix's default of 0 — without it, an object
// whose distance from the camera sits right at a level boundary flips level every
// single frame on the slightest camera jitter, which for two levels whose geometry
// isn't pixel-identical (simplification can shift vertices even slightly) looks
// exactly like z-fighting as the two heights alternate, even though only one level
// ever actually renders in any given frame. See the LOD spec's hysteresis design.
const msftLodHysteresisFrac = 0.15

func (l *loader) applyMSFTLod(meshes []scenes.Mesh, lod *msftLod, extras *nodeExtras) {
	if len(meshes) != 1 {
		panic("pix/gltf: MSFT_lod is only supported on a node with exactly one non-skinned triangle primitive")
	}
	m := meshes[0]
	radius := m.BoundingSphere().Radius
	var coverage []float32
	if extras != nil {
		coverage = extras.MSFTScreenCoverage
	}
	var lastDistance float32
	for i, id := range lod.Ids {
		if id < 0 || id >= len(l.doc.Nodes) {
			panic(fmt.Sprintf("pix/gltf: MSFT_lod.ids[%d]=%d out of range", i, id))
		}
		alt := l.doc.Nodes[id]
		if alt.Mesh == nil {
			panic(fmt.Sprintf("pix/gltf: MSFT_lod alternate node %d has no mesh", id))
		}
		geo, mat := l.meshGeoMat(*alt.Mesh)
		minDistance := radius * 2 * float32(i+1)
		if i < len(coverage) && coverage[i] > 0 {
			minDistance = radius / float32(math.Sqrt(float64(coverage[i])))
		}
		lastDistance = minDistance
		m.AddLOD(geo, mat, minDistance)
		geo.Release() // AddLOD copies its own reference, same as NewMesh
	}
	// Scaled off the outermost (largest) threshold, not the innermost: a fixed
	// fraction of a small near threshold would barely widen the far one at all, and
	// erring toward a stickier-than-needed near boundary is harmless, while too
	// little hysteresis at the far boundary is exactly the flicker this exists to
	// prevent.
	m.SetLODHysteresis(lastDistance * msftLodHysteresisFrac)
}

// meshGeoMat builds a bare geometry + resolves the material for meshIdx's single
// triangle primitive, without creating any scene node — used for an MSFT_lod
// alternate, which only ever needs to feed AddLOD, never its own Mesh/node.
func (l *loader) meshGeoMat(meshIdx int) (geometries.Geometry, materials.Material) {
	gm := l.doc.Meshes[meshIdx]
	if len(gm.Primitives) != 1 {
		panic("pix/gltf: MSFT_lod alternates must reference a single-primitive mesh")
	}
	data, _ := l.buildData(gm.Primitives[0])
	geo := l.renderer.GeometryStore.Create(data)
	mat := l.materialFor(gm.Primitives[0].Material)
	return geo, mat
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

// addMesh creates a Mesh (or, when a primitive carries skin data and the node
// references a skin, a SkinnedMesh bound to that skin's scenes.Skeleton) child per
// triangle primitive of the node's mesh, parented under parent. Primitives with morph
// targets start at the node's default weights and are recorded in morphedMeshes, for
// the node's weight animations to drive. Each mesh takes the glTF mesh's name, or the
// node's if the mesh has none. Returns the plain (non-skinned) Mesh handles
// it created, in primitive order — used by applyMSFTLod, which only ever expects
// exactly one.
func (l *loader) addMesh(parent scenes.Node, nodeIdx int) []scenes.Mesh {
	gn := l.doc.Nodes[nodeIdx]
	gm := l.doc.Meshes[*gn.Mesh]
	var skel scenes.Skeleton
	hasSkel := gn.Skin != nil && *gn.Skin >= 0 && *gn.Skin < len(l.skeletons)
	if hasSkel {
		skel = l.skeletons[*gn.Skin]
	}
	names := targetNames(gm, gn)
	weights := defaultMorphWeights(gm, gn)
	meshName := gm.Name
	if meshName == "" {
		meshName = gn.Name
	}

	var meshes []scenes.Mesh
	for _, prim := range gm.Primitives {
		mode := 4
		if prim.Mode != nil {
			mode = *prim.Mode
		}
		if mode != 4 { // triangles only
			continue
		}
		data, skinned := l.buildData(prim)
		if len(data.Attributes) == 0 {
			continue
		}
		vertexCount := len(geometries.AttributeData[glm.Vec3f](data.Attributes[0]))
		data.MorphTargets = l.morphTargets(prim, vertexCount, names)
		geo := l.renderer.GeometryStore.Create(data)
		mat := l.materialFor(prim.Material)
		var morphed morphedMesh
		if hasSkel && skinned {
			sm := l.scene.NewSkinnedMesh(geo, mat, skel)
			geo.Release() // the mesh holds its own copy
			sm.SetName(meshName)
			parent.Add(sm)
			morphed = sm
		} else {
			m := l.scene.NewMesh(geo, mat)
			geo.Release()
			m.SetName(meshName)
			parent.Add(m)
			meshes = append(meshes, m)
			morphed = m
		}
		if len(data.MorphTargets) > 0 {
			if len(weights) == len(data.MorphTargets) {
				morphed.SetMorphTargetWeights(weights)
			}
			l.morphedMeshes[nodeIdx] = append(l.morphedMeshes[nodeIdx], morphed)
		}
		l.added++
	}
	return meshes
}

// buildData reads a primitive's attributes into a GeometryConfig. The returned
// bool reports whether it carries both skin index and weight attributes (JOINTS_0
// componentType UNSIGNED_BYTE or UNSIGNED_SHORT; WEIGHTS_0 FLOAT — normalized
// UBYTE/USHORT weights are not supported) — addMesh uses it to decide Mesh vs
// SkinnedMesh independent of whether the owning node even references a skin.
func (l *loader) buildData(prim primitive) (geometries.GeometryConfig, bool) {
	var positions, normals []glm.Vec3f
	var colors []glm.Vec4f
	var uvs []glm.Vec2f
	var joints []glm.Vec4[uint16]
	var weights []glm.Vec4f
	var indices []uint32
	if prim.Indices != nil {
		indices = l.readIndices(*prim.Indices)
	}
	for name, acc := range prim.Attributes {
		switch name {
		case "POSITION":
			positions = castTo[glm.Vec3f](l.accessorBytes(acc))
		case "NORMAL":
			normals = castTo[glm.Vec3f](l.accessorBytes(acc))
		case "TEXCOORD_0":
			uvs = castTo[glm.Vec2f](l.accessorBytes(acc))
		case "COLOR_0":
			if a := l.doc.Accessors[acc]; a.ComponentType == 5126 { // FLOAT only
				if a.Type == "VEC4" {
					colors = castTo[glm.Vec4f](l.accessorBytes(acc))
				} else if a.Type == "VEC3" {
					v3 := castTo[glm.Vec3f](l.accessorBytes(acc))
					colors = make([]glm.Vec4f, len(v3))
					for i, c := range v3 {
						colors[i] = glm.Vec4f{c[0], c[1], c[2], 1}
					}
				}
			}
		case "JOINTS_0":
			joints = l.readJoints(acc)
		case "WEIGHTS_0":
			if a := l.doc.Accessors[acc]; a.ComponentType == 5126 { // FLOAT only
				weights = castTo[glm.Vec4f](l.accessorBytes(acc))
			}
		}
	}
	if len(positions) == 0 {
		return geometries.GeometryConfig{}, false
	}
	attrs := []geometries.Attribute{geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, positions)}
	if len(normals) > 0 {
		attrs = append(attrs, geometries.NewAttribute(geometries.AttributeNormal, geometries.Float32x3, normals))
	}
	if len(colors) > 0 {
		attrs = append(attrs, geometries.NewAttribute(geometries.AttributeColor, geometries.Float32x4, colors))
	}
	if len(uvs) > 0 {
		attrs = append(attrs, geometries.NewAttribute(geometries.AttributeUV, geometries.Float32x2, uvs))
	}
	skinned := len(joints) == len(positions) && len(weights) == len(positions) && len(positions) > 0
	if skinned {
		attrs = append(attrs, geometries.NewAttribute(geometries.AttributeSkinIndex, geometries.Uint16x4, joints))
		attrs = append(attrs, geometries.NewAttribute(geometries.AttributeSkinWeight, geometries.Float32x4, weights))
	}
	return geometries.GeometryConfig{Attributes: attrs, Indices: indices}, skinned
}

// ---- skinning ----

// loadSkins builds a scenes.Skeleton for every glTF skin, upfront (before the scene
// graph traversal — a skin's joints, parenting and bind pose come entirely from
// raw doc data, not from already-built scene nodes, so nothing here depends on
// traversal order).
//
// For each skin: a joint's skeleton-relative parent is its nearest ancestor that
// is itself a joint of the same skin (walking up via parent[]), or -1 if none —
// so intervening non-joint nodes (rare) collapse into the bind-pose transform
// instead of becoming extra bones. Its bind-pose local transform is therefore the
// composition of every node's local matrix from that ancestor (exclusive) down to
// the joint (inclusive) — see relativeMatrix. A joint with no joint ancestor is a
// root; its glTF parent (skin.Skeleton if given, else the first root joint's
// direct parent) becomes the skin's "attach node" — buildNode places the
// Skeleton there instead of a plain group, and skips the joint nodes.
func (l *loader) loadSkins() {
	if len(l.doc.Skins) == 0 {
		return
	}
	l.buildParentMap()
	l.skeletons = make([]scenes.Skeleton, len(l.doc.Skins))
	l.jointOwner = map[int]int{}
	l.jointBoneIdx = map[int]int{}
	l.attachNode = make([]int, len(l.doc.Skins))
	l.attachSkin = map[int]int{}

	for si, sk := range l.doc.Skins {
		isJoint := make(map[int]bool, len(sk.Joints))
		jointIdx := make(map[int]int, len(sk.Joints))
		for i, j := range sk.Joints {
			isJoint[j] = true
			jointIdx[j] = i
			l.jointOwner[j] = si
			l.jointBoneIdx[j] = i
		}

		n := len(sk.Joints)
		parents := make([]int32, n)
		attach := -1
		if sk.Skeleton != nil {
			attach = *sk.Skeleton
		}
		for i, j := range sk.Joints {
			p := l.parent[j]
			for p != -1 && !isJoint[p] {
				p = l.parent[p]
			}
			if p == -1 {
				parents[i] = -1
				if attach == -1 {
					attach = l.parent[j] // direct (possibly non-joint) parent
				}
			} else {
				parents[i] = int32(jointIdx[p])
			}
		}

		names := make([]string, n)
		bindPose := make([]scenes.Transform, n)
		for i, j := range sk.Joints {
			names[i] = l.doc.Nodes[j].Name
			stopAt := attach
			if parents[i] >= 0 {
				stopAt = sk.Joints[parents[i]]
			}
			pos, rot, scale := decomposeMatrix(l.relativeMatrix(j, stopAt))
			bindPose[i] = scenes.Transform{Position: pos, Rotation: rot, Scale: scale}
		}

		invBind := make([]glm.Mat4f, n)
		for i := range invBind {
			invBind[i] = glm.Mat4fIdentity
		}
		if sk.InverseBindMatrices != nil {
			m := castTo[glm.Mat4f](l.accessorBytes(*sk.InverseBindMatrices))
			copy(invBind, m)
		}

		l.skeletons[si] = l.scene.NewSkeleton(scenes.SkeletonConfig{
			Names: names, Parents: parents, InverseBind: invBind, BindPose: bindPose,
		})
		l.attachNode[si] = attach
		if attach >= 0 {
			l.attachSkin[attach] = si
		}
	}
}

// buildParentMap fills l.parent[i] with node i's glTF parent index, or -1.
func (l *loader) buildParentMap() {
	l.parent = make([]int, len(l.doc.Nodes))
	for i := range l.parent {
		l.parent[i] = -1
	}
	for i, n := range l.doc.Nodes {
		for _, c := range n.Children {
			l.parent[c] = i
		}
	}
}

// relativeMatrix composes local matrices from stopAt (exclusive) down to idx
// (inclusive), root-to-leaf — i.e. idx's transform relative to stopAt's space,
// exactly as if stopAt's own world transform were identity.
func (l *loader) relativeMatrix(idx, stopAt int) glm.Mat4f {
	var chain []int
	for cur := idx; cur != stopAt && cur != -1; cur = l.parent[cur] {
		chain = append(chain, cur)
	}
	m := glm.Mat4fIdentity
	for i := len(chain) - 1; i >= 0; i-- {
		m = m.Mul4x4(nodeLocalMatrix(l.doc.Nodes[chain[i]]))
	}
	return m
}

// nodeLocalMatrix returns a glTF node's local transform as a matrix (its
// authored matrix, or its composed TRS).
func nodeLocalMatrix(gn node) glm.Mat4f {
	if len(gn.Matrix) == 16 {
		var m glm.Mat4f
		copy(m[:], gn.Matrix)
		return m
	}
	pos, rot, scale := glm.Vec3f{}, glm.QuatfIdentity, glm.Vec3f{1, 1, 1}
	if len(gn.Translation) == 3 {
		pos = glm.Vec3f{gn.Translation[0], gn.Translation[1], gn.Translation[2]}
	}
	if len(gn.Rotation) == 4 {
		rot = glm.Quatf{gn.Rotation[0], gn.Rotation[1], gn.Rotation[2], gn.Rotation[3]}
	}
	if len(gn.Scale) == 3 {
		scale = glm.Vec3f{gn.Scale[0], gn.Scale[1], gn.Scale[2]}
	}
	return glm.Transform(scale, rot, pos)
}

// readJoints decodes a JOINTS_0 accessor (UNSIGNED_BYTE or UNSIGNED_SHORT) into
// skeleton-relative joint indices.
func (l *loader) readJoints(idx int) []glm.Vec4[uint16] {
	acc := l.doc.Accessors[idx]
	raw := l.accessorBytes(idx)
	out := make([]glm.Vec4[uint16], acc.Count)
	switch acc.ComponentType {
	case 5121: // UNSIGNED_BYTE
		for i := range out {
			b := raw[i*4 : i*4+4]
			out[i] = glm.Vec4[uint16]{uint16(b[0]), uint16(b[1]), uint16(b[2]), uint16(b[3])}
		}
	case 5123: // UNSIGNED_SHORT
		for i := range out {
			b := raw[i*8 : i*8+8]
			out[i] = glm.Vec4[uint16]{
				binary.LittleEndian.Uint16(b[0:]), binary.LittleEndian.Uint16(b[2:]),
				binary.LittleEndian.Uint16(b[4:]), binary.LittleEndian.Uint16(b[6:]),
			}
		}
	}
	return out
}

// ---- animation ----

// loadAnimations builds a scenes.AnimationClip per glTF animation. Must run after
// the scene graph traversal (build's node loop) — a track's Target is resolved
// directly to the already-built scene node/bone, not a name — or, for a "weights"
// channel, to the node's morphed meshes. Channels targeting an unbuilt node
// (unreachable from the loaded scene) are skipped; CUBICSPLINE samplers are treated
// as LINEAR over their
// value keys (the in/out tangents are ignored) — not spec-exact, but avoids
// silently misreading the 3x-wider CUBICSPLINE output layout as flat keys.
func (l *loader) loadAnimations() []*scenes.AnimationClip {
	clips := make([]*scenes.AnimationClip, 0, len(l.doc.Animations))
	for _, ga := range l.doc.Animations {
		clip := &scenes.AnimationClip{Name: ga.Name}
		for _, ch := range ga.Channels {
			if ch.Target.Node == nil || ch.Sampler < 0 || ch.Sampler >= len(ga.Samplers) {
				continue
			}
			targets, channel, ok := l.animTargets(*ch.Target.Node, ch.Target.Path)
			if !ok {
				continue
			}
			sampler := ga.Samplers[ch.Sampler]
			interp := scenes.InterpLinear
			if sampler.Interpolation == "STEP" {
				interp = scenes.InterpStep
			}
			times := castTo[float32](l.accessorBytes(sampler.Input))
			values := l.animValues(sampler, floatsPerKey(channel, targets), len(times))
			if len(times) == 0 || len(values) == 0 {
				continue
			}
			if last := times[len(times)-1]; last > clip.Duration {
				clip.Duration = last
			}
			// A node's weights drive every primitive of its mesh; the tracks share
			// their keys.
			for _, target := range targets {
				clip.Tracks = append(clip.Tracks, scenes.Track{
					Target: target, Channel: channel, Interp: interp, Times: times, Values: values,
				})
			}
		}
		clips = append(clips, clip)
	}
	return clips
}

// animTargets resolves a channel's target node and path to the scene nodes its
// tracks drive and the channel they animate: the node itself for its transform, or
// its mesh's morphed primitives for "weights". false if there is nothing to drive.
func (l *loader) animTargets(nodeIdx int, path string) ([]scenes.SceneNode, scenes.Channel, bool) {
	var channel scenes.Channel
	switch path {
	case "translation":
		channel = scenes.ChannelPosition
	case "rotation":
		channel = scenes.ChannelRotation
	case "scale":
		channel = scenes.ChannelScale
	case "weights":
		meshes := l.morphedMeshes[nodeIdx]
		targets := make([]scenes.SceneNode, len(meshes))
		for i, m := range meshes {
			targets[i] = m
		}
		return targets, scenes.ChannelMorphWeights, len(targets) > 0
	default:
		return nil, 0, false
	}
	target, ok := l.animTarget(nodeIdx)
	if !ok {
		return nil, 0, false
	}
	return []scenes.SceneNode{target}, channel, true
}

// floatsPerKey is how many floats one key of a channel holds.
func floatsPerKey(channel scenes.Channel, targets []scenes.SceneNode) int {
	switch channel {
	case scenes.ChannelRotation:
		return 4
	case scenes.ChannelMorphWeights:
		return targets[0].(morphedMesh).MorphTargetCount()
	}
	return 3
}

// animValues reads a sampler's output accessor into a flat []float32 of
// floatsPerKey floats per key, extracting only the value (middle third) of each
// key when the sampler is CUBICSPLINE. Outputs may be quantized integers (see
// accessorFloats).
func (l *loader) animValues(sampler animSampler, floatsPerKey, keyCount int) []float32 {
	all := l.accessorFloats(sampler.Output)
	if sampler.Interpolation == "CUBICSPLINE" {
		if len(all) != keyCount*floatsPerKey*3 {
			return nil
		}
		out := make([]float32, keyCount*floatsPerKey)
		for i := range keyCount {
			copy(out[i*floatsPerKey:], all[i*floatsPerKey*3+floatsPerKey:i*floatsPerKey*3+floatsPerKey*2])
		}
		return out
	}
	if len(all) != keyCount*floatsPerKey {
		return nil
	}
	return all
}

// animTarget resolves a glTF node index to the scene handle its track should
// drive: the pre-built scenes.Bone if it's a joint, otherwise its built scene node.
// false if the node was never built (unreachable from the loaded scene).
func (l *loader) animTarget(nodeIdx int) (scenes.SceneNode, bool) {
	if si, ok := l.jointOwner[nodeIdx]; ok {
		return l.skeletons[si].Bone(l.jointBoneIdx[nodeIdx]), true
	}
	if nodeIdx < 0 || nodeIdx >= len(l.nodes) || !l.nodes[nodeIdx].IsValid() {
		return nil, false
	}
	return l.nodes[nodeIdx], true
}

// ---- textures & materials ----

// texture decodes + uploads a glTF texture for a given usage (deduped by texture
// index + usage), returning a handle (zero if missing/undecodable).
//
// Usage — not just color space — is what the loader has to decide here, because it
// determines the GPU format: only the call site knows an image is a normal map, and
// a normal map wants two channels with Z reconstructed in the shader rather than a
// wasted third. See textures.Format.
func (l *loader) texture(idx int, usage textures.Format) textures.Texture {
	if idx < 0 || idx >= len(l.doc.Textures) {
		return textures.Texture{}
	}
	key := texKey{idx, usage}
	if t, ok := l.texCache[key]; ok {
		return t
	}
	img, err := l.prepareTexture(key)
	l.uploadTexture(key, img, err)
	return l.texCache[key]
}

// materialTextures is every texture the document's materials use, each with the usage
// it is uploaded for, once each.
func (l *loader) materialTextures() []texKey {
	var keys []texKey
	seen := map[texKey]bool{}
	add := func(ref *textureRef, usage textures.Format) {
		if ref == nil {
			return
		}
		key := texKey{ref.Index, usage}
		if !seen[key] && key.index >= 0 && key.index < len(l.doc.Textures) {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	for _, gm := range l.doc.Materials {
		if gm.Extensions != nil && gm.Extensions.Transmission != nil {
			add(gm.Extensions.Transmission.TransmissionTexture, textures.Grayscale)
		}
		add(gm.NormalTexture, textures.Normal)
		add(gm.EmissiveTexture, textures.SRGB)
		if gm.OcclusionTexture != nil {
			add(&gm.OcclusionTexture.textureRef, textures.Linear)
		}
		if pbr := gm.PbrMetallicRoughness; pbr != nil {
			add(pbr.BaseColorTexture, textures.SRGB)
			add(pbr.MetallicRoughnessTexture, textures.Linear)
		}
	}
	return keys
}

// prepareTextures decodes and prepares every one of keys on all the CPU's cores, and
// uploads each as it is ready — on this goroutine, which is the renderer's. Decoding and
// building mip chains is most of what loading a large scene costs, and none of it needs
// the others; uploading as they come keeps no more than a few prepared at a time in
// memory, where a scene's worth of mip chains would run to gigabytes.
func (l *loader) prepareTextures(keys []texKey) {
	type prepared struct {
		key texKey
		img textures.Image
		err error
	}
	workers := runtime.NumCPU()
	jobs := make(chan texKey)
	results := make(chan prepared, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range jobs {
				img, err := l.prepareTexture(key)
				results <- prepared{key: key, img: img, err: err}
			}
		}()
	}
	go func() {
		for _, key := range keys {
			jobs <- key
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	for p := range results {
		l.uploadTexture(p.key, p.img, p.err)
	}
}

// prepareTexture decodes the image key names and prepares it for its usage. It reads
// only the document and its buffers, so it runs on any goroutine.
func (l *loader) prepareTexture(key texKey) (textures.Image, error) {
	gt := l.doc.Textures[key.index]
	if gt.Source == nil {
		return textures.Image{}, fmt.Errorf("texture %d has no image", key.index)
	}
	img, err := l.decodeImage(*gt.Source)
	if err != nil {
		return textures.Image{}, err
	}
	return textures.Prepare(img, key.usage, l.options.MipChain), nil
}

// uploadTexture uploads img for key, or with err records that key has no texture, so
// it is not tried again.
func (l *loader) uploadTexture(key texKey, img textures.Image, err error) {
	if err != nil {
		l.texCache[key] = textures.Texture{}
		return
	}
	t := l.renderer.TextureStore.Upload(img)
	l.texCache[key] = t
	l.allTex = append(l.allTex, t)
}

func (l *loader) loadMaterials() {
	// glTF materials are metallic-roughness PBR → load them as PBR materials with
	// their base-color, normal, and metallic-roughness maps.
	samp := l.renderer.TextureStore.DefaultSampler()
	l.materials = make([]*materials.PBRMaterial, len(l.doc.Materials)+1)
	def := l.renderer.NewPBRMaterial()
	def.SetRoughness(1)
	l.materials[0] = def
	for i, gm := range l.doc.Materials {
		m := l.renderer.NewPBRMaterial()
		m.SetMetallic(1)
		m.SetRoughness(1)
		// alphaMode BLEND → transparent (src-alpha over); MASK → opaque where alpha
		// reaches alphaCutoff, and no surface elsewhere; OPAQUE, the default → alpha
		// ignored. Only BLEND takes alpha as how much of the surface is there: glass
		// declared OPAQUE, with a colour map whose alpha is 0, is still whole glass.
		m.SetIgnoresAlpha(gm.AlphaMode != "BLEND")
		switch gm.AlphaMode {
		case "BLEND":
			m.SetBlend(materials.BlendAlpha)
		case "MASK":
			cutoff := float32(0.5)
			if gm.AlphaCutoff != nil {
				cutoff = *gm.AlphaCutoff
			}
			m.SetAlphaCutoff(cutoff)
		}
		// KHR_materials_transmission (glass): diffuse suppressed, blended over the
		// scene behind it (SetTransmission picks the blend mode). A transmission
		// factor > 0 makes the surface see-through.
		if ext := gm.Extensions; ext != nil && ext.Transmission != nil {
			tf := float32(1)
			if ext.Transmission.TransmissionFactor != nil {
				tf = *ext.Transmission.TransmissionFactor
			}
			if tf > 0 {
				m.SetTransmission(tf)
				// The mask matters more than the factor for most assets: these
				// materials are usually declared OPAQUE with a factor of 1 and a
				// texture that is glass in only a few places. Loading the factor
				// alone turns the entire object into a ghost.
				if tt := ext.Transmission.TransmissionTexture; tt != nil {
					// Single-channel data (the extension reads red), so upload it
					// as R8 rather than paying for four channels.
					if t := l.texture(tt.Index, textures.Grayscale); t.IsValid() {
						m.SetTransmissionMap(t)
						m.SetTransmissionMapSampler(samp)
					}
				}
			}
		}
		// KHR_materials_ior: how much the surface reflects head-on, and how sharply it
		// bends what passes through it.
		if ext := gm.Extensions; ext != nil && ext.IOR != nil && ext.IOR.IOR != nil {
			m.SetIOR(*ext.IOR.IOR)
		}
		// KHR_materials_volume: the volume behind a transmissive surface. glTF measures
		// its thickness in the mesh's own units, and pix in world units, which agree for
		// a mesh at the scale it was made at.
		if ext := gm.Extensions; ext != nil && ext.Volume != nil {
			m.SetThickness(ext.Volume.ThicknessFactor)
			color := colors.RGB32F{1, 1, 1}
			if c := ext.Volume.AttenuationColor; len(c) == 3 {
				color = colors.RGB32F{c[0], c[1], c[2]}
			}
			// glTF's default, an infinite distance, absorbs nothing, as pix's 0 does.
			distance := float32(0)
			if d := ext.Volume.AttenuationDistance; d != nil {
				distance = *d
			}
			m.SetAttenuation(color, distance)
		}
		if gm.NormalTexture != nil {
			if t := l.texture(gm.NormalTexture.Index, textures.Normal); t.IsValid() {
				m.SetNormalMap(t)
				m.SetNormalMapSampler(samp)
			}
		}
		// The light the surface emits: its emissive factor, scaled by
		// KHR_materials_emissive_strength, times its emissive texture.
		if f := gm.EmissiveFactor; len(f) == 3 {
			strength := float32(1)
			if ext := gm.Extensions; ext != nil && ext.EmissiveStrength != nil {
				strength = ext.EmissiveStrength.EmissiveStrength
			}
			m.SetEmissive(colors.RGB32F{f[0] * strength, f[1] * strength, f[2] * strength})
		}
		if gm.EmissiveTexture != nil {
			if t := l.texture(gm.EmissiveTexture.Index, textures.SRGB); t.IsValid() {
				m.SetEmissiveMap(t)
				m.SetEmissiveMapSampler(samp)
			}
		}
		if gm.OcclusionTexture != nil {
			if t := l.texture(gm.OcclusionTexture.Index, textures.Linear); t.IsValid() {
				m.SetOcclusionMap(t)
				m.SetOcclusionMapSampler(samp)
			}
			if gm.OcclusionTexture.Strength != nil {
				m.SetOcclusionStrength(*gm.OcclusionTexture.Strength)
			}
		}
		if pbr := gm.PbrMetallicRoughness; pbr != nil {
			if len(pbr.BaseColorFactor) >= 4 {
				m.SetColor(colors.RGBA32F{pbr.BaseColorFactor[0], pbr.BaseColorFactor[1], pbr.BaseColorFactor[2], pbr.BaseColorFactor[3]})
			}
			if pbr.MetallicFactor != nil {
				m.SetMetallic(*pbr.MetallicFactor)
			}
			if pbr.RoughnessFactor != nil {
				m.SetRoughness(*pbr.RoughnessFactor)
			}
			if pbr.BaseColorTexture != nil {
				if t := l.texture(pbr.BaseColorTexture.Index, textures.SRGB); t.IsValid() {
					m.SetColorMap(t)
					m.SetColorMapSampler(samp)
				}
			}
			// One combined metallic-roughness texture (glTF: .b metallic, .g roughness)
			// feeds both independent map slots.
			if pbr.MetallicRoughnessTexture != nil {
				if t := l.texture(pbr.MetallicRoughnessTexture.Index, textures.Linear); t.IsValid() {
					m.SetMetallicMap(t)
					m.SetMetallicMapSampler(samp)
					m.SetRoughnessMap(t)
					m.SetRoughnessMapSampler(samp)
				}
			}
		}
		l.materials[i+1] = m
	}
}

// materialFor returns the (embedded) Material for a glTF material index; NewMesh
// takes its own copy, and the loader releases these builders after building.
func (l *loader) materialFor(ptr *int) materials.Material {
	if ptr == nil || *ptr+1 >= len(l.materials) {
		return l.materials[0]
	}
	return l.materials[*ptr+1]
}

func (l *loader) decodeImage(idx int) (img image.Image, err error) {
	if idx >= len(l.doc.Images) {
		return nil, fmt.Errorf("image %d out of range", idx)
	}
	gi := l.doc.Images[idx]
	var raw []byte
	if gi.BufferView != nil {
		bv := l.doc.BufferViews[*gi.BufferView]
		raw = l.buffers[bv.Buffer][bv.ByteOffset : bv.ByteOffset+bv.ByteLength]
	} else {
		if raw, err = l.resolveURI(gi.URI); err != nil {
			return nil, err
		}
	}
	img, _, err = image.Decode(bytes.NewReader(raw))
	return img, err
}

// ---- node transforms ----

// setLocal sets a scene node's local transform from a glTF node's TRS (or matrix,
// which is decomposed).
func setLocal(n scenes.Node, gn node) {
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
		return pos, glm.QuatfIdentity, scale
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

// accessorBytes returns an accessor's elements tightly packed, with any sparse
// overrides applied. The result may alias the loaded buffer, so it must not be
// modified.
func (l *loader) accessorBytes(idx int) []byte {
	acc := l.doc.Accessors[idx]
	elemSize := componentSize(acc.ComponentType) * typeComponents(acc.Type)
	dense := l.denseAccessorBytes(acc, elemSize)
	if acc.Sparse == nil {
		return dense
	}
	return l.applySparse(acc, elemSize, dense)
}

// applySparse returns a copy of dense with the accessor's sparse elements replaced.
func (l *loader) applySparse(acc accessor, elemSize int, dense []byte) []byte {
	out := append([]byte(nil), dense...)
	sparse := acc.Sparse
	indexView := l.doc.BufferViews[sparse.Indices.BufferView]
	indices := l.buffers[indexView.Buffer][indexView.ByteOffset+sparse.Indices.ByteOffset:]
	valueView := l.doc.BufferViews[sparse.Values.BufferView]
	values := l.buffers[valueView.Buffer][valueView.ByteOffset+sparse.Values.ByteOffset:]
	for i := range sparse.Count {
		var element int
		switch sparse.Indices.ComponentType {
		case 5121: // UNSIGNED_BYTE
			element = int(indices[i])
		case 5123: // UNSIGNED_SHORT
			element = int(binary.LittleEndian.Uint16(indices[i*2:]))
		default: // 5125 UNSIGNED_INT
			element = int(binary.LittleEndian.Uint32(indices[i*4:]))
		}
		if element >= acc.Count {
			continue
		}
		copy(out[element*elemSize:(element+1)*elemSize], values[i*elemSize:])
	}
	return out
}

// denseAccessorBytes returns an accessor's buffer view elements tightly packed, or
// zeros when it has no buffer view.
func (l *loader) denseAccessorBytes(acc accessor, elemSize int) []byte {
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
	for i := range acc.Count {
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
