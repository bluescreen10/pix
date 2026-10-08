package scenes

import (
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
)

// Extract publishes this scene's current rendering description into p. It is the whole
// of what a renderer is told; nothing downstream of it touches a Node, a payload table,
// or the Scene itself.
//
// The packet BORROWS the scene's cached tables — it does not copy the world. The tables
// are rebuilt only when the scene changes structurally, so extracting an unchanged
// scene costs a handful of slice headers no matter how large it is. A consumer that
// already applied the revision it is handed can skip the tables entirely.
//
// Until the frame is submitted, neither the scene nor any resource it references may be
// mutated. That is an API contract, not something enforced here.
func (s *Scene) Extract(p *FramePacket) {
	// Settle first: world transforms, skinning and the clock all feed the tables below,
	// so extraction owns that rather than leaving a consumer to remember the order.
	s.Sync()
	if s.packetDirty {
		s.rebuildPacketTables()
		revision := s.packet.Meshes.Revision + 1
		s.packet.Meshes.Revision = revision
		s.packet.LODs.Revision = revision
		s.packetDirty = false
	}

	// Transforms carry no revision yet: they are rewritten and re-uploaded every frame,
	// so a consumer has nothing to skip. Ranged invalidation is a later phase.
	s.packet.Transforms.Data = s.world
	s.packet.InstanceTransforms.Data = s.instanceTransforms

	// One revision covers all three object tables. They are rebuilt by the same walk
	// and cannot disagree, so splitting them would mean three counters that always move
	// together — the revisions worth separating are the ones with different causes.
	s.extractViews()
	s.extractLights()
	s.extractSkins()
	s.extractParticles()

	s.packet.Frame++
	*p = s.packet
	// Transform dirtiness is edge-triggered: this packet carries it, and later Sync
	// calls accumulate changes until the next extraction.
	s.packet.TransformsDirty = false
}

// rebuildPacketTables rewalks the payload lists into the packet tables. Called only
// when the scene changed structurally — a mesh added or removed, a material swapped, a
// node attached or detached.
//
// Only nodes attached to the scene are described. Creating a mesh does NOT attach it;
// scene.Add (or parenting it under something attached) does. An unattached node is
// never visited by updateTransforms, so its world matrix would still be the identity it
// was born with: describing it anyway would silently place it at the origin, ignoring
// every transform set on it. Omitting it makes the mistake obvious — the mesh is simply
// missing until it is added.
func (s *Scene) rebuildPacketTables() {
	s.packet.Meshes.Data = s.packet.Meshes.Data[:0]
	s.packet.LODs.Data = s.packet.LODs.Data[:0]

	for i := range s.meshes {
		md := &s.meshes[i]
		if s.flags[md.ownerNode]&flagAttached == 0 {
			continue
		}
		s.addMesh(MeshPacket{
			ID:            s.objectID(md.ownerNode),
			Transforms:    IndexRange{First: md.ownerNode, Count: 1},
			Bounds:        md.bounds,
			Flags:         s.renderFlags(md.ownerNode),
			LODHysteresis: md.hysteresis,
		}, md.lods)
	}

	for _, sm := range s.skinnedMeshes.Entries() {
		// Both must be attached: the mesh node puts it in the scene, and the skeleton
		// root supplies the transform its drawable is rendered with.
		root := s.skeletons.Value(sm.skeleton).ownerNode
		if s.flags[sm.ownerNode]&flagAttached == 0 || s.flags[root]&flagAttached == 0 {
			continue
		}
		s.packet.Meshes.Data = append(s.packet.Meshes.Data, MeshPacket{
			ID:         s.objectID(sm.ownerNode),
			Transforms: IndexRange{First: root, Count: 1},
			Geometry:   sm.outputGeo.ID(),
			Material:   sm.material.ID(),
			Bounds:     sm.bounds,
			Flags:      s.renderFlags(sm.ownerNode),
		})
	}

	// Instance transforms are addressed as if they sit right after every node's world
	// matrix — see FramePacket.InstanceTransforms, and drawList.sync, which uploads them
	// into one contiguous buffer on that same assumption.
	instanceBase := uint32(len(s.world))
	for i := range s.instancedMeshes {
		im := &s.instancedMeshes[i]
		if s.flags[im.ownerNode]&flagAttached == 0 {
			continue
		}
		s.addMesh(MeshPacket{
			ID:            s.objectID(im.ownerNode),
			Transforms:    IndexRange{First: instanceBase + im.transformBase, Count: im.count},
			Bounds:        im.bounds,
			Flags:         s.renderFlags(im.ownerNode),
			LODHysteresis: im.hysteresis,
		}, im.lods)
	}
}

