# Terminal Persistence Architecture

This document describes how texelterm persists terminal state across resizes, shell restarts, and server restarts.

## Overview

Terminal persistence in texelation covers three main areas:

1. **Scrollback History** - Terminal output preserved across resizes with proper reflow
2. **Shell Environment** - Environment variables, working directory preserved across shell restarts
3. **Command History** - Per-terminal bash history isolation

```
┌─────────────────────────────────────────────────────────────────┐
│                     TERMINAL PERSISTENCE                        │
├─────────────────────┬─────────────────────┬─────────────────────┤
│  Scrollback History │  Shell Environment  │  Command History    │
│  (Three-level arch) │  (File-based)       │  (Per-pane files)   │
├─────────────────────┼─────────────────────┼─────────────────────┤
│  DiskHistory        │  ~/.texel-env.$$    │  ~/.texel-history-  │
│  ScrollbackHistory  │                     │  <pane-id>          │
│  DisplayBuffer      │                     │                     │
└─────────────────────┴─────────────────────┴─────────────────────┘
```

---

## 1. Scrollback History (Three-Level Architecture)

**Status**: Complete (2025-12-12)

The scrollback system separates storage from display for efficient reflow on resize, inspired by SNES tile scrolling.

### Architecture

```
┌─────────────────────────────────────────┐
│              DISK HISTORY               │
│   (TXHIST02 format - O(1) random access)│
│   Unlimited logical lines on disk       │
└─────────────────────────────────────────┘
                    │
                    │ Load/Unload on demand
                    ▼
┌─────────────────────────────────────────┐
│         SCROLLBACK HISTORY              │
│   (~5000 logical lines in memory)       │
│   Sliding window with global indices    │
└─────────────────────────────────────────┘
                    │
                    │ Wrap to current width
                    ▼
┌─────────────────────────────────────────┐
│            DISPLAY BUFFER               │
│   (Physical lines - current width)      │
│   ┌─────────────────────────────────┐   │
│   │     Off-screen ABOVE (~200)     │   │
│   ├─────────────────────────────────┤   │
│   │     VISIBLE VIEWPORT            │   │
│   ├─────────────────────────────────┤   │
│   │     Off-screen BELOW (~50)      │   │
│   └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
```

### Key Properties

| Level | Width-dependent | Resize cost | Size | Persistence |
|-------|-----------------|-------------|------|-------------|
| Disk History | No | None | Unlimited | Yes |
| Scrollback History | No | None | ~5000 lines | No (runtime) |
| Display Buffer | Yes | O(viewport) | ~500 lines | No |

### Data Structures

**LogicalLine** (`parser/logical_line.go`) - Width-independent line storage:
```go
type LogicalLine struct {
    Cells []Cell  // Full unwrapped content, any length
}
func (l *LogicalLine) WrapToWidth(width int) []PhysicalLine
```

**ScrollbackHistory** (`parser/scrollback_history.go`) - In-memory sliding window:
```go
type ScrollbackHistory struct {
    lines       []*LogicalLine
    startIndex  int64  // Global index of first line in memory
    disk        *DiskHistory
}
```

**DisplayBuffer** (`parser/display_buffer.go`) - Physical lines at current width:
```go
type DisplayBuffer struct {
    lines              []*PhysicalLine
    currentLine        *LogicalLine
    width, height      int
    viewportOffset     int
    atLiveEdge         bool
}
```

**DiskHistory** (`parser/disk_history.go`) - TXHIST02 indexed format:
```go
type DiskHistory struct {
    file    *os.File
    offsets []int64  // Line offset index for O(1) access
}
```

### Usage

```go
// Enable disk-backed display buffer
err := v.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
    MaxMemoryLines: 5000,
    MarginAbove:    200,
    MarginBelow:    50,
})
defer v.CloseDisplayBuffer()
```

### Configuration

Enable in `~/.config/texelation/theme.json`:
```json
{
  "texelterm": {
    "display_buffer_enabled": true
  }
}
```

### Performance

Benchmarks on AMD Ryzen 9 3950X:
- **PlaceChar**: ~563ns/op
- **Resize**: ~146µs/op with 1000 lines (O(viewport) not O(history))
- **Scroll**: ~8.6ns/op, 0 allocations

### Files

