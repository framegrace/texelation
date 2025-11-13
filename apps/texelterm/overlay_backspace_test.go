package texelterm

import (
    "testing"

    "github.com/gdamore/tcell/v2"
    "texelation/apps/texelterm/parser"
)

// TestOverlayBackspaceShrinksLineDuringCapture verifies Backspace updates the TextArea
// while overlay is capturing (caret beyond width).
func TestOverlayBackspaceShrinksLineDuringCapture(t *testing.T) {
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
    base := term.Render()
    card := newLongLineEditorCard(term)
    card.Resize(10, 4)
    _ = card.Render(base)
    if !card.shouldCapture() {
        t.Fatalf("expected capture")
    }

    // Confirm initial text
    before := card.ta.Lines[0]
    // Backspace at end should remove last rune and move caret left
    card.interceptKey(tcell.NewEventKey(tcell.KeyBackspace2, 0, 0))
    after := card.ta.Lines[0]
    if len([]rune(after)) != len([]rune(before))-1 {
        t.Fatalf("backspace did not shrink: before %q, after %q", before, after)
    }
}
