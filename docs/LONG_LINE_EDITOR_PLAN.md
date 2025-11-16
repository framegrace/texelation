# Long Line Editor for TexelTerm - Implementation Plan

## Overview

A card-based overlay editor that provides better editing experience for long command lines in texelterm. When lines exceed terminal width or user manually triggers it, an overlay TextArea opens for comfortable multi-line editing.

## Problem Statement

- Current terminal doesn't reflow; long lines extend beyond visible space and get hidden
- Editing long commands at the shell prompt is difficult
- Need better editing while maintaining compatibility with shell features (history, tab completion, etc.)

## Solution Architecture

**Core Concept**: Unidirectional buffering with smart passthrough
- Overlay textarea buffers input locally
- On "commit triggers", flush buffer to PTY and close overlay
- Use shell integration (OSC 133) to detect prompt boundaries
- Let shell remain authoritative; overlay is temporary buffer only

## Implementation Phases

### Phase 1: Shell Integration (OSC 133)
**Status**: Not Started

Add support for OSC 133 sequences to detect prompt boundaries:
- `OSC 133 ; A ST` - Prompt start
- `OSC 133 ; B ST` - Prompt end / Input start
- `OSC 133 ; C ST` - Input end / Command start
- `OSC 133 ; D [; exitcode] ST` - Command end

**Tasks**:
1. Extend `apps/texelterm/parser/parser.go` handleOSC() to recognize OSC 133 A/B/C/D
2. Add fields to `parser.VTerm` to track prompt state:
   - `promptActive` (bool) - are we at a prompt?
   - `inputStartLine`, `inputStartCol` (int) - where input region begins
   - `commandActive` (bool) - is a command currently executing?
3. Add callbacks: `PromptStart()`, `InputStart()`, `CommandStart()`, `CommandEnd(exitCode int)`
4. Wire callbacks to texelterm to enable/disable overlay triggering

**Deliverable**: VTerm can detect when shell is ready for input vs executing commands

---

### Phase 2: LongLineEditor Card (MVP)
**Status**: Not Started

Create a card that wraps TexelUI's TextArea for overlay editing.

**File**: `apps/texelterm/longeditor/editor_card.go`

**Features**:
- Manual toggle with Ctrl+O
- TextArea widget from TexelUI for multi-line editing
- Simple commit on Enter: send accumulated text to PTY
- Cancel on Escape: discard changes, close overlay
- Passthrough for Ctrl+C, Ctrl+D, Ctrl+Z, Ctrl+\\

**State Machine**:
```
Inactive (overlay hidden)
  ├─> Editing (overlay visible, capturing keys)
  └─> (back to Inactive on commit/cancel)
```

**Implementation Details**:

```go
package longeditor

import (
    "github.com/gdamore/tcell/v2"
    "texelation/texel"
    "texelation/texel/cards"
    "texelation/texelui/core"
    "texelation/texelui/widgets"
)

type EditorCard struct {
    active      bool
    ui          *core.UIManager
    textarea    *widgets.TextArea
    width       int
    height      int
    onCommit    func(text string) // callback to send text to PTY
    onCancel    func()
    refreshChan chan<- bool
}

// Card interface implementation
func (e *EditorCard) Run() error
func (e *EditorCard) Stop()
func (e *EditorCard) Resize(cols, rows int)
func (e *EditorCard) Render(input [][]texel.Cell) [][]texel.Cell
func (e *EditorCard) HandleKey(ev *tcell.EventKey)
func (e *EditorCard) SetRefreshNotifier(refreshChan chan<- bool)
func (e *EditorCard) HandleMessage(msg texel.Message)

// Control interface
func (e *EditorCard) Toggle()
func (e *EditorCard) Open(initialText string)
func (e *EditorCard) Close()
```

**Key Routing Logic**:
- When `active == false`: pass all keys through unchanged
- When `active == true`:
  - Ctrl+O → Toggle (close overlay)
  - Enter → Commit (call onCommit with textarea content, close)
  - Escape → Cancel (call onCancel, close)
  - Ctrl+C, Ctrl+D, Ctrl+Z, Ctrl+\\ → Passthrough (commit + send key, close)
  - All other keys → Route to textarea for editing

