package scenes

import "github.com/bluescreen10/pix/materials"

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
	if s.drawableDirty {
		s.rebuildPacketTables()
		s.meshRevision++
		s.drawableDirty = false
	}

	p.Source = s.sourceID
	p.Frame++
	p.Time = s.elapsed

	// Transforms carry no revision yet: they are rewritten and re-uploaded every frame,
	// so a consumer has nothing to skip. Ranged invalidation is a later phase.
	p.Transforms.Data = s.world
	p.InstanceTransforms.Data = s.instanceTransforms
	p.TransformsDirty = s.transformsDirty
	s.transformsDirty = false

	// One revision covers all three object tables. They are rebuilt by the same walk
	// and cannot disagree, so splitting them would mean three counters that always move
	// together — the revisions worth separating are the ones with different causes.
	s.extractLights(p)
	s.extractSkins(p)
	s.extractParticles(p)

	p.Meshes = Table[MeshPacket]{Revision: s.meshRevision, Data: s.packetMeshes}
	p.LODs = Table[LODLevel]{Revision: s.meshRevision, Data: s.packetLODs}
	p.Materials = Table[materials.ID]{Revision: s.meshRevision, Data: s.packetMaterials}
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
	if s.matSlot == nil {
		s.matSlot = make(map[materials.ID]uint32)
	}
	clear(s.matSlot)
	s.packetMeshes = s.packetMeshes[:0]
	s.packetLODs = s.packetLODs[:0]
	s.packetMaterials = s.packetMaterials[:0]

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

	for _, sm := range s.skinnedMeshes.All() {
		// Both must be attached: the mesh node puts it in the scene, and the skeleton
		// root supplies the transform its drawable is rendered with.
		root := s.skeletons.Get(sm.skeleton).ownerNode
		if s.flags[sm.ownerNode]&flagAttached == 0 || s.flags[root]&flagAttached == 0 {
			continue
		}
		s.packetMeshes = append(s.packetMeshes, MeshPacket{
			ID:         s.objectID(sm.ownerNode),
			Transforms: IndexRange{First: root, Count: 1},
			Geometry:   sm.outputGeo.ID(),
			Material:   s.materialSlot(sm.material),
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
	mp.Material = s.materialSlot(lods[0].material)
	mp.LODRange = IndexRange{First: uint32(len(s.packetLODs))}
	for _, l := range lods[1:] {
		s.packetLODs = append(s.packetLODs, LODLevel{
			Geometry:    l.geometry.ID(),
			Material:    s.materialSlot(l.material),
			MinDistance: l.minDistance,
		})
		mp.LODRange.Count++
	}
	s.packetMeshes = append(s.packetMeshes, mp)
}

// materialSlot returns m's slot in the distinct material table, appending it the first
// time this rebuild sees it. The dedup map is scratch, cleared per rebuild and reused so
// the walk does not allocate one every time.
//
// The indirection is what keeps the material set small: a thousand-instance field with
// four LOD levels contributes a thousand draws and at most four material entries.
func (s *Scene) materialSlot(m materials.Material) uint32 {
	id := m.ID()
	slot, seen := s.matSlot[id]
	if !seen {
		slot = uint32(len(s.packetMaterials))
		s.packetMaterials = append(s.packetMaterials, id)
		s.matSlot[id] = slot
	}
	return slot
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
func (s *Scene) extractLights(p *FramePacket) {
	out := s.packetLights[:0]
	for _, l := range s.dirLights {
		lp := LightPacket{
			ID: l.id, Kind: LightDirectional, Direction: l.Direction,
			Color: l.Color, Intensity: l.Intensity,
		}
		applyShadowSettings(&lp, l.shadow)
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
	s.packetLights = out
	p.Lights.Data = out
	p.Environment = EnvironmentPacket{Ambient: s.ambient, Fog: StateOf(s.fog)}
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
func (s *Scene) extractSkins(p *FramePacket) {
	out := s.packetSkins[:0]
	for _, sm := range s.skinnedMeshes.All() {
		sk := s.skeletons.Get(sm.skeleton)
		out = append(out, SkinPacket{
			Source:      sm.srcGeometry.ID(),
			Output:      sm.outputGeo.ID(),
			Joints:      IndexRange{First: sk.jointBase, Count: uint32(len(sk.bones))},
			VertexCount: sm.vertCount,
		})
	}
	s.packetSkins = out
	p.Skins.Data = out
	p.Joints.Data = s.packetJoints
}

// newParticleID mints the next stable particle-system identity.
func (s *Scene) newParticleID() ParticleID {
	s.nextParticleID++
	return s.nextParticleID
}

// extractParticles publishes each attached system's description and the simulation
// step it currently has queued. Like every other table, it BORROWS: the pending births
// stay on the container until retireParticleStep says the step was actually rendered.
//
// That split is what makes a simulation step exactly-once without any acknowledgement
// protocol. Extracting without rendering costs nothing — the same births are published
// again next time, and alive is not advanced, so capacity accounting stays right.
// Rendering is what consumes them, and Render owns both halves, so the two cannot drift
// apart.
func (s *Scene) extractParticles(p *FramePacket) {
	out := s.packetParticles[:0]
	newborns := s.packetNewborns[:0]
	for i := range s.particleContainers {
		d := &s.particleContainers[i]
		if s.flags[d.ownerNode]&flagAttached == 0 {
			continue
		}
		pp := ParticlePacket{
			ID:        d.id,
			Transform: d.ownerNode,
			Geometry:  d.geometry.ID(),
			Material:  s.materialSlot(d.material),
			Capacity:  d.capacity,
			Update:    d.update,
			DT:        d.dt,
			Epoch:     d.epoch,
			Newborns:  IndexRange{First: uint32(len(newborns)), Count: uint32(len(d.pending))},
		}
		newborns = append(newborns, d.pending...)
		out = append(out, pp)
	}
	s.packetParticles, s.packetNewborns = out, newborns
	p.Particles.Data = out
	p.Newborns.Data = newborns
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
