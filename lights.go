// The GPU light table: the buffer the lit shaders read, packed each frame from a
// packet's light values, this renderer's own shadow resources and the main view's light
// clusters. The
// light objects a caller configures live in the scenes package; nothing here is
// visible to a producer.
package pix

import (
	"unsafe"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/gamekit/utils"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
	"github.com/chewxy/math32"
)

// MaxDirLights is how many directional lights a scene is lit by; the rest are dropped.
// Point and spot lights have no limit: each pixel is shaded only by those its light
// cluster lists (see light_clusters.go).
const MaxDirLights = 4

// noShadowMap is the shadowMap sentinel: a directional light that casts no shadow
// (or whose map isn't allocated) stores this, and the lit shaders skip sampling.
const noShadowMap uint32 = 0xFFFFFFFF

// noShadow is the sentinel a point or spot light without a shadow stores in place of
// the index of its shadow record.
const noShadow uint32 = 0xFFFFFFFF

// noLightMask is the mask sentinel: a directional light with no mask (see
// scenes.LightMask), which lets all of its light through.
const noLightMask uint32 = 0xFFFFFFFF

type gpuDirLight struct {
	dir   [4]float32 // xyz = travel direction; w unused
	color [4]float32 // rgb; w = intensity
	// shadowVP is world → light clip, the same matrix the depth pass rendered with, one
	// per cascade. Every algorithm but ShadowCascaded fills only the first and leaves
	// cascades at 1, which collapses the lookup's cascade selection to index 0.
	shadowVP [MaxShadowCascades]glm.Mat4f
	// shadowSplit[i] is the view distance cascade i covers out to; shadowBias[i] is the
	// constant part of that cascade's depth bias, in its own normalized depth units.
	shadowSplit [MaxShadowCascades]float32
	shadowBias  [MaxShadowCascades]float32
	// shadowTexel[i] is how much world space one of that cascade's texels covers, and
	// shadowDepthScale[i] converts a world depth offset into its normalized depth. The
	// lit shader needs both to size the offsets that depend on the surface normal, which
	// the fit cannot know. Zero disables those, which is what the warps want.
	shadowTexel      [MaxShadowCascades]float32
	shadowDepthScale [MaxShadowCascades]float32
	shadowMap        uint32 // bindless heap index of the depth map, or noShadowMap
	cascades         uint32 // how many of shadowVP/shadowSplit/shadowBias are filled
	// mapSide is one cascade square's resolution in texels, which a wide kernel needs in
	// order to step by texels; filter is the renderer's ShadowFilter.
	mapSide uint32
	filter  ShadowFilter
	// maskU and maskV take a world position to the light's mask: its texture coordinates
	// are (dot(maskU.xyz, p) + maskU.w, dot(maskV.xyz, p) + maskV.w) — see
	// maskProjection. mask is the mask's heap index, or noLightMask, and maskSampler
	// the sampler it is read with.
	maskU, maskV glm.Vec4f
	mask         uint32
	maskSampler  uint32
}

// maskProjection returns the two planes that take a world position to a directional
// light's mask coordinates, along the axes scenes.LightMaskAxes gives: one repeat spans
// size world units, and the mask is moved through the world by offset.
func maskProjection(dir glm.Vec3f, size float32, offset glm.Vec3f) (u, v glm.Vec4f) {
	uAxis, vAxis := scenes.LightMaskAxes(dir)
	scale := 1 / size
	u = uAxis.Scale(scale).Vec4(-uAxis.Dot(offset) * scale)
	v = vAxis.Scale(scale).Vec4(-vAxis.Dot(offset) * scale)
	return u, v
}

// gpuPointLight is one point light as the shading loops read it: small, because a
// pixel reads every light its cluster lists. The few that cast shadows point at a
// gpuPointShadow.
type gpuPointLight struct {
	pos    glm.Vec4f      // xyz world; w = range
	color  colors.RGBA32F // rgb; w = intensity
	shadow uint32         // index into the point shadows, or noShadow
	_      [3]uint32
}

type gpuPointShadow struct {
	shadowVP   [6]glm.Mat4f // per cube face: world → face clip
	shadowMap  [6]uint32    // per cube face: depth map heap index, or noShadowMap
	shadowBias float32      // depth-compare bias, in the face cameras' normalized depth units
	_          uint32
}

