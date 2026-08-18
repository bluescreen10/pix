package geometry

import "github.com/bluescreen10/dawn-go/wgpu"

type GeoDesc struct {
	Position uint32
	Normal   uint32
	UVS      uint32
	Colors   uint32
}

type AttrType int

type GeoSystem struct {
	buffers map[AttrType]*wgpu.Buffer
	desc    pix.Slab[GeoDesc]
	device  *wgpu.Device

	pendingUpdate    map[GeometryRef]struct{}
	pendeingDeletion map[GeometryRef]struct{}
}

type GeoID int

type GeometryConfig struct{}

func (s GeoSystem) New(GeometryConfig) GeometryRef {
	return 0
}

func (s GeoSystem) GetGeometryAttribute(GeometryRef, AttrType)

func (s GeoSystem) SetGeometryAttribute(GeometryRef, AttrType, []byte)

type Renderer struct {
	Geometries GeoSystem
}

type GeometryRef struct {
}
