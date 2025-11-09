package texelterm

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"texelation/apps/texelterm/parser"
)

// Test that an insertion made while overlay is authoritative ends up at the
// same caret position in both overlay TA and vterm when switching back to
// terminal authority (caret inside viewport). This simulates the PTY echo by
// applying the same rune to vterm via the parser.
func TestOverlayInsertSyncConsistency(t *testing.T) {
	// Setup minimal terminal and vterm
	term := &TexelTerm{width: 10, height: 4}
	v := parser.NewVTerm(10, 4)
	p := parser.NewParser(v)
	term.vterm = v

	// Type a long command to push caret beyond width
	for _, r := range []rune("abcdefghijk") { // len=11, caretX=11
		p.Parse(r)
	}

	// Render base app buffer
	base := term.Render()
	if base == nil || len(base) == 0 {
		t.Fatalf("expected base buffer, got nil/empty")
	}

	// Build overlay card and confirm capture
	card := newLongLineEditorCard(term)
	card.Resize(10, 4)
	buf := card.Render(base)
	if buf == nil {
		t.Fatalf("nil buffer from overlay render")
	}
	if !card.shouldCapture() {
		t.Fatalf("expected overlay to capture (caret past width)")
	}

	// Insert 'X' while overlay is authoritative
	ev := tcell.NewEventKey(tcell.KeyRune, 'X', 0)
	card.interceptKey(ev) // updates overlay TA and (at runtime) forwards to terminal
	// Simulate PTY echo into vterm
	p.Parse('X')

	// Move caret back inside viewport in vterm so authority returns to terminal
	// Move to x=9
	_, y := v.Cursor()
	v.SetCursorPos(y, 9)

	// Render base + overlay again
	base2 := term.Render()
	buf2 := card.Render(base2)
	if buf2 == nil {
		t.Fatalf("nil buffer after re-render")
	}

	// Verify overlay is not capturing now (caret back inside width)
	if card.shouldCapture() {
		t.Fatalf("overlay still capturing after caret returned inside width")
	}
	// Overlay should be drawn (line still long) but blurred and mirroring vterm
	if card.ta == nil {
		t.Fatalf("overlay TA is nil")
	}
	// Extract vterm line text
	top := v.VisibleTop()
	cells := v.HistoryLineCopy(top + y)
	vtext := string(cellsToRunes(cells))
	if got := card.ta.Lines[0]; got != vtext {
		t.Fatalf("overlay text mismatch: got %q, want %q", got, vtext)
	}
	if card.ta.CaretX != 9 {
		t.Fatalf("overlay caretX=%d, want 9", card.ta.CaretX)
	}
}
