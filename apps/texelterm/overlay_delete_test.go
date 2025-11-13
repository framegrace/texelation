package texelterm

import (
    "testing"

    "github.com/gdamore/tcell/v2"
    "texelation/apps/texelterm/parser"
)

// TestOverlayDeleteShrinksLineDuringCapture verifies Delete updates the TextArea
// while overlay is capturing (caret beyond width): it removes the rune at caret.
func TestOverlayDeleteShrinksLineDuringCapture(t *testing.T) {
    term := &TexelTerm{width: 10, height: 4}
    v := parser.NewVTerm(10, 4)
    p := parser.NewParser(v)
    term.vterm = v
    term.inputStartKnown = true
    term.inputStartCol = 0

    // Seed long line to force capture
    for _, r := range []rune("abcdefghijk") { // len=11
        p.Parse(r)
    }

    // Move caret left by one so Delete removes 'k'
    v.SetCursorPos(0, 10)

    base := term.Render()
    card := newLongLineEditorCard(term)
    card.Resize(10, 4)
    _ = card.Render(base)
    if !card.shouldCapture() {
        t.Fatalf("expected capture")
    }

    before := card.ta.Lines[0]
    // Delete at caret should remove the last rune
    card.interceptKey(tcell.NewEventKey(tcell.KeyDelete, 0, 0))
    after := card.ta.Lines[0]
    if len([]rune(after)) != len([]rune(before))-1 {
        t.Fatalf("delete did not shrink: before %q, after %q", before, after)
    }
}