// gpuSpotLight is one spot light as the shading loops read it; see gpuPointLight.
type gpuSpotLight struct {
	pos      glm.Vec4f      // xyz world; w = range
	dir      glm.Vec4f      // xyz cone axis (travel); w = cosOuter (outer cutoff)
	color    colors.RGBA32F // rgb; w = intensity
	cosInner float32        // inner cutoff cos (smooth edge between inner and outer)
	shadow   uint32         // index into the spot shadows, or noShadow
	_        [2]uint32
}

type gpuSpotShadow struct {
	shadowVP   glm.Mat4f // world → light clip (same matrix the depth pass rendered with)
	shadowMap  uint32    // bindless heap index of the depth map, or noShadowMap
	shadowBias float32   // depth-compare bias, in this camera's normalized depth units
	_          [2]uint32
}

// gpuLights is the head of the light table (scalar; matches LightBuf in lighting.glsl).
// The point and spot lights and their shadows follow it in the same buffer, and it
// carries their addresses.
type gpuLights struct {
	ambient colors.RGBA32F
	// fogColor is rgb + the fog mode in w; fogParams is (near, far, density, _).
	// Fog rides in the light table rather than in each root because both the forward
	// and the deferred lighting passes already carry the table, and neither push
	// constant has to grow.
	fogColor  colors.RGBA32F
	fogParams glm.Vec4f
	numDir    uint32
	numPoint  uint32
	numSpot   uint32
	pad0      uint32
	// The environment the scene is lit by — see environmentState and lighting.glsl;
	// envRadiance is noEnvironment when there is none.
	envRadiance  uint32
	envSampler   uint32
	envMips      uint32
	envBRDF      uint32
	envIntensity float32
	envRotation  float32
	points       uint64
	spots        uint64
	pointShadows uint64
	spotShadows  uint64
	// clusters is the main view's light clusters: where its cells are and how a world
	// position finds its own (see clusterGrid).
	clusters          uint64
	clusterViewProj   glm.Mat4f
	clusterDepth      glm.Vec4f
	clusterSliceScale float32
	clusterSliceBias  float32
	dirs              [MaxDirLights]gpuDirLight
}

var (
	lightsSize      = uint64(unsafe.Sizeof(gpuLights{}))
	pointLightSize  = uint64(unsafe.Sizeof(gpuPointLight{}))
	pointShadowSize = uint64(unsafe.Sizeof(gpuPointShadow{}))
	spotLightSize   = uint64(unsafe.Sizeof(gpuSpotLight{}))
	spotShadowSize  = uint64(unsafe.Sizeof(gpuSpotShadow{}))
)

// Lights is the scene light table: ambient, directional lights and the cluster grid in
// a fixed-size head, followed by the point and spot lights and their shadows, all in
// one host buffer the lit shaders read through the draw root.
type Lights struct {
	backend gpu.Backend
	data    gpuLights

	points       []gpuPointLight
	pointShadows []gpuPointShadow
	spots        []gpuSpotLight
	spotShadows  []gpuSpotShadow

	buf gpu.Buffer
}

// NewLights creates the table. ambient defaults to a low neutral fill so an
// unlit-looking scene still shows geometry; call SetAmbient to change it.
//
// MemoryHost (not Device): the table is rewritten every frame — the cluster grid
// follows the camera, and a shadow-casting light's shadowVP is refit from the view —
// and staging that through the shared uploader would force a real GPU submit+wait
// every frame just for a few KB of light data; a direct host write (like
// worldBuf/drawableBuf already do for the same reason) costs a memcpy instead.
func NewLights(b gpu.Backend) *Lights {
	l := &Lights{backend: b}
	l.data.ambient = [4]float32{0.08, 0.08, 0.08, 0}
	return l
}