**Rendering Logic**:
- When inactive: return input buffer unchanged (transparent)
- When active:
  - Render input buffer as base
  - Render UIManager on top (TextArea with border)
  - Position: centered or at prompt location (TBD)

**Tasks**:
1. Create `apps/texelterm/longeditor/` directory
2. Implement EditorCard struct and Card interface
3. Integrate TexelUI UIManager with TextArea widget
4. Implement state machine and key routing
5. Add unit tests for state transitions

**Deliverable**: Working card that can be toggled with Ctrl+O, provides editing, commits on Enter

---

### Phase 3: Integration with TexelTerm
**Status**: Not Started

Wire LongLineEditor card into texelterm pipeline.

**Tasks**:
1. Modify `apps/texelterm/term.go` New() function:
   - Create LongLineEditor card
   - Add to pipeline after terminal card
   - Wire onCommit callback to send text to PTY
2. Connect shell integration callbacks:
   - When InputStart detected, make overlay available
   - When CommandStart detected, disable overlay
3. Add configuration to theme.json:
   ```json
   "texelterm": {
     "long_line_editor_enabled": true,
     "long_line_editor_auto_open": false,
     "long_line_editor_width_threshold": 80
   }
   ```
4. Update texelterm tests to verify card integration

**Deliverable**: texelterm with working Ctrl+O toggle for overlay editing

---

### Phase 4: Auto-Open and Enhanced Passthrough
**Status**: Not Started (Future)

Automatic opening when line exceeds width + better shell interaction.

**Features**:
- Auto-open overlay when typing exceeds terminal width (configurable)
- Arrow-up/down at boundary: flush → passthrough → detect shell response → reopen if long
- Tab completion passthrough support
- Better handling of shell state changes

**State Machine Extended**:
```
Inactive
  ├─> Editing (manual Ctrl+O or auto on width threshold)
  └─> WaitingForShell (after passthrough, monitoring PTY for response)
        └─> back to Editing (if shell returns long line) or Inactive
```

**Challenges**:
- Detecting when shell has finished responding to arrow-up
- Capturing the new line content from PTY output
- Handling edge cases (shell crashes, unexpected output)

**Deliverable**: More seamless integration with shell history and completion

---

### Phase 5: Advanced Features
**Status**: Not Started (Future)

Long-term enhancements for production use.

**Features**:
- Ctrl+R reverse search passthrough
- Syntax highlighting in overlay (bash/zsh syntax)
- Multi-line command handling (backslash continuations)
- History viewing mode (read-only overlay for scrolling through long history lines)
- Configurable positioning (centered vs in-place at prompt)
- Mouse support in overlay

---

## Technical Architecture

### Card Pipeline Flow
```
User Input
  ↓
LongLineEditor Card (overlay)
  ├─ If active: capture keys, edit in textarea
  ├─ On commit: send to PTY via TerminalCard
  └─ If inactive: pass through
  ↓
TerminalCard (VTerm + PTY)
  ↓
Buffer Output
```

### Shell Integration Setup

Users need to configure their shell. Examples:

**Bash** (add to `.bashrc`):
```bash
# OSC 133 shell integration for Texelation
if [[ "$TERM" == "xterm-256color" ]] || [[ -n "$TEXELATION" ]]; then
    PS1='\[\e]133;A\a\]'$PS1'\[\e]133;B\a\]'

    __texel_prompt_command() {
        local exit_code=$?
        echo -ne "\e]133;D;${exit_code}\a"
        echo -ne "\e]133;A\a"
    }

    PROMPT_COMMAND='__texel_prompt_command'
    trap 'echo -ne "\e]133;C\a"' DEBUG
fi
```

**Zsh** (add to `.zshrc`):
```zsh
# OSC 133 shell integration for Texelation
if [[ "$TERM" == "xterm-256color" ]] || [[ -n "$TEXELATION" ]]; then
    precmd() { print -Pn "\e]133;A\a" }
    precmd_functions+=( precmd )
    preexec() { print -n "\e]133;C\a" }
    preexec_functions+=( preexec )
    PS1=$'%{\e]133;A\a%}'$PS1$'%{\e]133;B\a%}'
fi
```

