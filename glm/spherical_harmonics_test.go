package glm_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix/glm"
)

// projectHarmonics projects radiance onto spherical harmonics from samples spread evenly
// over the sphere, on a Fibonacci spiral.
func projectHarmonics(radiance func(direction glm.Vec3f) glm.Vec3f) glm.SphericalHarmonics {
	const samples = 20000
	golden := math.Pi * (3 - math.Sqrt(5))
	var harmonics glm.SphericalHarmonics
	for i := range samples {
		y := 1 - 2*(float64(i)+0.5)/samples
		radius := math.Sqrt(1 - y*y)
		angle := golden * float64(i)
		direction := glm.Vec3f{float32(radius * math.Cos(angle)), float32(y), float32(radius * math.Sin(angle))}
		harmonics = harmonics.AddSample(direction, radiance(direction), 4*math.Pi/samples)
	}
	return harmonics
}

// isNear reports whether every component of got is within tolerance of want.
func isNear(got, want glm.Vec3f, tolerance float32) bool {
	for i := range got {
		if math.Abs(float64(got[i]-want[i])) > float64(tolerance) {
			return false
		}
	}
	return true
}

// The directions the tests below look in: the axes both ways, and two slanting ones.
var harmonicsDirections = []glm.Vec3f{
	{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1},
	glm.Vec3f{1, 2, 3}.Normalize(), glm.Vec3f{-2, 0.5, 1}.Normalize(),
}

// TestSphericalHarmonicsHoldUniformLight: light the same from everywhere comes back the
// same in every direction, and falls on every surface as π times itself.
func TestSphericalHarmonicsHoldUniformLight(t *testing.T) {
	light := glm.Vec3f{0.25, 0.5, 1}
	harmonics := projectHarmonics(func(glm.Vec3f) glm.Vec3f {
		return light
	})
	for _, direction := range harmonicsDirections {
		if got := harmonics.Evaluate(direction); !isNear(got, light, 1e-3) {
			t.Errorf("Evaluate(%v) = %v, want %v", direction, got, light)
		}
		if got, want := harmonics.Irradiance(direction), light.Scale(math.Pi); !isNear(got, want, 1e-3) {
			t.Errorf("Irradiance(%v) = %v, want %v", direction, got, want)
		}
	}
}

// TestSphericalHarmonicsHoldEveryBand: a function made of harmonics from every band —
// constant, linear in x, and quadratic in xy and xz — comes back exactly, and its irradiance
// is each part weighted by its band's share of the cosine lobe: π, 2π/3 and π/4.
func TestSphericalHarmonicsHoldEveryBand(t *testing.T) {
	radiance := func(d glm.Vec3f) glm.Vec3f {
		return glm.Vec3f{1, d[0], d[0]*d[1] + d[0]*d[2]}
	}
	harmonics := projectHarmonics(radiance)
	for _, direction := range harmonicsDirections {
		if got, want := harmonics.Evaluate(direction), radiance(direction); !isNear(got, want, 1e-3) {
			t.Errorf("Evaluate(%v) = %v, want %v", direction, got, want)
		}
		value := radiance(direction)
		want := glm.Vec3f{math.Pi * value[0], 2 * math.Pi / 3 * value[1], math.Pi / 4 * value[2]}
		if got := harmonics.Irradiance(direction); !isNear(got, want, 1e-3) {
			t.Errorf("Irradiance(%v) = %v, want %v", direction, got, want)
		}
	}
}

// TestSphericalHarmonicsIrradianceOfHalfTheSky: red light from above and blue from
// below fall on a surface whose normal is y up in proportion to how much of its
// hemisphere each fills, cosine weighted: π (1 + y) / 2 red and π (1 - y) / 2 blue.
// Nine coefficients give that exactly, though they hold the sharp horizon itself only
// blurred.
func TestSphericalHarmonicsIrradianceOfHalfTheSky(t *testing.T) {
	harmonics := projectHarmonics(func(d glm.Vec3f) glm.Vec3f {
		if d[1] > 0 {
			return glm.Vec3f{1, 0, 0}
		}
		return glm.Vec3f{0, 0, 1}
	})
	for _, direction := range harmonicsDirections {
		y := float64(direction[1])
		want := glm.Vec3f{float32(math.Pi * (1 + y) / 2), 0, float32(math.Pi * (1 - y) / 2)}
		if got := harmonics.Irradiance(direction); !isNear(got, want, 1e-2) {
			t.Errorf("Irradiance(%v) = %v, want %v", direction, got, want)
		}
	}
}

// TestSphericalHarmonicsBlend: harmonics added and scaled are those of the functions
// added and scaled, so probes blend coefficient by coefficient.
func TestSphericalHarmonicsBlend(t *testing.T) {
	upward := projectHarmonics(func(d glm.Vec3f) glm.Vec3f {
		return glm.Vec3f{d[1], 0, 0}
	})
	sideways := projectHarmonics(func(d glm.Vec3f) glm.Vec3f {
		return glm.Vec3f{0, d[0] * d[0], 0}
	})
	blend := upward.Scale(0.25).Add(sideways.Scale(0.75))
	for _, direction := range harmonicsDirections {
		want := glm.Vec3f{0.25 * direction[1], 0.75 * direction[0] * direction[0], 0}
		if got := blend.Evaluate(direction); !isNear(got, want, 1e-3) {
			t.Errorf("blend.Evaluate(%v) = %v, want %v", direction, got, want)
		}
	}
}
