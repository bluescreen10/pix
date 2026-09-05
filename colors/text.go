package colors

import (
	"fmt"
	"strconv"
	"strings"
)

// String renders a colour as space-separated channels, which is what Parse reads back.
func (c RGB[T]) String() string {
	return fmt.Sprintf("%v %v %v", c[0], c[1], c[2])
}

// String renders a colour as space-separated channels, which is what Parse reads back.
func (c RGBA[T]) String() string {
	return fmt.Sprintf("%v %v %v %v", c[0], c[1], c[2], c[3])
}

// ParseRGB32F reads "r g b" (or comma-separated) with channels in [0,1]. A single
// value is read as grey.
func ParseRGB32F(s string) (RGB32F, error) {
	f, err := channels(s, 1, 3)
	if err != nil {
		return RGB32F{}, err
	}
	if len(f) == 1 {
		return RGB32F{f[0], f[0], f[0]}, nil
	}
	if len(f) != 3 {
		return RGB32F{}, fmt.Errorf("want 1 or 3 channels, got %d", len(f))
	}
	return RGB32F{f[0], f[1], f[2]}, nil
}

// ParseRGBA32F reads "r g b" or "r g b a" (or comma-separated) with channels in [0,1].
// A single value is read as opaque grey; three leave alpha at 1.
func ParseRGBA32F(s string) (RGBA32F, error) {
	f, err := channels(s, 1, 4)
	if err != nil {
		return RGBA32F{}, err
	}
	switch len(f) {
	case 1:
		return RGBA32F{f[0], f[0], f[0], 1}, nil
	case 3:
		return RGBA32F{f[0], f[1], f[2], 1}, nil
	case 4:
		return RGBA32F{f[0], f[1], f[2], f[3]}, nil
	}
	return RGBA32F{}, fmt.Errorf("want 1, 3 or 4 channels, got %d", len(f))
}

// channels splits on whitespace or commas and parses each field as a float32.
func channels(s string, minN, maxN int) ([]float32, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == ','
	})
	if len(fields) < minN || len(fields) > maxN {
		return nil, fmt.Errorf("want %d to %d channels, got %d", minN, maxN, len(fields))
	}
	out := make([]float32, len(fields))
	for i, f := range fields {
		v, err := strconv.ParseFloat(f, 32)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", f)
		}
		out[i] = float32(v)
	}
	return out, nil
}
