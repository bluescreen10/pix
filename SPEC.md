# pix — Scene Graph & Renderer Architecture Spec

## Goals

Build a 3D renderer in Go that exposes an ergonomic scene-graph API (`parent.Add(child)`, typed node kinds, hierarchical transforms) while avoiding the per-frame full-tree traversal that hurts performance in naive implementations.

The design rests on three principles:

1. **Public API is a tree; internal storage is flat.** Users see a familiar scene graph; the engine sees structure-of-arrays.
2. **Stable handles, not pointers.** Generation-counted IDs let storage move freely without invalidating user code.
3. **Separate ownership by lifetime.** Scenes own nodes. Renderer owns GPU resources. Loaders depend on both, owned by neither.

---

## Core Storage Model

### Shared per-node arrays (Scene-owned)

Every node, regardless of kind, has an entry in the scene's parallel arrays, indexed by slot:

```go
type Scene struct {
    // Hierarchy
    parent, firstChild, lastChild   []NodeID
    nextSibling, prevSibling        []NodeID

    // Transforms (all index by the same key) Stored cached friendly
    local, world []glm.Mat4f

    rotations []glm.Quatf
    positions []glm.Vec3f
    scales    []glm.Vec3f

    // State
    flags      []uint32   // alive | visible | castShadow | receiveShadow | dirty | static | visibleEffective
    generation []uint32   // bumped on destroy; validates handles
    kind       []NodeKind
    payload    []uint32   // index into the kind-specific table

    // Allocator
    freeHead   uint32     // intrusive free list threaded through parent[] of dead slots

    // Iteration
    topoOrder  []uint32   // parent-before-child; rebuilt lazily on topology change
    topoDirty  bool

    root NodeID
}
```

These arrays may contain gaps (destroyed slots). That's intentional — slot stability is what makes handles work. The render passes never iterate these arrays directly; they iterate `topoOrder` (gapless, live-only) or the payload tables (compact).

### Per-kind payload tables (Scene-owned)

Type-specific data lives in separate compact arrays:

```go
meshes        []Meshes          // {geometryID, materialID, boundsLocal, ownerNode}
skinned       []SkinnedMesh   // mesh + skeletonID + boneMatricesOffset
dirLights     []DirectionalLight  // {color, intensity, direction, shadowMapIdx}
pointLights   []PointLight        // {color, intensity, range, decay}
ambientLights []AmbientLight
cameras       []Camera
```

Each payload entry stores an `OwnerNode uint32` back-pointer for swap-remove on destroy.

### Handles

```go
type NodeID     struct { index, gen uint32 }
type GeometryID struct { index, gen uint32 }
type MaterialID struct { index, gen uint32 }
type TextureID  struct { index, gen uint32 }
type SamplerID  struct { index, gen uint32 }
```

All handles are 8-byte values. Zero value is invalid (generations start at 1). Stale handles fail their generation check on next use.

---

## Node API

`Node` is a value-type handle, not a struct that owns data. Methods are thin lookups validated by the generation counter.

```go
type Node struct {
    scene *Scene
    id    NodeID
}

func (n Node) Add(child Node)
func (n Node) Remove(child Node)
func (n Node) Children() []Node                    // allocates
func (n Node) ForEachChild(fn func(Node) bool)     // zero-alloc
func (n Node) Parent() Node
func (n Node) Transform() Mat4                     // local
func (n Node) WorldTransform() Mat4
func (n Node) SetTransform(m Mat4)                 // marks subtree dirty
func (n Node) Visible() bool
func (n Node) SetVisible(b bool)
func (n Node) CastShadow() bool
func (n Node) SetCastShadow(b bool)
func (n Node) ReceiveShadow() bool
func (n Node) SetReceiveShadow(b bool)
func (n Node) UpdateMatrix(force bool)             // only sets dirty bit; does not recurse
func (n Node) Destroy()                            // destroys subtree
```

### Typed node handles

Each kind has a typed handle that embeds `Node`, inheriting all hierarchy methods:

```go
type Group             struct { Node }
type Mesh              struct { Node }
type SkinnedMesh       struct { Node }
type DirectionalLight  struct { Node }
type PointLight        struct { Node }
type AmbientLight      struct { Node }
type Camera            struct { Node }
```

Typed methods access kind-specific data:

