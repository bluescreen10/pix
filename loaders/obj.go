package loaders

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/glm"
)

// OBJ loads Wavefront .obj files.
type OBJ struct {
	r *pix.Renderer
}

// NewOBJ creates an OBJ loader backed by the given renderer.
func NewOBJ(r *pix.Renderer) *OBJ { return &OBJ{r: r} }

// Load reads a .obj file from disk (also parses the .mtl sidecar if present).
func (o *OBJ) Load(path string) (*pix.Scene, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("obj: read %q: %w", path, err)
	}
	return o.parse(data, filepath.Dir(path))
}

// LoadText parses an OBJ document from memory; external textures are not resolved.
func (o *OBJ) LoadText(text string) (*pix.Scene, error) {
	return o.parse([]byte(text), "")
}

// ---- internal types ----

type objVertKey struct{ v, vt, vn int } // -1 = absent

type objGroup struct {
	matName string
	vertMap map[objVertKey]uint32
	pos     []glm.Vec3f
	nor     []glm.Vec3f
	uv      []glm.Vec2f
	idx     []uint32
}

type objMat struct {
	color   glm.Color3f
	texPath string
}

type objParser struct {
	r       *pix.Renderer
	baseDir string

	positions []glm.Vec3f
	normals   []glm.Vec3f
	uvs       []glm.Vec2f

	mats       map[string]*objMat
	groups     map[string]*objGroup
	groupOrder []string
	current    string
}

func (o *OBJ) parse(data []byte, baseDir string) (*pix.Scene, error) {
	p := &objParser{
		r:       o.r,
		baseDir: baseDir,
		mats:    map[string]*objMat{"": {color: glm.Color3f{1, 1, 1}}},
		groups:  map[string]*objGroup{},
		current: "",
	}
	p.ensureGroup("")

	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if err := p.parseLine(sc.Text()); err != nil {
			return nil, err
		}
	}
	return p.buildScene()
}

func (p *objParser) ensureGroup(mat string) *objGroup {
	key := mat
	if _, ok := p.groups[key]; !ok {
		p.groups[key] = &objGroup{matName: mat, vertMap: map[objVertKey]uint32{}}
		p.groupOrder = append(p.groupOrder, key)
	}
	return p.groups[key]
}

func (p *objParser) parseLine(line string) error {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' {
		return nil
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}

	switch fields[0] {
	case "v":
		v, err := parseVec3(fields[1:])
		if err != nil {
			return err
		}
		p.positions = append(p.positions, v)
	case "vn":
		v, err := parseVec3(fields[1:])
		if err != nil {
			return err
		}
		p.normals = append(p.normals, v)
	case "vt":
		u, err := parseFloat(fields[1])
		if err != nil {
			return err
		}
		v := float32(0)
		if len(fields) > 2 {
			v, err = parseFloat(fields[2])
			if err != nil {
				return err
			}
		}
		p.uvs = append(p.uvs, glm.Vec2f{u, v})
	case "f":
		if err := p.parseFace(fields[1:]); err != nil {
			return err
		}
	case "usemtl":
		if len(fields) > 1 {
			p.current = fields[1]
			p.ensureGroup(p.current)
		}
	case "mtllib":
		if len(fields) > 1 && p.baseDir != "" {
			p.loadMTL(filepath.Join(p.baseDir, fields[1]))
		}
	case "g", "o":
		// groups are distinguished by material, not name
	}
	return nil
}

func (p *objParser) parseFace(specs []string) error {
	if len(specs) < 3 {
		return nil
	}
	keys := make([]objVertKey, len(specs))
	for i, s := range specs {
		k, err := parseVertSpec(s, len(p.positions), len(p.uvs), len(p.normals))
		if err != nil {
			return fmt.Errorf("obj: face vertex %q: %w", s, err)
		}
		keys[i] = k
	}

	g := p.ensureGroup(p.current)
	// Fan triangulation of n-gons.
	for i := 1; i < len(keys)-1; i++ {
		g.addTri(keys[0], keys[i], keys[i+1], p)
	}
	return nil
}

func (g *objGroup) addTri(k0, k1, k2 objVertKey, p *objParser) {
	if k0.vn < 0 && k1.vn < 0 && k2.vn < 0 {
		// No normals in OBJ: compute flat normal and attach a unique vn index per triangle.
		// We use a fake vn index = len(existing normals) + 0 for each tri so dedup still works.
		// Actually, just store the resolved position and compute a shared flat normal later.
		// For simplicity: add a generated normal entry on-the-fly.
		pos0 := p.positions[k0.v]
		pos1 := p.positions[k1.v]
		pos2 := p.positions[k2.v]
		n := computeFlatNormal(pos0, pos1, pos2)
		ni := len(p.normals)
		p.normals = append(p.normals, n)
		k0.vn, k1.vn, k2.vn = ni, ni, ni
	}
	g.addVert(k0, p)
	g.addVert(k1, p)
	g.addVert(k2, p)
}

func (g *objGroup) addVert(k objVertKey, p *objParser) {
	if idx, ok := g.vertMap[k]; ok {
		g.idx = append(g.idx, idx)
		return
	}
	idx := uint32(len(g.pos))
	g.vertMap[k] = idx
	g.idx = append(g.idx, idx)
	g.pos = append(g.pos, p.positions[k.v])
	if k.vn >= 0 {
		g.nor = append(g.nor, p.normals[k.vn])
	} else {
		g.nor = append(g.nor, glm.Vec3f{0, 1, 0})
	}
	if k.vt >= 0 {
		g.uv = append(g.uv, p.uvs[k.vt])
	} else {
		g.uv = append(g.uv, glm.Vec2f{0, 0})
	}
}

