package texelterm

import (
	"strings"
	"testing"

	"texelation/apps/texelterm/parser"
)

func TestAltScreenSelection(t *testing.T) {
	// 1. Setup
	v := parser.NewVTerm(80, 24)
	app := &TexelTerm{
		vterm: v,
	}
	p := parser.NewParser(v)

	// Helper to write string
	writeString := func(s string) {
		for _, r := range s {
			p.Parse(r)
		}
	}

	// 2. Write to Main Screen
	writeString("Main Screen Content")

	// 3. Switch to Alt Screen (CSI ? 1049 h)
	writeString("\x1b[?1049h")

	// 4. Write to Alt Screen
	// Clear screen first (optional, usually 1049 does it, but let's be sure)
	// writeString("\x1b[2J\x1b[H")
	writeString("Alt Screen Content")

	// 5. Select "Alt Screen Content"
	// "Alt Screen Content" is at 0,0 (length 18)
	// Selection logic uses anchorLine/Col and currentLine/Col
	// In Alt Screen, visible top is 0. So lines are 0-based.
	app.selection.active = true
	app.selection.anchorLine = 0
	app.selection.anchorCol = 0
	app.selection.currentLine = 0
	app.selection.currentCol = 17 // Inclusive of last char?
	// selectionRangeLocked logic: returns endCol + 1.
	// If I set currentCol=17, range will be 0..18.
	// "Alt Screen Content" is 18 chars. Indices 0..17.

	// 6. Verify Selection Text
	text := app.buildSelectionTextLocked()

	if !strings.Contains(text, "Alt Screen") {
		t.Errorf("Expected selection to contain 'Alt Screen', got %q", text)
	}
	if strings.Contains(text, "Main Screen") {
		t.Errorf("Selection contained content from Main Screen: %q", text)
	}
}