```go
func (m Mesh) Geometry() GeometryID
func (m Mesh) Material() MaterialID
func (m Mesh) SetMaterial(MaterialID)

func (l DirectionalLightNode) Color() Color
func (l DirectionalLightNode) SetColor(Color)
func (l DirectionalLightNode) SetIntensity(float32)
```

Because `Mesh` embeds `Node`, `crate1.Add(crate2)` works without any per-kind reimplementation. Any node may parent any other — this avoids forcing intermediate Groups for simple parent/child relationships (e.g., a turret mesh with a barrel mesh child).

### Construction

Nodes are allocated through the scene (the scene owns the arrays they live in):

```go
scene := pix.NewScene()
group := scene.NewGroup()
mesh  := scene.NewMesh(geo, mat)
light := scene.NewDirectionalLight(color, intensity)

group.Add(mesh)
scene.Add(group)
scene.Add(light)
```

`scene.NewX` allocates a slot, initializes shared-array entries, appends to the relevant payload table, and returns a typed handle. By default, new nodes are orphans (not yet parented); `scene.Add` parents them under `scene.root`.

---

## Allocator

The scene owns the allocator. There is exactly one place that touches the parallel arrays and the free list:

```go
func (s *Scene) allocNode(kind NodeKind) NodeID {
    var idx uint32
    if s.freeHead != invalidIdx {
        idx = s.freeHead
        s.freeHead = s.parent[idx].index   // free list threaded through parent[]
        s.resetSlot(idx, kind)
    } else {
        idx = uint32(len(s.parent))
        // append to all parallel arrays
    }
    return NodeID{index: idx, gen: s.generation[idx]}
}
```

**Free list is threaded through `parent[]` of dead slots.** Dead slots are already useless; reusing their storage as linked-list nodes avoids a separate `freeList []uint32`.

