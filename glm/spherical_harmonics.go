package glm

import "github.com/chewxy/math32"

// SphericalHarmonics is a colour that varies smoothly over directions — the light
// arriving at a point from all around it, say — held as its first nine real spherical
// harmonic coefficients, bands 0 to 2, one RGB value each. Nine are enough for diffuse
// lighting, which is why light probes store them: the cosine lobe diffuse light is
// blurred by leaves almost nothing of the bands beyond them. The zero value is black in
// every direction.
//
// The coefficients are in the usual order: Y00; Y1-1, Y10, Y11; Y2-2, Y2-1, Y20, Y21,
// Y22 — with y, z, x as the first band's directions, as most engines and papers
// write them.
type SphericalHarmonics [9]Vec3f

// harmonicsBasis returns the nine basis functions' values in direction, which must be
// unit length.
func harmonicsBasis(direction Vec3f) [9]float32 {
	x, y, z := direction[0], direction[1], direction[2]
	return [9]float32{
		0.282095,
		0.488603 * y, 0.488603 * z, 0.488603 * x,
		1.092548 * x * y, 1.092548 * y * z, 0.315392 * (3*z*z - 1), 1.092548 * x * z, 0.546274 * (x*x - y*y),
	}
}

// AddSample returns s with one sample of the function projected onto it: value, seen
// in direction, which must be unit length, standing for weight steradians of the
// sphere. Samples spread evenly over the whole sphere each weigh 4π divided by their
// count.
func (s SphericalHarmonics) AddSample(direction, value Vec3f, weight float32) SphericalHarmonics {
	basis := harmonicsBasis(direction)
	for i := range s {
		s[i] = s[i].Add(value.Scale(basis[i] * weight))
	}
	return s
}

// Add returns the coefficient-wise sum of s and other: the harmonics of the two
// functions added together.
func (s SphericalHarmonics) Add(other SphericalHarmonics) SphericalHarmonics {
	for i := range s {
		s[i] = s[i].Add(other[i])
	}
	return s
}

// Scale returns s with every coefficient multiplied by scalar. With Add, it blends
// probes: a.Scale(1-t).Add(b.Scale(t)).
func (s SphericalHarmonics) Scale(scalar float32) SphericalHarmonics {
	for i := range s {
		s[i] = s[i].Scale(scalar)
	}
	return s
}

// Evaluate returns the function's value in direction, which must be unit length, as
// nine coefficients hold it: blurred, so sharp detail such as a sun is spread wide.
func (s SphericalHarmonics) Evaluate(direction Vec3f) Vec3f {
	basis := harmonicsBasis(direction)
	var value Vec3f
	for i := range s {
		value = value.Add(s[i].Scale(basis[i]))
	}
	return value
}

// Irradiance returns the light falling on a surface facing normal, which must be unit
// length, when s is the radiance arriving from every direction: its integral over the
// hemisphere about normal, each direction weighted by its cosine to it. A uniform
// radiance of 1 gives π; divide by π for the light a white diffuse surface reflects.
func (s SphericalHarmonics) Irradiance(normal Vec3f) Vec3f {
	// The cosine lobe's convolution of each band (Ramamoorthi and Hanrahan, 2001).
	bandWeights := [3]float32{math32.Pi, 2 * math32.Pi / 3, math32.Pi / 4}
	basis := harmonicsBasis(normal)
	var irradiance Vec3f
	for i := range s {
		band := 0
		if i >= 4 {
			band = 2
		} else if i >= 1 {
			band = 1
		}
		irradiance = irradiance.Add(s[i].Scale(basis[i] * bandWeights[band]))
	}
	return irradiance
}
