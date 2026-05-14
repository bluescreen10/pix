package pix

import "github.com/bluescreen10/dawn-go/wgpu"

// Side controls which faces are rendered (and thus which are culled).
type Side uint8

const (
	SideFront Side = iota // back faces culled (default)
	SideBack              // front faces culled (used for shadow passes)
	SideBoth              // no culling
)

func (s Side) ToWGPU() wgpu.CullMode {
	switch s {
	case SideFront:
		return wgpu.CullModeBack
	case SideBack:
		return wgpu.CullModeFront
	case SideBoth:
		return wgpu.CullModeNone
	default:
		return wgpu.CullModeBack
	}
}

// BlendMode controls how the fragment color is blended with the framebuffer.
type BlendMode uint8

const (
	BlendOpaque      BlendMode = iota // no blending
	BlendNormal                       // alpha-premultiplied src over dst
	BlendAdditive                     // src added onto dst (glow, fire)
	BlendSubtractive                  // dst darkened by src
	BlendMultiply                     // src * dst
)

func (b BlendMode) ToWGPU() *wgpu.BlendState {
	switch b {
	case BlendNormal:
		return &wgpu.BlendState{
			Color: wgpu.BlendComponent{
				Operation: wgpu.BlendOperationAdd,
				SrcFactor: wgpu.BlendFactorSrcAlpha,
				DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
			},
			Alpha: wgpu.BlendComponent{
				Operation: wgpu.BlendOperationAdd,
				SrcFactor: wgpu.BlendFactorOne,
				DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
			},
		}
	case BlendAdditive:
		return &wgpu.BlendState{
			Color: wgpu.BlendComponent{
				Operation: wgpu.BlendOperationAdd,
				SrcFactor: wgpu.BlendFactorSrcAlpha,
				DstFactor: wgpu.BlendFactorOne,
			},
			Alpha: wgpu.BlendComponent{
				Operation: wgpu.BlendOperationAdd,
				SrcFactor: wgpu.BlendFactorOne,
				DstFactor: wgpu.BlendFactorOne,
			},
		}
	case BlendSubtractive:
		return &wgpu.BlendState{
			Color: wgpu.BlendComponent{
				Operation: wgpu.BlendOperationReverseSubtract,
				SrcFactor: wgpu.BlendFactorSrcAlpha,
				DstFactor: wgpu.BlendFactorOne,
			},
			Alpha: wgpu.BlendComponent{
				Operation: wgpu.BlendOperationAdd,
				SrcFactor: wgpu.BlendFactorZero,
				DstFactor: wgpu.BlendFactorOne,
			},
		}
	case BlendMultiply:
		return &wgpu.BlendState{
			Color: wgpu.BlendComponent{
				Operation: wgpu.BlendOperationAdd,
				SrcFactor: wgpu.BlendFactorDst,
				DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
			},
			Alpha: wgpu.BlendComponent{
				Operation: wgpu.BlendOperationAdd,
				SrcFactor: wgpu.BlendFactorDstAlpha,
				DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
			},
		}
	default: // BlendOpaque
		return nil
	}
}

// DepthFunc selects the depth comparison used when depth testing is enabled.
type DepthFunc uint8

const (
	DepthFuncLess      DepthFunc = iota // default for color pass
	DepthFuncLessEqual                  // default for shadow pass
	DepthFuncEqual
	DepthFuncGreaterEqual
	DepthFuncGreater
	DepthFuncNotEqual
	DepthFuncAlways
	DepthFuncNever
)

func (d DepthFunc) ToWGPU() wgpu.CompareFunction {
	switch d {
	case DepthFuncLess:
		return wgpu.CompareFunctionLess
	case DepthFuncLessEqual:
		return wgpu.CompareFunctionLessEqual
	case DepthFuncEqual:
		return wgpu.CompareFunctionEqual
	case DepthFuncGreaterEqual:
		return wgpu.CompareFunctionGreaterEqual
	case DepthFuncGreater:
		return wgpu.CompareFunctionGreater
	case DepthFuncNotEqual:
		return wgpu.CompareFunctionNotEqual
	case DepthFuncAlways:
		return wgpu.CompareFunctionAlways
	case DepthFuncNever:
		return wgpu.CompareFunctionNever
	default:
		return wgpu.CompareFunctionLess
	}
}
