# Terminal Persistence Architecture

This document describes how texelterm persists terminal state across resizes, shell restarts, and server restarts.

## Overview

Terminal persistence in texelation covers three main areas:

1. **Scrollback History** - Terminal output preserved across resizes with proper reflow
2. **Shell Environment** - Environment variables, working directory preserved across shell AND server restarts
3. **Command History** - Per-terminal bash history isolation

```
┌─────────────────────────────────────────────────────────────────┐
│                     TERMINAL PERSISTENCE                        │
├─────────────────────┬─────────────────────┬─────────────────────┤
│  Scrollback History │  Shell Environment  │  Command History    │
│  (Three-level arch) │  (Pane-ID files)    │  (Per-pane files)   │
├─────────────────────┼─────────────────────┼─────────────────────┤
│  DiskHistory        │  ~/.texel-env-      │  ~/.texel-history-  │
│  ScrollbackHistory  │  <pane-id>          │  <pane-id>          │
│  DisplayBuffer      │                     │                     │
└─────────────────────┴─────────────────────┴─────────────────────┘
```

All persistence is keyed by **pane ID** (stable UUID), enabling seamless restoration across shell restarts AND server restarts.

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

**Status**: Complete (works across shell restarts AND server restarts)

Environment variables and working directory are preserved using pane-ID-based files that survive both shell restarts and server restarts.

### How It Works

```
┌─────────────────────────────────────────────────────────────────┐
│  Shell Integration (PROMPT_COMMAND)                             │
│  After each command, writes to ~/.texel-env-<pane-id>:          │
│    - All environment variables (VAR=value format)               │
│    - Working directory as __TEXEL_CWD=/path/to/dir              │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Terminal App (runShell)                                        │
│  On shell start (restart OR server restart):                    │
│    1. Read ~/.texel-env-<pane-id>                               │
│    2. Parse environment variables                               │
│    3. Extract __TEXEL_CWD for working directory                 │
│    4. Filter out BASH_FUNC_* (cause import errors)              │
│    5. Start shell with cmd.Env and cmd.Dir                      │
└─────────────────────────────────────────────────────────────────┘
```

### Key Implementation Details

**Pane ID Stability**: Pane IDs are UUIDs assigned when a pane is created and restored from snapshots on server restart. This makes environment files persist across server restarts.

**Environment File Format** (`~/.texel-env-<pane-id-hex>`):
```bash
HOME=/home/user
PATH=/usr/bin:/bin
MY_VAR=some_value
__TEXEL_CWD=/home/user/projects/myproject
```

**Shell Integration** (`~/.config/texelation/shell-integration/bash.sh`):
- Runs after each command via `PROMPT_COMMAND`
- Writes environment using `env >| ~/.texel-env-$TEXEL_PANE_ID`
- Appends `__TEXEL_CWD=$(pwd)` for working directory
- Uses `>|` to bypass noclobber protection

**Terminal Restoration** (`apps/texelterm/term.go:runShell()`):
```go
// Read environment from pane-ID-based file
envFile := filepath.Join(homeDir, fmt.Sprintf(".texel-env-%s", paneID))
if data, err := os.ReadFile(envFile); err == nil {
    for _, line := range strings.Split(string(data), "\n") {
        if strings.HasPrefix(line, "__TEXEL_CWD=") {
            cwd = strings.TrimPrefix(line, "__TEXEL_CWD=")
            continue
        }
        if strings.HasPrefix(line, "BASH_FUNC_") {
            continue // Skip bash functions
        }
        env = append(env, line)
    }
}
cmd.Env = env
cmd.Dir = cwd  // Restore working directory
```

### Why File-Based?

DCS (Device Control String) sequences were attempted first but failed:
- Base64-encoded environment is 8KB+ of data
- Bash's `PROMPT_COMMAND` hangs waiting for stdout writes
- Even background jobs with stdout connected cause hangs

File-based approach advantages:
- `env >| file` completes instantly (no stdout writes)
- No prompt hangs or delays
- Works reliably with Fedora's noclobber default
- Naturally persists across server restarts

### Files

- `~/.config/texelation/shell-integration/bash.sh` - Captures env + CWD after each command
- `~/.texel-env-<pane-id>` - Per-pane environment storage
- `apps/texelterm/term.go:runShell()` - Reads env file on shell start
- `apps/texelterm/term.go:SetPaneID()` - Receives pane ID from server

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

### Implemented & Working
- Scrollback reflow on resize
- Environment preserved across shell restart
- Environment preserved across server restart
- Working directory restored on restart
- Per-terminal command history isolation
- Fedora noclobber compatibility
- No prompt hangs or delays

### Edge Cases to Monitor
- Large environments (1000+ variables)
- Concurrent terminal startup from snapshots
- Environment files with special characters in values
