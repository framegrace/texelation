package texelterm

import (
    "testing"

    "github.com/gdamore/tcell/v2"
    "texelation/apps/texelterm/parser"
)

// TestOverlayInsertMismatchBeyondViewport reproduces a sync mismatch when editing a long
// line while the overlay is capturing: TextArea performs insertion while vterm (by default)
// replaces at the cursor. After authority returns to the terminal (caret back inside width),
// the overlay mirrors vterm and the text differs from what was seen during capture.
func TestOverlayInsertMismatchBeyondViewport(t *testing.T) {
    // Terminal width where long line exceeds viewport.
    term := &TexelTerm{width: 10, height: 4}
    v := parser.NewVTerm(10, 4)
    p := parser.NewParser(v)
    term.vterm = v
    term.inputStartKnown = true
    term.inputStartCol = 0

    // Seed a long command (len=11, cursorX=11 >= cols) to trigger capture mode.
    for _, r := range []rune("abcdefghijk") {
        p.Parse(r)
    }

    // Base render buffer from the app.
    base := term.Render()
    if base == nil || len(base) == 0 {
        t.Fatalf("expected base buffer, got nil/empty")
    }

    // Build overlay editor card and confirm it wants to capture.
    card := newLongLineEditorCard(term)
    card.Resize(10, 4)
    _ = card.Render(base)
    if !card.shouldCapture() {
        t.Fatalf("expected overlay to capture (caret past width)")
    }

    // Move caret left by one inside the overlay (still >= cols, so still capturing).
    // Mirror that movement in vterm (as PTY/host would) so both caret positions align.
    card.interceptKey(tcell.NewEventKey(tcell.KeyLeft, 0, 0))
    _, y := v.Cursor()
    v.SetCursorPos(y, 10) // caret at x=10, before final 'k'

    // Enable terminal insert mode to mimic typical shell line editing behaviour.
    // CSI ? 4 h  -> set insert mode
    for _, r := range []rune{'\x1b', '[', '?', '4', 'h'} {
        p.Parse(r)
    }

    // Insert 'X' at x=10 via overlay; capture the overlay's view of the line.
    card.interceptKey(tcell.NewEventKey(tcell.KeyRune, 'X', 0))
    if card.ta == nil {
        t.Fatalf("overlay TextArea not initialized")
    }
    overlayDuringCapture := card.ta.Lines[0]

    // Simulate PTY echo into vterm (now in insert mode, so it shifts tail).
    p.Parse('X')

    // Move caret back inside viewport in vterm so authority returns to terminal.
    v.SetCursorPos(y, 9)

    // Re-render; overlay should mirror vterm now (no capture).
    base2 := term.Render()
    _ = card.Render(base2)
    terminalMirrored := card.ta.Lines[0]

    // Expect equality: overlay uses replace semantics while capturing, matching vterm.
    if overlayDuringCapture != terminalMirrored {
        t.Fatalf("expected overlay to match vterm after returning to terminal authority; overlay %q vs vterm %q", overlayDuringCapture, terminalMirrored)
    }
}