// addMesh completes mp from its LOD chain and records it. Level 0 goes on the mesh
// itself and the coarser levels into the shared LOD table, which is why this is worth
// a helper: Mesh and InstancedMesh differ in how they are transformed and in nothing
// else, so only the part above varies.
func (s *Scene) addMesh(mp MeshPacket, lods []lodLevel) {
	mp.Geometry = lods[0].geometry.ID()
	mp.Material = lods[0].material.ID()
	mp.LODRange = IndexRange{First: uint32(len(s.packet.LODs.Data))}
	for _, l := range lods[1:] {
		s.packet.LODs.Data = append(s.packet.LODs.Data, LODLevel{
			Geometry:    l.geometry.ID(),
			Material:    l.material.ID(),
			MinDistance: l.minDistance,
		})
		mp.LODRange.Count++
	}
	s.packet.Meshes.Data = append(s.packet.Meshes.Data, mp)
}

// objectID is a node's identity as a packet object: stable while the node lives, and
// distinguishable from whatever later reuses its slot.
func (s *Scene) objectID(node uint32) ObjectID {
	return ObjectID{Index: node, Gen: s.generation[node]}
}

// renderFlags translates a node's scene flags into the per-drawable bits the renderer
// consumes. The scene's flag set is larger and mostly its own business — liveness,
// dirtiness, attachment — so only these two cross the boundary.
func (s *Scene) renderFlags(node uint32) RenderFlags {
	var f RenderFlags
	if s.flags[node]&flagCastShadow != 0 {
		f |= RenderCastsShadow
	}
	if s.flags[node]&flagReceiveShadow != 0 {
		f |= RenderReceivesShadow
	}
	return f
}

// extractViews refills the published view table with every attached, visible camera,
// in creation order. Cameras are polled like lights: there are a handful of them, and a
// camera is the one node expected to move every frame.
//
// A camera's view comes from its node's world matrix, so a camera parented under
// something moves with it.
func (s *Scene) extractViews() {
	out := s.packet.Views[:0]
	for i := range s.cameras {
		c := &s.cameras[i]
		if s.flags[c.ownerNode]&flagAttached == 0 || s.flags[c.ownerNode]&flagVisible == 0 {
			continue
		}
		world := s.world[c.ownerNode]
		out = append(out, ViewPacket{
			ID:         c.id,
			View:       world.Inv(),
			Projection: c.projection(),
			Position:   glm.Vec3f{world[12], world[13], world[14]},
			Exposure:   c.exposure,
		})
	}
	s.packet.Views = out
}

// newLightID mints the next stable light identity. Never reused: a renderer keys shadow
// resources on it, and a recycled id would hand a new light the dead one's depth map.
func (s *Scene) newLightID() LightID {
	s.nextLightID++
	return s.nextLightID
}

// extractLights refills the published light table. Unlike meshes, lights are polled
// rather than tracked: their fields are exported and mutable, so there is no setter to
// hang dirtiness off, and there are a handful of them against thousands of objects.
// This is O(lights) per frame by design — see docs/frame-packet.md.
func (s *Scene) extractLights() {
	out := s.packet.Lights.Data[:0]
	for _, l := range s.dirLights {
		lp := LightPacket{
			ID: l.id, Kind: LightDirectional, Direction: l.Direction,
			Color: l.Color, Intensity: l.Intensity,
		}
		if l.mask.IsEnabled() {
			lp.MaskTexture = l.mask.Texture.Index()
			lp.MaskSize = l.mask.Size
			lp.MaskOffset = l.mask.Offset
		}
		if l.shadow != nil {
			applyShadowSettings(&lp, &l.shadow.LightShadow)
			lp.ShadowMethod = l.shadow.method
			lp.shadowCascades = l.shadow.cascades
			lp.shadowDistance = l.shadow.distance
			lp.ShadowSplits = l.shadow.splits
		}
		out = append(out, lp)
	}
	for _, l := range s.pointLights {
		lp := LightPacket{
			ID: l.id, Kind: LightPoint, Position: l.Position,
			Color: l.Color, Intensity: l.Intensity, Range: l.Range,
		}
		applyShadowSettings(&lp, l.shadow)
		out = append(out, lp)
	}
	for _, l := range s.spotLights {
		lp := LightPacket{
			ID: l.id, Kind: LightSpot, Position: l.Position, Direction: l.Direction,
			Color: l.Color, Intensity: l.Intensity, Range: l.Range,
			Angle: l.Angle, Penumbra: l.Penumbra,
		}
		applyShadowSettings(&lp, l.shadow)
		out = append(out, lp)
	}
	s.packet.Lights.Data = out
	ambient := colors.RGB32F{s.ambient[0] * s.ambientIntensity, s.ambient[1] * s.ambientIntensity, s.ambient[2] * s.ambientIntensity}
	s.packet.Environment = EnvironmentPacket{Ambient: ambient, Fog: StateOf(s.fog), Map: environmentState(s.environment)}
}

