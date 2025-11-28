# Bracketed Paste Mode Integration Guide

## What is Bracketed Paste Mode?

Bracketed paste mode (DECSET 2004) allows terminals to distinguish between typed and pasted text by wrapping pasted content in special escape sequences:
- Start marker: `ESC[200~`
- End marker: `ESC[201~`

This prevents auto-indent issues in editors like vim/neovim when pasting code.

## Implementation Status

✅ **Parser support complete** - texelterm now correctly handles:
- `CSI ? 2004 h` - Enable bracketed paste mode
- `CSI ? 2004 l` - Disable bracketed paste mode
- Mode state tracking
- Reset behavior (DECSTR, RIS both disable it)

## Terminal App Integration

To integrate bracketed paste mode in your terminal application (e.g., `apps/texelterm/term.go`):

### 1. Set up the callback

When creating your VTerm instance, register a callback:

```go
vterm := parser.NewVTerm(width, height)

// Track when apps enable/disable bracketed paste mode
vterm.OnBracketedPasteModeChange = func(enabled bool) {
    if enabled {
        log.Println("Application enabled bracketed paste mode")
        // Set a flag to wrap future pastes
    } else {
        log.Println("Application disabled bracketed paste mode")
        // Clear the flag
    }
}
```

### 2. Detect paste operations

You need to detect when the user is pasting vs. typing. This depends on your platform:

**For tcell (cross-platform):**
```go
// tcell provides PasteStart/PasteEnd events
case *tcell.EventPaste:
    if ev.Start() {
        // Paste starting
        if vterm.IsBracketedPasteModeEnabled() {
            // Send start marker
            parser.Parse([]rune("\x1b[200~"))
        }
    } else {
        // Paste ending
        if vterm.IsBracketedPasteModeEnabled() {
            // Send end marker
            parser.Parse([]rune("\x1b[201~"))
        }
    }
```

**For PTY-based terminals:**
- Check clipboard change rate
- Use OS-specific paste detection
- Or provide explicit paste command (Ctrl+Shift+V)

### 3. Example complete integration

```go
type TerminalApp struct {
    vterm             *parser.VTerm
    parser            *parser.Parser
    bracketedPasteOn  bool
}

func (t *TerminalApp) setupVTerm(width, height int) {
    t.vterm = parser.NewVTerm(width, height)
    t.parser = parser.NewParser(t.vterm)

    // Track bracketed paste mode state
    t.vterm.OnBracketedPasteModeChange = func(enabled bool) {
        t.bracketedPasteOn = enabled
    }
}

func (t *TerminalApp) handlePaste(text string) {
    if t.bracketedPasteOn {
        // Wrap paste in markers
        wrapped := "\x1b[200~" + text + "\x1b[201~"
        for _, r := range wrapped {
            t.parser.Parse(r)
        }
    } else {
        // Send as-is
        for _, r := range text {
            t.parser.Parse(r)
        }
    }
}
```

## Testing

Test with neovim:

1. Start neovim in texelterm
2. Enter insert mode: `i`
3. Paste some indented code
4. **Expected:** Code maintains original formatting (no auto-indent)
5. **Without bracketed paste:** Each line gets progressively more indented

## How Applications Use It

When neovim (or similar) detects bracketed paste support:

1. Enables mode on startup: `CSI ? 2004 h`
2. Watches for `ESC[200~` (paste start)
3. Disables auto-indent temporarily
4. Collects all input until `ESC[201~` (paste end)
5. Re-enables auto-indent

## Debug

To verify it's working:

```go
// Add debug logging
vterm.OnBracketedPasteModeChange = func(enabled bool) {
    log.Printf("Bracketed paste mode: %v", enabled)
}

// Check mode state
if vterm.IsBracketedPasteModeEnabled() {
    log.Println("Mode is currently enabled")
}
```

## References

- [Bracketed Paste Blog Post](https://cirw.in/blog/bracketed-paste)
- [xterm Control Sequences](https://invisible-island.net/xterm/ctlseqs/ctlseqs.html)
- Test file: `apps/texelterm/esctest/bracketed_paste_test.go`