**Fish** (add to `config.fish`):
```fish
# OSC 133 shell integration for Texelation
if test "$TERM" = "xterm-256color"; or set -q TEXELATION
    function fish_prompt
        echo -en "\e]133;A\a"
        # your prompt here
        echo -en "\e]133;B\a"
    end
end
```

---

## Testing Strategy

### Unit Tests
- OSC 133 parsing (parser_test.go)
- EditorCard state machine (editor_card_test.go)
- Key routing logic (editor_card_test.go)

### Integration Tests
- Full pipeline with LongLineEditor card
- Shell integration with mock PTY
- Auto-open threshold behavior

### Manual Testing
- Test with bash, zsh, fish
- Test history navigation (arrow-up/down)
- Test tab completion
- Test with various terminal widths
- Test with very long lines (1000+ chars)

---

## Configuration

Theme.json additions:
```json
{
  "texelterm": {
    "long_line_editor_enabled": true,
    "long_line_editor_auto_open": false,
    "long_line_editor_width_threshold": 80,
    "long_line_editor_toggle_key": "Ctrl+O"
  },
  "ui": {
    "overlay_bg": "#1e1e1e",
    "overlay_border": "#4a4a4a"
  }
}
```

---

## Open Questions & Risks

### Questions
1. **Positioning**: Center overlay or place at prompt location?
   - **Decision**: Start with centered (simpler), add in-place option later with shell integration
2. **Width**: Fixed width or responsive?
   - **Decision**: Responsive, fill most of screen (e.g., 90% width, 70% height)
3. **Tab handling**: Should Tab work in overlay or pass through to shell?
   - **Decision**: Initially pass through (for completion), add Tab-in-editor option later

### Risks
1. **Shell compatibility**: Not all shells support OSC 133 easily
   - **Mitigation**: Provide clear setup docs, fallback to manual toggle only
2. **Race conditions**: Shell might update line while overlay is open
   - **Mitigation**: Keep overlay ephemeral, always trust shell state on commit
3. **Complex shell modes**: Ctrl+R, vi mode, etc. may break assumptions
   - **Mitigation**: Start simple (MVP), iterate based on user feedback
4. **Performance**: Very long lines (10k+ chars) might be slow
   - **Mitigation**: Test and optimize TextArea if needed

---

## Success Criteria

### MVP (Phase 1-3)
- [x] OSC 133 sequences recognized and tracked
- [x] Ctrl+O toggles overlay editor
- [x] Can edit text in overlay with full TextArea features
- [x] Enter commits text to shell
- [x] Escape cancels and closes overlay
- [x] Basic passthrough keys work (Ctrl+C, Ctrl+D)
- [x] Integration tests pass
- [x] Works with bash when shell integration is configured

### Phase 4
- [ ] Auto-opens when line exceeds width
- [ ] Arrow-up/down passthrough works correctly
- [ ] Tab completion passthrough works

### Phase 5
- [ ] Ctrl+R reverse search supported
- [ ] Syntax highlighting in overlay
- [ ] History viewing mode functional

---

## Timeline Estimate

- **Phase 1** (OSC 133): 2-4 hours
- **Phase 2** (EditorCard MVP): 4-6 hours
- **Phase 3** (Integration): 2-3 hours
- **Testing & Polish**: 2-3 hours

**Total MVP**: ~10-16 hours

---

## Notes

- Keep overlay as **temporary buffer**, not synchronized view
- Always **trust shell** as authoritative source
- Commit frequently to avoid state divergence
- Focus on **common case** (simple line editing), handle edge cases later
- User must configure shell for best experience

---

## Related Documentation

- `docs/TEXEL_APP_GUIDE.md` - How to build apps with cards
- `docs/CLIENT_SERVER_ARCHITECTURE.md` - Desktop and app lifecycle
- `apps/texelterm/parser/parser.go` - VT sequence parsing
- `texelui/widgets/textarea.go` - TextArea widget implementation
