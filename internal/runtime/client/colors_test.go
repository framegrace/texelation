// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestParseHexColor(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValid bool
		wantR     int32
		wantG     int32
		wantB     int32
	}{
		{
			name:      "valid hex white",
			input:     "#ffffff",
			wantValid: true,
			wantR:     255,
			wantG:     255,
			wantB:     255,
		},
		{
			name:      "valid hex black",
			input:     "#000000",
			wantValid: true,
			wantR:     0,
			wantG:     0,
			wantB:     0,
		},
		{
			name:      "valid hex red",
			input:     "#ff0000",
			wantValid: true,
			wantR:     255,
			wantG:     0,
			wantB:     0,
		},
		{
			name:      "valid hex green",
			input:     "#00ff00",
			wantValid: true,
			wantR:     0,
			wantG:     255,
			wantB:     0,
		},
		{
			name:      "valid hex blue",
			input:     "#0000ff",
			wantValid: true,
			wantR:     0,
			wantG:     0,
			wantB:     255,
		},
		{
			name:      "valid hex mixed",
			input:     "#ff5733",
			wantValid: true,
			wantR:     255,
			wantG:     87,
			wantB:     51,
		},
		{
			name:      "empty string",
			input:     "",
			wantValid: false,
		},
		{
			name:      "missing hash",
			input:     "ffffff",
			wantValid: false,
		},
		{
			name:      "too short",
			input:     "#fff",
			wantValid: false,
		},
		{
			name:      "too long",
			input:     "#ffffff00",
			wantValid: false,
		},
		{
			name:      "invalid characters",
			input:     "#gggggg",
			wantValid: false,
		},
		{
			name:      "uppercase hex",
			input:     "#ABCDEF",
			wantValid: true,
			wantR:     171,
			wantG:     205,
			wantB:     239,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			color, valid := parseHexColor(tt.input)

			if valid != tt.wantValid {
				t.Errorf("parseHexColor(%q) valid = %v, want %v", tt.input, valid, tt.wantValid)
			}

			if tt.wantValid {
				r, g, b := color.RGB()
				if r != tt.wantR || g != tt.wantG || b != tt.wantB {
					t.Errorf("parseHexColor(%q) RGB = (%d, %d, %d), want (%d, %d, %d)",
						tt.input, r, g, b, tt.wantR, tt.wantG, tt.wantB)
				}
			} else {
				if color != tcell.ColorDefault {
					t.Errorf("parseHexColor(%q) should return ColorDefault when invalid", tt.input)
				}
			}
		})
	}
}

func TestColorFromRGB(t *testing.T) {
	tests := []struct {
		name  string
		rgb   uint32
		wantR int32
		wantG int32
		wantB int32
	}{
		{
			name:  "white",
			rgb:   0xffffff,
			wantR: 255,
			wantG: 255,
			wantB: 255,
		},
		{
			name:  "black",
			rgb:   0x000000,
			wantR: 0,
			wantG: 0,
			wantB: 0,
		},
		{
			name:  "red",
			rgb:   0xff0000,
			wantR: 255,
			wantG: 0,
			wantB: 0,
		},
		{
			name:  "green",
			rgb:   0x00ff00,
			wantR: 0,
			wantG: 255,
			wantB: 0,
		},
		{
			name:  "blue",
			rgb:   0x0000ff,
			wantR: 0,
			wantG: 0,
			wantB: 255,
		},
		{
			name:  "mixed color",
			rgb:   0xff5733,
			wantR: 255,
			wantG: 87,
			wantB: 51,
		},
		{
			name:  "gray",
			rgb:   0x808080,
			wantR: 128,
			wantG: 128,
			wantB: 128,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			color := colorFromRGB(tt.rgb)
			r, g, b := color.RGB()

			if r != tt.wantR || g != tt.wantG || b != tt.wantB {
				t.Errorf("colorFromRGB(0x%06x) RGB = (%d, %d, %d), want (%d, %d, %d)",
					tt.rgb, r, g, b, tt.wantR, tt.wantG, tt.wantB)
			}
		})
	}
}

func TestColorRoundTrip(t *testing.T) {
	// Test that parsing a hex color and converting back gives consistent results
	hexColors := []string{
		"#ffffff",
		"#000000",
		"#ff0000",
		"#00ff00",
		"#0000ff",
		"#123456",
		"#abcdef",
	}

	for _, hex := range hexColors {
		t.Run(hex, func(t *testing.T) {
			color1, valid := parseHexColor(hex)
			if !valid {
				t.Fatalf("parseHexColor(%q) should be valid", hex)
			}

			r, g, b := color1.RGB()
			rgb := (uint32(r) << 16) | (uint32(g) << 8) | uint32(b)

			color2 := colorFromRGB(rgb)
			r2, g2, b2 := color2.RGB()

			if r != r2 || g != g2 || b != b2 {
				t.Errorf("Round trip failed for %q: (%d,%d,%d) != (%d,%d,%d)",
					hex, r, g, b, r2, g2, b2)
			}
		})
	}
}