- `apps/texelterm/parser/logical_line.go` - LogicalLine with wrapping
- `apps/texelterm/parser/scrollback_history.go` - Memory window with disk backing
- `apps/texelterm/parser/display_buffer.go` - Physical line viewport
- `apps/texelterm/parser/disk_history.go` - TXHIST02 indexed format
- `apps/texelterm/parser/vterm_display_buffer.go` - VTerm integration layer

---

## 2. Shell Environment Persistence

**Status**: Shell restart working, server restart planned

### Current Implementation (Shell Restart)

When a user declines to exit a shell (presses 'n' on exit confirmation), the environment is restored from a temporary file.

**Components:**

1. **Shell Integration** (`~/.config/texelation/shell-integration/bash.sh`)
   - Runs after each command via `PROMPT_COMMAND`
   - Writes environment to `~/.texel-env.$$` using `env >| ~/.texel-env.$$`
   - Uses `>|` to bypass noclobber protection

2. **Terminal App** (`apps/texelterm/term.go:runShell()`)
   - On shell restart, reads `~/.texel-env.<old-pid>`
   - Filters out `BASH_FUNC_*` entries (cause import errors)
   - Passes environment to new shell via `cmd.Env`
   - Deletes the temporary file after reading

3. **OSC 133 Integration**
   - Tracks command boundaries: A=prompt start, B=prompt end, C=command start, D=command end
   - Environment is captured after D (command end) marker

### Why File-Based?

DCS (Device Control String) sequences failed due to bash limitations - base64-encoded environment (8KB+) written to stdout causes `PROMPT_COMMAND` to hang. File-based approach:
- `env >| file` completes instantly (no stdout writes)
- No prompt hangs or delays
- Works reliably with noclobber

### Future: Server Restart Persistence

Environment will be integrated into the terminal snapshot system:

```
1. Shell writes: env → ~/.texel-env.$$
2. Terminal periodically reads file → internal state
3. Snapshot system: terminal state (with env) → disk
4. Server restart: snapshot → restore terminal + environment
```

### Files

- `~/.config/texelation/shell-integration/bash.sh` - Environment capture
- `apps/texelterm/term.go` - Environment restoration (runShell)
- `apps/texelterm/parser/parser.go` - OSC 133 integration

---

## 3. Per-Terminal Command History

**Status**: Complete

Each terminal panel maintains independent bash command history.

### Implementation

- Server passes pane ID to app via `PaneIDSetter` interface
- TexelTerm stores pane ID and sets `TEXEL_PANE_ID` environment variable
- Shell integration reads `$TEXEL_PANE_ID` and sets `HISTFILE=~/.texel-history-$TEXEL_PANE_ID`
- Each panel gets isolated history: `~/.texel-history-<pane-id-hex>`

### Benefits

- Multiple bash shells run simultaneously with independent histories
- No "last shell wins" problem when exiting shells
- History persists across shell restart
- Each panel's history preserved separately

---

## 4. Future Enhancements

### Privacy Mode (Planned)

OSC sequences to control persistence:
- `OSC 1337;HistoryPrivacy=on` - Stop persisting new lines
- `OSC 1337;HistoryPrivacy=off` - Resume persisting
- Useful for commands with sensitive data (passwords, tokens)

### Redaction Patterns (Planned)

Regex patterns to automatically redact sensitive data:
```json
{
  "texelterm": {
    "history": {
      "privacy": {
        "redact_patterns": [
          "password\\s*=\\s*\\S+",
          "token\\s*=\\s*\\S+"
        ]
      }
    }
  }
}
```

### Encryption (Planned)

- ChaCha20-Poly1305 or AES-256-GCM for history files
- Key derivation from user password (Argon2)
- System keyring integration for key storage
- Per-session random salt/nonce

### Session Metadata (Planned)

```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "start_time": "2025-12-04T10:30:00Z",
  "command": "/bin/bash",
  "working_dir": "/home/user/projects",
  "hostname": "workstation",
  "line_count": 15234,
  "encrypted": true
}
```

### History Management App (Planned)

- Search, browse, replay terminal sessions
- Full-text search with encrypted content
- Configurable retention policies
- Export/import functionality

---

## Testing Status

### Implemented
- Scrollback reflow on resize
- Environment preserved across shell restart
- Per-terminal history isolation
- Fedora noclobber compatibility
- No prompt hangs or delays

### Future Testing
- Server restart with multiple terminals
- Large environments (1000+ variables)
- Concurrent terminal startup from snapshots