**Destroy never moves data in shared arrays.** It clears `flagAlive`, increments `generation[i]`, swap-removes the payload entry (patching the moved entry's `OwnerNode`), and pushes the slot onto the free list. Any outstanding handle to that slot now fails its generation check.

**Slot reuse is safe.** A new alloc may reuse a destroyed slot, but with an incremented generation — old handles stay detectably stale (no ABA).

### Destroy policy for nodes with children

Default: **destroy the whole subtree.** `node.Destroy()` walks children post-order and destroys each. Provide `node.DetachChildren()` for cases where children should be preserved (reparented to grandparent or made roots).

---

## Hierarchy Operations

All add/remove/reparent operations funnel through a single internal `reparent`:

```go
func (s *Scene) reparent(child, newParent NodeID) {
    s.validate(child); s.validate(newParent)
    if s.wouldCycle(child, newParent) { panic("cycle") }
    s.detachFromParent(child)                      // no-op if no parent

    // O(1) append at tail of newParent's child list
    s.parent[child.index] = newParent
    if last := s.lastChild[newParent.index]; last.index == invalidIdx {
        s.firstChild[newParent.index] = child
    } else {
        s.nextSibling[last.index]  = child
        s.prevSibling[child.index] = last
    }
    s.lastChild[newParent.index] = child

    s.markSubtreeDirty(child.index)
    s.topoDirty = true
}
```

- `scene.Add(x)`  → `reparent(x, scene.root)`
- `parent.Add(x)` → `reparent(x, parent)`

Doubly-linked siblings (`prevSibling`/`nextSibling`) plus `lastChild` give **O(1) add and O(1) remove**, independent of sibling count.

**Cycle check is mandatory** — walk up from `newParent` to root, fail if `child` appears.

---

## Per-Frame Update Pipeline

The scene graph is not traversed per frame. Update is driven by dirty flags and the cached `topoOrder`:

```
1. flushMaterials()    // upload changed uniforms, rebuild changed bind groups/pipelines
2. flushTopoIfDirty()  // rebuild topoOrder if topology changed since last frame
3. updateTransforms()  // single linear pass over topoOrder; only dirty entries
4. updateVisibility()  // effectiveVisible[i] = local && effectiveVisible[parent]
5. cullAndBatch()      // iterate meshes[] payload table; frustum-cull; emit DrawCommands
6. sortDraws()         // by pipeline → material → geometry
7. recordAndSubmit()   // encode wgpu commands
```

### Transform update

```go
for _, i := range scene.topoOrder {
    if scene.flags[i] & flagDirty == 0 && !force { continue }
    p := scene.parent[i]
    if p.index == invalidIdx {
        scene.world[i] = scene.local[i]
    } else {
        scene.world[i] = scene.world[p.index].Mul(scene.local[i])
    }
    scene.flags[i] &^= flagDirty
}
```

`topoOrder` guarantees parents precede children, so a single forward pass is correct. Static subtrees: tag with `flagStatic` and either skip during the pass or move into a separate `staticWorld[]` block iterated only on topology change.

### Visibility propagation

`flagVisibleEffective[i] = flagVisible[i] && flagVisibleEffective[parent[i]]`, computed in the same pass. Mesh culling reads the effective bit.

---

## Renderer & GPU Resources

The renderer is a separate component that owns all GPU resources. Scenes reference resources by ID.

```go
type Renderer struct {
    device *wgpu.Device
    queue  *wgpu.Queue

    geometries []GeometryData    // {vertexBuf, indexBuf, vertexCount, boundsLocal, refcount}
    materials  []MaterialData    // {pipelineID, bindGroup, uniformBuf, uniforms, dirty, refcount}
    textures   []TextureData     // {texture, defaultView, format, mipCount, refcount}
    samplers   []SamplerData     // {sampler, refcount}

    pipelineCache map[PipelineKey]PipelineID
    samplerCache  map[SamplerDesc]SamplerID
    deferredFree  []deferredResource   // released after in-flight frame finishes
}
```

### Why GPU resources don't live in the scene

- **Shared across many nodes** — one geometry instances thousands of meshes
- **Shared across scenes** — UI and world scenes share fonts/textures
- **Lifetime is GPU-tied, not scene-tied** — destroying a scene must not free buffers other scenes use

### Geometry

```go
geo := renderer.NewBoxGeometry(1, 1, 1)
geo := renderer.NewGeometry(vertices, indices, layout)
```

Returns `GeometryID`. Vertex/index data uploaded once; reused by every mesh referencing it.

### Materials

```go
mat := renderer.NewStandardMaterial(pix.StandardMaterialDesc{
    Albedo:    Color{1, 0.5, 0.2, 1},
    Metallic:  0.0,
    Roughness: 0.5,
    AlbedoMap: tex,
})

/* return */
type StandardMaterial struct {
    renderer *Renderer
    ref       Ref[Material]
}

```

Material state is split by mutation cost:

```go
type Material struct {
    name string (optional)
    // pipeline-affecting (blend, depth, cull, shader defines)
    materialKey uint64 // today called materialFlags

    // bind-group-affecting (textures, samplers, buffer bindings)
    textures []TextureID
    sampler     SamplerID

    bindGroup   *wgpu.BindGroup
    bindGroupLayout *wgpu.BindGroupLayout

    // uniforms-only (colors, scalars)
    uniforms    []*Uniforms
    uniformBuf  []*wgpu.Buffer

    dirty       uint32   // dirtyUniforms | dirtyBindGroup | dirtyKey
}
```

Setters mutate data and set the right dirty bit:

```go
func (m StandardMaterial) SetColor(c Color)         // dirtyUniforms
func (m StandardMaterial) SetColorMap(t TextureID)    // dirtyBindGroup
func (m StandardMaterial) SetBlendMode(b BlendMode) // dirtyPipeline
```

`flushMaterials` at the top of each frame processes only what changed, at the cost tier that change requires:

- **dirtyUniforms** → `queue.WriteBuffer` of the small uniform block
- **dirtyBindGroup** → allocate new bind group from the cached layout
- **dirtyPipeline** → look up or compile a `wgpu.RenderPipeline` via the pipeline cache

Each tier costs roughly 10–100× the previous one, so granular flags ensure a color tweak doesn't pay pipeline-rebuild cost.

### Textures

```go
tex := renderer.NewTexture2D(TextureDesc{
    Width: 1024, Height: 1024,
    Format: FormatRGBA8UnormSrgb,   // explicit; no silent default
    Usage:  UsageSampled | UsageCopyDst,
    Mips:   MipsAuto,                // GPU mip generation pass
})
renderer.WriteTexture(tex, pixelData, WriteRegion{Layer: 0, Mip: 0})
```

Render-target textures use `UsageRenderAttachment` and are written by the GPU, not via `WriteTexture`. Convenience constructor `NewDepthTexture` picks correct format/usage.

Format selection is explicit. Albedo/color → `RGBA8UnormSrgb`. Normal maps and data textures → `RGBA8Unorm`. No silent defaults.

### Samplers

Separate resource (matches wgpu/WebGPU). Aggressively cached — `Sampler` hashes the descriptor and returns existing ID on match. A typical scene has 5–10 unique samplers regardless of texture count.

```go
texture.SetMinFilter(...)
mat.NeedsUpdate()
```

### Material instances (optional layer)

For per-mesh appearance variation without losing pipeline dedup:

```go
inst := mat.Clone()
inst.SetColor(Color{1, 0, 0, 1})   // only this instance's uniforms
Mesh.SetMaterial(inst.ID())
```

Each instance has its own small uniform block layered on the base material. Maps cleanly to GPU instancing.

### Reference counting

GPU resources are refcounted by node-side references:

- `scene.NewMesh(geo, mat)` increments `geometries[geo].refcount` and `materials[mat].refcount`
- `Mesh.SetMaterial(newMat)` decrements old material refcount, increments new
- Destroying a mesh node decrements both
- When refcount hits zero, the resource enters `deferredFree`
- After the current frame's GPU work completes (`Device.Poll`), `deferredFree` is drained and actual `wgpu.Texture.Release()` etc. happen

This is where the existing polling-goroutine pattern slots in directly.

---

## Loaders

Loaders are a separate package (`pix/loaders`) that depends on both `Renderer` and `Scene`, owned by neither.

```go
func loaders.LoadGLTF(r *pix.Renderer, s *pix.Scene, path string) (*GLTFAsset, error)

type GLTFAsset struct {
    Roots      []pix.Node          // added under scene.root
    Geometries []pix.GeometryID    // for refcount / explicit release
    Materials  []pix.MaterialID
    Textures   []pix.TextureID
    Animations []AnimationClip
    Skins      []SkinID
}

func (a *GLTFAsset) Release()           // refcount-decrement everything
func (a *GLTFAsset) Instantiate(s *pix.Scene, parent pix.Node) []pix.Node
```

Each part of a glTF file lands in its rightful owner:

| glTF concept   | Lands in              | API                                |
|----------------|-----------------------|------------------------------------|
| Buffers        | Renderer.geometries   | `renderer.NewGeometry(...)`        |
| Images         | Renderer.textures     | `renderer.NewTexture2D(...)`       |
| Samplers       | Renderer.samplers     | `renderer.NewSampler(...)`         |
| Materials      | Renderer.materials    | `renderer.NewStandardMaterial(...)`|
| Nodes          | Scene                 | `scene.NewMesh(...)` etc.          |
| Skins          | Scene.skeletons       | dedicated skeleton table           |
| Animations     | Animation subsystem   | `AnimationClip`                    |

### Why a separate package

- Renderer doesn't know about scenes — it operates on resource IDs and `DrawCommand` lists
- Multiple loaders (glTF, OBJ, USD, custom) shouldn't all be `pix` core dependencies
- glTF brings JSON, base64, optional draco/KTX2 decode — keep that opt-in

### Patterns to support

**Load vs. instantiate split.** `LoadGLTF` does expensive parsing + GPU upload once. `asset.Instantiate(scene, parent)` cheaply adds another copy of just the nodes, referencing existing geometries/materials. Makes the cost model explicit.

**Vertex layout repack.** glTF buffer views have arbitrary attribute layouts; the renderer has a specific layout shaders expect. Loader repacks vertex data on CPU at load time. Shader permutation for per-asset layouts is a later optimization.

**Material extension policy.** Decide which `KHR_*` extensions are supported upfront. Fall back to base PBR with a warning on the rest. Don't load materials the renderer can't draw.

**Async loading.** Parse + decode on a worker goroutine. Post GPU-upload commands back to the device-owning goroutine via channel. Reuses the same Dawn-threading-discipline pattern already established.

---

## Boundary Summary

```
┌──────────────────────────────────────────────────────────────┐
│ pix/loaders   (depends on Renderer + Scene, owned by neither)│
│   LoadGLTF, LoadOBJ, ...                                     │
└────────────┬───────────────────────────┬─────────────────────┘
             │                           │
             ▼                           ▼
┌──────────────────────────┐  ┌──────────────────────────────┐
│ Scene                    │  │ Renderer                     │
│  - nodes (shared arrays) │  │  - geometries (GPU buffers)  │
│  - kind payload tables   │  │  - materials (pipeline+BG)   │
│  - hierarchy ops         │──┤  - textures, samplers        │
│  - transform/vis passes  │  │  - pipeline & sampler caches │
│  - allocator + handles   │  │  - flushMaterials, draw      │
└──────────────────────────┘  └──────────────────────────────┘
   references resources by ID ──►  owns GPU resources
```

The scene contributes **what's where**; the renderer contributes **how to draw it**. Neither needs to know the other's internal layout — they communicate through small ID handles and a per-frame `DrawCommand` list.

---

# pix — Resource Refcounting Supplement

Supplements `spec.md`. Covers the decoupled `Ref` design for GPU resource handles (geometries, materials, textures, samplers), refcount semantics, lifetime rules, and the generation vs. version distinction.

---

## Goals

- Decouple `Scene` from `Renderer`. Scene code stores and forwards refs without knowing what backs them.
- Make resource lifetime explicit and safe: no use-after-free, no silent slot-reuse bugs (ABA), no leaks of unreferenced resources.
- Keep the hot path cheap: refs sit in payload tables untouched during rendering; no per-frame refcount traffic.

---

## The `Ref` Type

```go
type Disposer interface {
    Dispose(id uint32)
    Generation(id uint32) uint32
}

type Ref[T any] struct {
    id       uint32
    gen      uint32       // captured at creation; compared against current to detect staleness
    refCount *int32       // shared across all clones of this ref
    owner    Disposer     // interface, not closure — avoids per-resource allocation
}
```

Size: 24 bytes (8 + pointer + interface). Stored by value; cloned by `Retain()`.

### Methods

```go
func (r Ref[T]) Retain() Ref {
    atomic.AddInt32(r.refCount, 1)
    return r
}

func (r Ref[T]) Release() {
    if atomic.AddInt32(r.refCount, -1) == 0 {
        r.owner.Dispose(r.id)
    }
}

func (r Ref[T]) Valid() bool {
    return r.owner != nil && r.owner.Generation(r.id) == r.gen
}

func (r Ref[T]) ID() uint32 { return r.id }
```

### Typed wrappers

The base `Ref` is unsafe to mix across resource kinds. Wrap it:

```go

```

Constructors return the typed wrapper; type system prevents passing a `Ref[Texture]` where a `Ref[Geometry]` is expected.

---

## Why `Ref` (and not `id + *Renderer`)

- **Scene has no renderer dependency.** `Scene.NewMesh(geo GeometryRef, mat MaterialRef)` knows nothing about who created the refs. Mock/test/swap the renderer freely.
- **Cross-package resources work uniformly.** Anything implementing `Disposer` can hand out refs — procedurally generated assets, CPU-side caches, network-backed textures.
- **Refcount lives with the ref, not the storer.** Storer doesn't need to know which table to retain/release in.

Tradeoff: 24 bytes per ref vs. 8 bytes for `(id, gen)` pair. With ~2 refs per mesh entry (geometry + material), that's 48 bytes/mesh extra. Acceptable; the inner render loop iterates `meshes[]` payload entries but doesn't touch refcounts.

---

## Lifetime Rules

### Creation: refcount starts at 1

```go
tex := renderer.NewTexture2D(...)   // refcount = 1; caller owns one reference
mat := renderer.NewStandardMaterial(...)
```

The `1` represents "the caller holds this ref." Caller must `Release()` exactly once, or pass the ref somewhere that will.

### Storage: retain before storing, release on overwrite

```go
func (m Mesh) SetMaterial(newMat MaterialRef) {
    md := &m.scene.meshes[m.scene.payload[m.slot()]]
    newCopy := newMat.Retain()         // retain new first
    md.Material.Release()               // then release old
    md.Material = newCopy
}
```

**Retain-before-release order matters.** If `newMat` and the existing stored material are the same logical resource, releasing first could briefly drop refcount to zero and trigger destruction before the retain restores it.

### Destruction: storer releases what it stored

```go
func (s *Scene) destroyMeshPayload(payloadIdx uint32) {
    md := s.meshes[payloadIdx]
    md.Geometry.Release()
    md.Material.Release()
    // ... swap-remove, patch OwnerNode ...
}
```

When a `Mesh` node is destroyed, its payload entry releases the geometry and material refs it stored at construction.

### Typical end-to-end

```go
tex := renderer.NewTexture2D(...)       // refcount(tex) = 1
mat := renderer.NewStandardMaterial(...) // refcount(mat) = 1
mat.SetAlbedo(tex)                       // refcount(tex) = 2

mesh := scene.NewMesh(geo, mat)          // refcount(geo) = 2, refcount(mat) = 2

// caller no longer needs raw refs:
tex.Release()                            // refcount(tex) = 1 (held by mat)
mat.Release()                            // refcount(mat) = 1 (held by mesh)
geo.Release()                            // refcount(geo) = 1 (held by mesh)

mesh.Destroy()                           // releases geo and mat refs
                                         // refcount(geo) = 0 → Dispose(geo.id)
                                         // refcount(mat) = 0 → Dispose(mat.id)
                                         // mat's Dispose releases its tex ref
                                         // refcount(tex) = 0 → Dispose(tex.id)
```

Equally valid: skip the explicit `Release()` calls and let `renderer.Destroy()` drop everything at shutdown. Leaks bounded by renderer lifetime.

---

## NewMesh Signature

```go
func (s *Scene) NewMesh(geo GeometryRef, mat MaterialRef) Mesh {
    id := s.allocNode(KindMesh)
    s.meshes = append(s.meshes, MeshData{
        Geometry:   geo.Retain(),       // refcount++
        Material:   mat.Retain(),       // refcount++
        OwnerNode:  id.index,
        BoundsLocal: extractBounds(geo),
    })
    s.payload[id.index] = uint32(len(s.meshes) - 1)
    return Mesh{Node{scene: s, id: id}}
}
```

`MeshData` stores `GeometryRef` and `MaterialRef` by value. The `Retain()` calls bump refcount; the original refs passed in remain owned by the caller (who must release them separately or let them be released via assignment to other storers).

---

## Generation vs. Version: Two Different Counters

The resource table tracks two independent integers per resource. They answer different questions.

### `gen` — slot recycling detector

Bumped **only on destroy**, when the slot is freed and may be reassigned.

Purpose: detect stale `Ref` values whose underlying resource has been freed and possibly replaced.

```go
geo := renderer.NewBoxGeometry(...)     // (id=42, gen=1)
geo.Release()                            // slot 42 freed; geometries[42].gen → 2
                                         // any held (id=42, gen=1) ref fails Valid()

newGeo := renderer.NewSphereGeometry(...) // reuses slot 42: (id=42, gen=2)
                                          // old refs still detect staleness via gen mismatch
```

Without `gen`, slot reuse silently aliases old refs onto the new resource (the ABA problem).

### `version` — content mutation tracker (optional)

Bumped on `UpdateGeometry`, `SetVertices`, animated stream updates, etc. — anywhere the resource's *contents* change while its *identity* stays the same.

Purpose: invalidate dependent caches (bind groups, pipeline state, derived buffers) without invalidating refs.

```go
geo := renderer.NewBoxGeometry(...)     // (gen=1, version=1), refcount=1
g2  := geo.Retain()                      // refcount=2; both refs see same resource

renderer.UpdateGeometry(geo, newVerts)   // version → 2; gen unchanged
                                         // geo and g2 both still Valid()
                                         // material pipeline cache sees version bump,
                                         // rebuilds bind group on next flushMaterials()
```

**Mutation does not invalidate refs.** Every mesh sharing the geometry sees the updated data on the next draw — that's the whole point of shared-by-handle resources. Animation, morph targets, dynamic vertex streams all work this way.

### Summary

| Operation                       | `gen` | `version` | Refs invalidated? |
|---------------------------------|-------|-----------|-------------------|
| Create resource                 | =1    | =1        | n/a               |
| Retain / Clone                  | —     | —         | no                |
| Update contents (vertex data, uniforms) | —     | ++        | **no**            |
| Last Release → destroy          | ++    | —         | **yes** (all)     |
| Slot reused by new resource     | (already bumped) | reset | (already invalid) |

---

## Deferred Destruction

`owner.Dispose(id)` does **not** immediately call `wgpu.Texture.Release()` etc. The GPU may still be reading from the resource in flight.

```go
func (r *Renderer) Dispose(id uint32) {
    r.deferredFree = append(r.deferredFree, deferredResource{
        kind:     /* texture/buffer/etc. */,
        id:       id,
        frame:    r.currentFrame,
    })
    // gen bumped here; slot marked free in the table
    r.textures[id].gen++
    r.pushFreeSlot(id)
}
```

The drain happens after the in-flight frame completes (via `Device.Poll`):

```go
func (r *Renderer) drainDeferredFree() {
    safe := r.currentFrame - framesInFlight
    for i := 0; i < len(r.deferredFree); {
        d := r.deferredFree[i]
        if d.frame <= safe {
            r.actuallyFree(d)              // wgpu.Texture.Release(), etc.
            r.deferredFree[i] = r.deferredFree[len(r.deferredFree)-1]
            r.deferredFree = r.deferredFree[:len(r.deferredFree)-1]
        } else {
            i++
        }
    }
}
```

Key point: **gen bumps and slot freeing happen at `Dispose` time**, not at drain time. From the ref-validation perspective, the resource is dead the moment refcount hits zero. Only the GPU-side cleanup is deferred.

---

## Concurrency Rules

`Retain` and `Release` are atomic and safe to call from any goroutine.

**Constraint**: never `Retain()` a ref unless you already hold a live reference to it. Specifically, do not load a ref out of shared storage and clone it without synchronization that guarantees no concurrent `Release()` of the last reference.

This is the standard rule for any refcounted system. The pattern that breaks it:

```go
// THREAD A                              // THREAD B
ref := sharedSlot.Load()                 ref := sharedSlot.Load()
                                         ref.Release()  // refcount 1 → 0, Dispose runs
ref.Retain()  // BUG: incrementing freed refcount
```

Fix: protect the load+retain pair under a mutex, or use `sync/atomic.Pointer` semantics where the slot itself participates in the protocol.

In practice, scene mutation is single-threaded (per the main spec's deferred question), so this is mostly a non-issue. The atomics on `refCount` exist for the renderer's own internal threading (deferred-free drain, async loaders) and for the case where game code releases refs from worker goroutines.

---

## Validation Discipline

`Valid()` is a debug/safety check, not a performance path. Don't gate every render-loop access on it. The intended uses:

- **Debug builds**: assert on `Valid()` in setters and renderer entry points.
- **External APIs**: validate refs at the boundary (e.g., `mesh.SetMaterial`) so user errors surface immediately.
- **Internal hot loops**: skip the check; lifetime invariants guarantee validity by construction (the payload table owns retains for everything it references).

---

## Implementation Checklist

- [ ] `Ref` struct with `id`, `gen`, `*refCount`, `Disposer`
- [ ] Typed wrappers: `GeometryRef`, `MaterialRef`, `TextureRef`, `SamplerRef`
- [ ] `Retain` / `Release` / `Valid` / `ID`
- [ ] `Renderer` implements `Disposer` for each resource kind (or one impl that switches on kind)
- [ ] Resource tables carry `gen uint32` and `version uint32` per slot
- [ ] `New*` constructors initialize `refCount = 1`
- [ ] `Dispose` bumps `gen`, frees slot, appends to `deferredFree`
- [ ] `drainDeferredFree` runs after `Device.Poll` confirms frame completion
- [ ] `Scene.NewMesh` (and similar) retain stored refs; `Destroy` paths release them
- [ ] Setters that overwrite stored refs retain-before-release

## Open / Deferred Questions

- **Static subtree handling** — separate `staticWorld[]` block or just `flagStatic` skip? Profile first.
- **Animation system layout** — separate `pix/anim` package? How tightly coupled to skinned mesh skeleton table?
- **Compute pass integration** — particle systems, GPU skinning, mip generation share infra; needs its own pass abstraction.
- **Multi-camera / multi-viewport** — `Render(scene, camera)` is the obvious signature, but shadow passes and offscreen render-to-texture want a more general "view" concept.
- **Threading model for `Scene` mutation** — is mutation single-threaded, or do we want `sync.RWMutex` around hierarchy ops? Default to single-threaded; revisit if profiling demands.