func (p *objParser) buildScene() (*pix.Scene, error) {
	scene := pix.NewScene()
	texCache := map[string]pix.Texture{}

	for _, key := range p.groupOrder {
		g := p.groups[key]
		if len(g.idx) == 0 {
			continue
		}

		geo := &pix.GeometryData{}
		geo.SetIndices(g.idx)
		geo.AddAttribute(pix.NewAttribute(pix.PositionAttrName, pix.PositionLocation, pix.Float32x3, g.pos))
		geo.AddAttribute(pix.NewAttribute(pix.NormalAttrName, pix.NormalLocation, pix.Float32x3, g.nor))
		geo.AddAttribute(pix.NewAttribute(pix.UVAttrName, pix.UVLocation, pix.Float32x2, g.uv))

		pixGeo := p.r.NewGeometry(geo)

		mat := p.r.NewBlinnPhongMaterial()
		if m, ok := p.mats[g.matName]; ok {
			mat.SetColor(m.color)
			if m.texPath != "" {
				if tex, ok := texCache[m.texPath]; ok {
					mat.SetColorMap(tex)
				} else if tex, err := loadImageFile(p.r, m.texPath); err == nil {
					texCache[m.texPath] = tex
					mat.SetColorMap(tex)
				}
			}
		}

		pixMat := mat.Ref()
		mat.Release()

		mesh := scene.NewMesh(pixGeo, pixMat)
		pixGeo.Release()
		pixMat.Release()

		scene.Add(mesh)
	}

	for _, tex := range texCache {
		tex.Release()
	}
	return scene, nil
}

func (p *objParser) loadMTL(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cur *objMat
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || fields[0] == "#" {
			continue
		}
		switch fields[0] {
		case "newmtl":
			if len(fields) > 1 {
				name := fields[1]
				m := &objMat{color: glm.Color3f{1, 1, 1}}
				p.mats[name] = m
				cur = m
			}
		case "Kd":
			if cur != nil && len(fields) >= 4 {
				r, _ := strconv.ParseFloat(fields[1], 32)
				g, _ := strconv.ParseFloat(fields[2], 32)
				b, _ := strconv.ParseFloat(fields[3], 32)
				cur.color = glm.Color3f{float32(r), float32(g), float32(b)}
			}
		case "map_Kd":
			if cur != nil && len(fields) > 1 {
				cur.texPath = filepath.Join(p.baseDir, fields[len(fields)-1])
			}
		}
	}
}

// ---- helpers ----

func parseVertSpec(s string, nPos, nUV, nNor int) (objVertKey, error) {
	parts := strings.Split(s, "/")
	k := objVertKey{v: -1, vt: -1, vn: -1}

	v, err := resolveOBJIndex(parts[0], nPos)
	if err != nil {
		return k, err
	}
	k.v = v

	if len(parts) > 1 && parts[1] != "" {
		vt, err := resolveOBJIndex(parts[1], nUV)
		if err != nil {
			return k, err
		}
		k.vt = vt
	}
	if len(parts) > 2 && parts[2] != "" {
		vn, err := resolveOBJIndex(parts[2], nNor)
		if err != nil {
			return k, err
		}
		k.vn = vn
	}
	return k, nil
}

func resolveOBJIndex(s string, count int) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return count + n, nil
	}
	return n - 1, nil // 1-based → 0-based
}

func parseVec3(fields []string) (glm.Vec3f, error) {
	if len(fields) < 3 {
		return glm.Vec3f{}, fmt.Errorf("not enough components")
	}
	x, err := parseFloat(fields[0])
	if err != nil {
		return glm.Vec3f{}, err
	}
	y, err := parseFloat(fields[1])
	if err != nil {
		return glm.Vec3f{}, err
	}
	z, err := parseFloat(fields[2])
	if err != nil {
		return glm.Vec3f{}, err
	}
	return glm.Vec3f{x, y, z}, nil
}

func parseFloat(s string) (float32, error) {
	v, err := strconv.ParseFloat(s, 32)
	return float32(v), err
}

func computeFlatNormal(p0, p1, p2 glm.Vec3f) glm.Vec3f {
	e1 := glm.Vec3f{p1[0] - p0[0], p1[1] - p0[1], p1[2] - p0[2]}
	e2 := glm.Vec3f{p2[0] - p0[0], p2[1] - p0[1], p2[2] - p0[2]}
	n := glm.Vec3f{
		e1[1]*e2[2] - e1[2]*e2[1],
		e1[2]*e2[0] - e1[0]*e2[2],
		e1[0]*e2[1] - e1[1]*e2[0],
	}
	l := float32(math.Sqrt(float64(n[0]*n[0] + n[1]*n[1] + n[2]*n[2])))
	if l > 0 {
		n[0] /= l
		n[1] /= l
		n[2] /= l
	}
	return n
}

func loadImageFile(r *pix.Renderer, path string) (pix.Texture, error) {
	f, err := os.Open(path)
	if err != nil {
		return pix.Texture{}, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return pix.Texture{}, err
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	pixels := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			rv, g, b, a := img.At(x, y).RGBA()
			i := (y*w + x) * 4
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = byte(rv>>8), byte(g>>8), byte(b>>8), byte(a>>8)
		}
	}
	td := pix.NewDataTexture(pixels, w, h, pix.TextureFormatRGBA8Unorm)
	return r.NewTexture(td), nil
}
