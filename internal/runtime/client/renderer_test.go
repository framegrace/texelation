// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestBlendColor(t *testing.T) {
	tests := []struct {
		name      string
		base      tcell.Color
		overlay   tcell.Color
		intensity float32
		wantR     int32
		wantG     int32
		wantB     int32
	}{
		{
			name:      "zero intensity returns base",
			base:      tcell.NewRGBColor(255, 0, 0), // Red
			overlay:   tcell.NewRGBColor(0, 0, 255), // Blue
			intensity: 0.0,
			wantR:     255,
			wantG:     0,
			wantB:     0,
		},
		{
			name:      "full intensity returns overlay",
			base:      tcell.NewRGBColor(255, 0, 0), // Red
			overlay:   tcell.NewRGBColor(0, 0, 255), // Blue
			intensity: 1.0,
			wantR:     0,
			wantG:     0,
			wantB:     255,
		},
		{
			name:      "half intensity blends colors",
			base:      tcell.NewRGBColor(255, 0, 0), // Red
			overlay:   tcell.NewRGBColor(0, 0, 255), // Blue
			intensity: 0.5,
			wantR:     127, // (255*0.5 + 0*0.5)
			wantG:     0,
			wantB:     127, // (0*0.5 + 255*0.5)
		},
		{
			name:      "blend white and black at 25%",
			base:      tcell.NewRGBColor(0, 0, 0),       // Black
			overlay:   tcell.NewRGBColor(255, 255, 255), // White
			intensity: 0.25,
			wantR:     63, // (0*0.75 + 255*0.25)
			wantG:     63,
			wantB:     63,
		},
		{
			name:      "blend gray shades",
			base:      tcell.NewRGBColor(100, 100, 100),
			overlay:   tcell.NewRGBColor(200, 200, 200),
			intensity: 0.5,
			wantR:     150, // (100*0.5 + 200*0.5)
			wantG:     150,
			wantB:     150,
		},
		{
			name:      "invalid overlay returns base",
			base:      tcell.NewRGBColor(255, 128, 64),
			overlay:   tcell.ColorDefault, // Invalid
			intensity: 0.5,
			wantR:     255,
			wantG:     128,
			wantB:     64,
		},
		{
			name:      "invalid base returns overlay",
			base:      tcell.ColorDefault, // Invalid
			overlay:   tcell.NewRGBColor(100, 150, 200),
			intensity: 0.5,
			wantR:     100,
			wantG:     150,
			wantB:     200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := blendColor(tt.base, tt.overlay, tt.intensity)
			r, g, b := result.RGB()

			if r != tt.wantR || g != tt.wantG || b != tt.wantB {
				t.Errorf("blendColor() RGB = (%d, %d, %d), want (%d, %d, %d)",
					r, g, b, tt.wantR, tt.wantG, tt.wantB)
			}
		})
	}
}

func TestApplyZoomOverlay(t *testing.T) {
	state := &clientState{
		defaultFg: tcell.NewRGBColor(255, 255, 255), // White
		defaultBg: tcell.NewRGBColor(0, 0, 0),       // Black
		desktopBg: tcell.NewRGBColor(32, 32, 32),    // Dark gray
	}

	t.Run("zero intensity returns original style", func(t *testing.T) {
		original := tcell.StyleDefault.
			Foreground(tcell.NewRGBColor(200, 100, 50)).
			Background(tcell.NewRGBColor(10, 20, 30))

		result := applyZoomOverlay(original, 0.0, state)

		if result != original {
			t.Error("zero intensity should return original style unchanged")
		}
	})

	t.Run("applies zoom overlay with intensity", func(t *testing.T) {
		original := tcell.StyleDefault.
			Foreground(tcell.NewRGBColor(200, 100, 50)).
			Background(tcell.NewRGBColor(10, 20, 30))

		result := applyZoomOverlay(original, 0.2, state)

		fg, bg, attrs := result.Decompose()

		// Should blend with outline color (120, 200, 255)
		fgR, fgG, fgB := fg.RGB()
		if fgR == 200 && fgG == 100 && fgB == 50 {
			t.Error("foreground should be blended, but appears unchanged")
		}

		bgR, bgG, bgB := bg.RGB()
		if bgR == 10 && bgG == 20 && bgB == 30 {
			t.Error("background should be blended, but appears unchanged")
		}

		// Should set bold
		if attrs&tcell.AttrBold == 0 {
			t.Error("zoom overlay should set bold attribute")
		}
	})

	t.Run("uses default colors when style has invalid colors", func(t *testing.T) {
		original := tcell.StyleDefault

		result := applyZoomOverlay(original, 0.2, state)

		fg, _, _ := result.Decompose()

		// Should use state.defaultFg (white)
		fgR, fgG, fgB := fg.RGB()
		if fgR == 255 && fgG == 255 && fgB == 255 {
			// Should be blended with outline, not pure white
			t.Error("foreground should be blended with outline")
		}
	})

	t.Run("preserves underline attribute", func(t *testing.T) {
		original := tcell.StyleDefault.Underline(true)

		result := applyZoomOverlay(original, 0.2, state)

		_, _, attrs := result.Decompose()

		if attrs&tcell.AttrUnderline == 0 {
			t.Error("underline attribute should be preserved")
		}
	})

	t.Run("preserves italic attribute", func(t *testing.T) {
		original := tcell.StyleDefault.Italic(true)

		result := applyZoomOverlay(original, 0.2, state)

		_, _, attrs := result.Decompose()

		if attrs&tcell.AttrItalic == 0 {
			t.Error("italic attribute should be preserved")
		}
	})
}

func TestBlendColorSymmetry(t *testing.T) {
	// Test that blending is consistent
	red := tcell.NewRGBColor(255, 0, 0)
	blue := tcell.NewRGBColor(0, 0, 255)

	// Blending red->blue at 0.3 should give same result as blue->red at 0.7
	blend1 := blendColor(red, blue, 0.3)
	blend2 := blendColor(blue, red, 0.7)

	r1, g1, b1 := blend1.RGB()
	r2, g2, b2 := blend2.RGB()

	if r1 != r2 || g1 != g2 || b1 != b2 {
		t.Errorf("Blending should be symmetric: (%d,%d,%d) != (%d,%d,%d)",
			r1, g1, b1, r2, g2, b2)
	}
}