// applyShadowSettings copies a light's shadow settings into its packet. A nil shadow
// means the light does not cast, which is the only thing the renderer needs to know to
// skip allocating anything for it.
func applyShadowSettings(lp *LightPacket, sh *LightShadow) {
	if sh == nil {
		return
	}
	lp.CastsShadow = true
	lp.ShadowSize = sh.size
	lp.ShadowBias = sh.bias
}

// extractSkins refills the published skin table. Like lights, skinned meshes are few
// and polled rather than tracked; unlike lights, the palettes they index were already
// recomputed by Scene.Sync, so this only records ranges into that table.
func (s *Scene) extractSkins() {
	out := s.packet.Skins.Data[:0]
	for _, sm := range s.skinnedMeshes.Entries() {
		sk := s.skeletons.Value(sm.skeleton)
		out = append(out, SkinPacket{
			Source:      sm.srcGeometry.ID(),
			Output:      sm.outputGeo.ID(),
			Joints:      IndexRange{First: sk.jointBase, Count: uint32(len(sk.bones))},
			VertexCount: sm.vertCount,
		})
	}
	s.packet.Skins.Data = out
}

// newParticleID mints the next stable particle-system identity.
func (s *Scene) newParticleID() ParticleID {
	s.nextParticleID++
	return s.nextParticleID
}

// extractParticles publishes each attached system's description and the simulation
// step it currently has queued. Like every other table, it borrows: the pending births
// stay on the container until Rendered confirms that the step was submitted.
//
// That split is what makes a simulation step exactly-once without any acknowledgement
// protocol. Extracting without rendering costs nothing — the same births are published
// again next time, and alive is not advanced, so capacity accounting stays right.
// Rendering is what consumes them, and Render owns both halves, so the two cannot drift
// apart.
func (s *Scene) extractParticles() {
	out := s.packet.Particles.Data[:0]
	newborns := s.packet.Newborns.Data[:0]
	for i := range s.particleContainers {
		d := &s.particleContainers[i]
		if s.flags[d.ownerNode]&flagAttached == 0 {
			continue
		}
		pp := ParticlePacket{
			ID:        d.id,
			Transform: d.ownerNode,
			Geometry:  d.geometry.ID(),
			Material:  d.material.ID(),
			Capacity:  d.capacity,
			Update:    d.update,
			Sort:      d.sort,
			DT:        d.dt,
			Epoch:     d.epoch,
			Newborns:  IndexRange{First: uint32(len(newborns)), Count: uint32(len(d.pending))},
		}
		newborns = append(newborns, d.pending...)
		out = append(out, pp)
	}
	s.packet.Particles.Data = out
	s.packet.Newborns.Data = newborns
}

// Rendered implements Producer: it consumes the simulation step the packet just
// rendered carried out —
// the births it staged are now the GPU's, and the accumulated dt has been simulated.
// Called by Render after the frame is submitted, never by a consumer — a renderer
// reaching into a producer to clear its queue is the coupling this boundary exists to
// remove.
//
// alive is advanced by the births handed over and never read back down as particles die
// on the GPU: a deliberate CPU-side over-estimate that trades capacity headroom for
// needing no readback.
func (s *Scene) Rendered() {
	for i := range s.particleContainers {
		d := &s.particleContainers[i]
		if s.flags[d.ownerNode]&flagAttached == 0 {
			continue
		}
		d.alive += uint32(len(d.pending))
		d.pending = d.pending[:0]
		d.dt, d.dtStaged = 0, false
	}
}
