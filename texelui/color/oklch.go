// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/color/oklch.go
// Summary: OKLCH color space conversion utilities.

package color

import "math"

// RGB represents an RGB color with 8-bit components.
type RGB struct {
	R, G, B int32
}

// OKLCHToRGB converts OKLCH color space to RGB.
// L: Lightness (0.0 - 1.0)
// C: Chroma (0.0 - ~0.4 for most displayable colors)
// H: Hue (0 - 360 degrees)
func OKLCHToRGB(L, C, H float64) RGB {
	// Convert OKLCH -> OKLAB -> Linear RGB -> sRGB

	// Step 1: OKLCH -> OKLAB
	// a = C * cos(H)
	// b = C * sin(H)
	hRad := H * math.Pi / 180.0
	a := C * math.Cos(hRad)
	b := C * math.Sin(hRad)

	// Step 2: OKLAB -> LMS (cube root space)
	l_ := L + 0.3963377774*a + 0.2158037573*b
	m_ := L - 0.1055613458*a - 0.0638541728*b
	s_ := L - 0.0894841775*a - 1.2914855480*b

	// Cube to get linear LMS
	l := l_ * l_ * l_
	m := m_ * m_ * m_
	s := s_ * s_ * s_

	// Step 3: LMS -> Linear RGB
	lr := +4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	lg := -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	lb := -0.0041960863*l - 0.7034186147*m + 1.7076147010*s

	// Step 4: Linear RGB -> sRGB (gamma correction)
	r := linearToSRGB(lr)
	g := linearToSRGB(lg)
	b_rgb := linearToSRGB(lb)

	// Clamp to valid range
	r = clamp(r, 0, 255)
	g = clamp(g, 0, 255)
	b_rgb = clamp(b_rgb, 0, 255)

	return RGB{R: r, G: g, B: b_rgb}
}

// RGBToOKLCH converts RGB to OKLCH color space.
// Returns L (0-1), C (0-~0.4), H (0-360).
func RGBToOKLCH(r, g, b int32) (L, C, H float64) {
	// Convert sRGB -> Linear RGB -> OKLAB -> OKLCH

	// Step 1: sRGB -> Linear RGB
	lr := sRGBToLinear(float64(r) / 255.0)
	lg := sRGBToLinear(float64(g) / 255.0)
	lb := sRGBToLinear(float64(b) / 255.0)

	// Step 2: Linear RGB -> LMS
	l := 0.4122214708*lr + 0.5363325363*lg + 0.0514459929*lb
	m := 0.2119034982*lr + 0.6806995451*lg + 0.1073969566*lb
	s := 0.0883024619*lr + 0.2817188376*lg + 0.6299787005*lb

	// Step 3: LMS -> OKLAB (cube root)
	l_ := math.Cbrt(l)
	m_ := math.Cbrt(m)
	s_ := math.Cbrt(s)

	L = 0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_
	a := 1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_
	b_lab := 0.0259040371*l_ + 0.7827717662*m_ - 0.8086757660*s_

	// Step 4: OKLAB -> OKLCH
	C = math.Sqrt(a*a + b_lab*b_lab)
	H = math.Atan2(b_lab, a) * 180.0 / math.Pi
	if H < 0 {
		H += 360.0
	}

	return L, C, H
}

// linearToSRGB converts linear RGB to sRGB with gamma correction.
func linearToSRGB(c float64) int32 {
	if c <= 0.0031308 {
		return int32(c * 12.92 * 255.0)
	}
	return int32((1.055*math.Pow(c, 1.0/2.4) - 0.055) * 255.0)
}

// sRGBToLinear converts sRGB to linear RGB (inverse gamma correction).
func sRGBToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// clamp restricts a value to the range [min, max].
func clamp(v, min, max int32) int32 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
