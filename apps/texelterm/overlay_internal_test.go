package texelterm

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"texelation/apps/texelterm/parser"
	"texelation/texel/theme"
)

// TestOverlayAppearsOnLongLineWithoutPTY verifies the long-line TextArea overlay activates
// when the caret moves beyond the visible width, without starting a PTY.
func TestOverlayAppearsOnLongLineWithoutPTY(t *testing.T) {
	// Setup a minimal TexelTerm without PTY
	term := &TexelTerm{width: 10, height: 6}
	v := parser.NewVTerm(10, 6)
	p := parser.NewParser(v)

	// Type a long command to push caret beyond width
	s := strings.Repeat("x", 20)
	for _, r := range s {
		p.Parse(r)
	}

	term.vterm = v
	term.overlayEnabled = true

	// Base render of term (no overlay here)
	base := term.Render()
	if base == nil || len(base) == 0 {
		t.Fatalf("expected render buffer, got nil/empty")
	}
	// Now apply the editor card overlay
	card := newLongLineEditorCard(term)
	card.Resize(10, 6)
	buf := card.Render(base)
	if !card.active {
		t.Fatalf("expected overlay to be active for long line, got inactive")
	}

	// Check that overlay rectangle lies within buffer and uses overlay background color
	rect := card.rect
	if rect.Y < 0 || rect.Y+rect.H > len(buf) || rect.X < 0 || rect.X+rect.W > len(buf[0]) {
		t.Fatalf("overlay rect out of bounds: %+v for buffer %dx%d", rect, len(buf[0]), len(buf))
	}

	// Verify overlay drew the long line text at overlay row (distinct from prompt row)
	overlayTextRunes := 0
	maxCheck := min(len(s), rect.W)
	for i := 0; i < maxCheck; i++ {
		if buf[rect.Y][rect.X+i].Ch == 'x' {
			overlayTextRunes++
		}
	}
	if overlayTextRunes == 0 {
		t.Fatalf("overlay did not draw expected text in overlay row")
	}

	// Optional: ensure overlay background differs from the default surface in at least one cell
	// This helps catch regressions where overlay might be invisible.
	tm := theme.Get()
	wantBG := tm.GetColor("texelterm", "longline_overlay_bg", tcell.NewRGBColor(56, 58, 70))
	diffFound := false
	for x := rect.X; x < rect.X+min(3, rect.W); x++ {
		_, bg, _ := buf[rect.Y][x].Style.Decompose()
		if bg.TrueColor() == wantBG.TrueColor() {
			diffFound = true
			break
		}
	}
	if !diffFound {
		t.Logf("warning: overlay bg did not match theme; overlay text confirmed")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