// rebuild derives the GPU light table from a packet's lights and environment, plus the
// renderer's own shadow resources for those lights and the main view's light clusters.
// Called every frame: the values are mutable, a casting light's fitted shadow camera
// moves with the view, and so does the cluster grid. Directional lights past
// MaxDirLights are dropped.
//
// filter is the kernel directional lookups use, which the shader reads per light.
//
// maskSampler is the sampler light masks are read with: linear and repeating, so a
// mask tiles the world without seams.
//
// environment is the scene's environment light, with no resources when it has none;
// environmentSampler and environmentBRDF are the renderer's, which every scene's
// environment light is read with.
//
// fogVolume, read with fogSampler, is a volumetric fog's volume, fogSize its
// resolution, and screenWidth x screenHeight the screen it lies over: the table
// carries them in place of the colour and distances the other fog models put there.
// They are read only when the scene's fog is volumetric.
//
// clusters is the main view's cluster grid, which the table carries for the lit
// shaders to find a position's lights with.
//
// shadows reports whether shadow maps may be advertised to the shader at all — the
// renderer's global toggle. A light whose map is still allocated but no longer being
// re-rendered must publish noShadowMap, or the shader keeps sampling a frozen map and
// the shadow stays on screen after it was turned off.
func (l *Lights) rebuild(env scenes.EnvironmentPacket, lights []scenes.LightPacket, res map[scenes.LightID]*shadowResource, shadows bool, filter ShadowFilter, maskSampler uint32, environment *environmentState, environmentSampler gpu.Sampler, environmentBRDF gpu.Texture,
	fogVolume gpu.Texture, fogSampler gpu.Sampler, fogSize VolumetricFogSettings, screenWidth, screenHeight uint32, clusters clusterGrid) {
	l.points = l.points[:0]
	l.pointShadows = l.pointShadows[:0]
	l.spots = l.spots[:0]
	l.spotShadows = l.spotShadows[:0]

	var next gpuLights
	next.clusters = clusters.cells.Addr
	next.clusterViewProj = clusters.viewProj
	next.clusterDepth = clusters.depthPlane
	next.clusterSliceScale = clusters.sliceScale
	next.clusterSliceBias = clusters.sliceBias
	next.ambient = env.Ambient.RGBA(1)
	next.envRadiance = noEnvironment
	if environment.radiance.IsValid() {
		next.envRadiance = environment.radiance.Index
		next.envSampler = environmentSampler.Index
		next.envMips = environmentMips
		next.envBRDF = environmentBRDF.Index
		next.envIntensity, next.envRotation = env.Map.Intensity, env.Map.Rotation
	}
	fs := env.Fog
	next.fogColor = fs.Color.RGBA(float32(fs.Mode))
	next.fogParams = glm.Vec4f{fs.Near, fs.Far, fs.Density, 0}
	if fs.Mode == scenes.FogVolumetric {
		// Volumetric fog is looked up rather than computed (see applyFog): the colour
		// carries the screen the volume lies over and the fog's reach, the parameters
		// where the volume is. Indices are small integers, which a float holds exactly.
		next.fogColor = colors.RGBA32F{1 / float32(screenWidth), 1 / float32(screenHeight), fs.Reach, float32(fs.Mode)}
		next.fogParams = glm.Vec4f{float32(fogVolume.Index), float32(fogSampler.Index), float32(fogSize.Depth), 0}
	}

	//FIXME: remove enclosure, use a helper method
	//
	// shadowOf is the light's resource, but only when shadows may be advertised at all.
	shadowOf := func(lp scenes.LightPacket) *shadowResource {
		if !shadows || !lp.CastsShadow {
			return nil
		}
		return res[lp.ID]
	}

	for _, lp := range lights {
		switch lp.Kind {
		case scenes.LightDirectional:
			if next.numDir >= MaxDirLights {
				continue
			}
			dir := lp.Direction.Normalize()
			gl := gpuDirLight{
				dir:       dir.Vec4(0),
				color:     lp.Color.RGBA(lp.Intensity),
				shadowMap: noShadowMap,
				mask:      noLightMask,
			}
			if lp.MaskSize > 0 {
				gl.mask, gl.maskSampler = lp.MaskTexture, maskSampler
				gl.maskU, gl.maskV = maskProjection(dir, lp.MaskSize, lp.MaskOffset)
			}
			// A casting light with an allocated map contributes its view-projection (the
			// un-flipped matrix the depth pass used) and heap index for shader sampling.
			if s := shadowOf(lp); s != nil && s.m.IsValid() {
				gl.shadowMap = s.m.Index()
				gl.mapSide = max(s.size(), 1)
				gl.filter = filter
				if n := len(s.cascades); n > 0 {
					gl.cascades = uint32(n)
					for i, c := range s.cascades {
						gl.shadowVP[i] = c.cam.ViewProjection()
						gl.shadowSplit[i] = c.far
						gl.shadowBias[i] = c.fit.bias
						gl.shadowTexel[i] = c.fit.texel
						gl.shadowDepthScale[i] = c.fit.depthScale
					}
				} else {
					gl.cascades = 1
					gl.shadowVP[0] = s.cam.ViewProjection()
					gl.shadowBias[0] = s.ndcBias
					gl.shadowTexel[0] = s.fit.texel
					gl.shadowDepthScale[0] = s.fit.depthScale
				}
			}
			next.dirs[next.numDir] = gl
			next.numDir++

		case scenes.LightPoint:
			gp := gpuPointLight{
				pos:    lp.Position.Vec4(lp.Range),
				color:  lp.Color.RGBA(lp.Intensity),
				shadow: noShadow,
			}
			if s := shadowOf(lp); s != nil {
				shadow := gpuPointShadow{shadowBias: s.ndcBias}
				for f := range shadow.shadowMap {
					shadow.shadowMap[f] = noShadowMap
					if s.faces[f].m.IsValid() {
						shadow.shadowVP[f] = s.faces[f].cam.ViewProjection()
						shadow.shadowMap[f] = s.faces[f].m.Index()
					}
				}
				gp.shadow = uint32(len(l.pointShadows))
				l.pointShadows = append(l.pointShadows, shadow)
			}
			l.points = append(l.points, gp)

		case scenes.LightSpot:
			dir := lp.Direction.Normalize()
			gs := gpuSpotLight{
				pos:      lp.Position.Vec4(lp.Range),
				dir:      dir.Vec4(math32.Cos(lp.Angle)),
				color:    lp.Color.RGBA(lp.Intensity),
				cosInner: math32.Cos(lp.Angle * (1 - glm.Clamp(lp.Penumbra, 0, 1))),
				shadow:   noShadow,
			}
			if s := shadowOf(lp); s != nil && s.m.IsValid() {
				gs.shadow = uint32(len(l.spotShadows))
				l.spotShadows = append(l.spotShadows, gpuSpotShadow{
					shadowVP:   s.cam.ViewProjection(),
					shadowMap:  s.m.Index(),
					shadowBias: s.ndcBias,
				})
			}
			l.spots = append(l.spots, gs)
		}
	}
	next.numPoint = uint32(len(l.points))
	next.numSpot = uint32(len(l.spots))
	l.data = next
}

// Addr returns the table's device address.
func (l *Lights) Addr() uint64 {
	return l.buf.Addr
}

// Sync writes the table: its head, then the point and spot lights and their shadows,
// whose addresses the head carries. The buffer grows to fit, at least doubling, so a
// scene adding lights a few at a time does not reallocate every frame.
func (l *Lights) Sync() {
	pointsAt := lightsSize
	pointShadowsAt := pointsAt + uint64(len(l.points))*pointLightSize
	spotsAt := pointShadowsAt + uint64(len(l.pointShadows))*pointShadowSize
	spotShadowsAt := spotsAt + uint64(len(l.spots))*spotLightSize
	size := spotShadowsAt + uint64(len(l.spotShadows))*spotShadowSize
	if !l.buf.IsValid() || l.buf.Size < size {
		if l.buf.IsValid() {
			size = max(size, l.buf.Size*2)
			l.backend.Free(l.buf)
		}
		l.buf = l.backend.Alloc(size, gpu.MemoryHost, "Lights")
	}

	l.data.points = l.buf.Addr + pointsAt
	l.data.pointShadows = l.buf.Addr + pointShadowsAt
	l.data.spots = l.buf.Addr + spotsAt
	l.data.spotShadows = l.buf.Addr + spotShadowsAt
	l.buf.Write(utils.ToBytes(&l.data), 0)
	if len(l.points) > 0 {
		l.buf.Write(utils.ToBytesSlice(l.points), pointsAt)
	}
	if len(l.pointShadows) > 0 {
		l.buf.Write(utils.ToBytesSlice(l.pointShadows), pointShadowsAt)
	}
	if len(l.spots) > 0 {
		l.buf.Write(utils.ToBytesSlice(l.spots), spotsAt)
	}
	if len(l.spotShadows) > 0 {
		l.buf.Write(utils.ToBytesSlice(l.spotShadows), spotShadowsAt)
	}
}

// Destroy releases the table buffer.
func (l *Lights) Destroy() {
	if l.buf.IsValid() {
		l.backend.Free(l.buf)
		l.buf = gpu.Buffer{}
	}
}
