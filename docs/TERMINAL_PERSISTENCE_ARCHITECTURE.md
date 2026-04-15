# Terminal Persistence Architecture

This document describes how texelterm persists terminal state across resizes, shell restarts, and server restarts.

## Overview

Terminal persistence in texelation covers three main areas:

1. **Scrollback History** - Terminal output preserved across resizes, shell restarts, and server restarts via a sparse globalIdx model backed by a WAL + chunked page store
2. **Shell Environment** - Environment variables, working directory preserved across shell AND server restarts
3. **Command History** - Per-terminal bash history isolation

```
┌─────────────────────────────────────────────────────────────────┐
│                     TERMINAL PERSISTENCE                        │
├─────────────────────┬─────────────────────┬─────────────────────┤
│  Scrollback History │  Shell Environment  │  Command History    │
│  (Sparse + WAL)     │  (Pane-ID files)    │  (Per-pane files)   │
├─────────────────────┼─────────────────────┼─────────────────────┤
│  sparse.Terminal    │  ~/.texel-env-      │  ~/.texel-history-  │
│  AdaptivePersist.   │  <pane-id>          │  <pane-id>          │
│  WAL + PageStore    │                     │                     │
└─────────────────────┴─────────────────────┴─────────────────────┘
```

All persistence is keyed by **pane ID** (stable UUID), enabling seamless restoration across shell restarts AND server restarts.

---

## 1. Scrollback History (Sparse Viewport + WAL)

**Status**: Main-screen cutover complete (2026-04-14). Design spec: [`docs/superpowers/specs/2026-04-11-sparse-viewport-write-window-split-design.md`](superpowers/specs/2026-04-11-sparse-viewport-write-window-split-design.md).

The scrollback system uses a globalIdx-keyed **sparse cell store** with an explicit split between the **write cursor** (what the TUI controls) and the **user viewport** (what the user scrolls). Persistence is handled by a Write-Ahead Log + chunked PageStore; the viewport can always project any range of globalIdx without reflowing unrelated content.

### Architecture

```
           TUI writes                         user scrolls
               │                                    │
               ▼                                    ▼
     ┌──────────────────┐                ┌──────────────────┐
     │   WriteWindow    │                │    ViewWindow    │
     │  writeTop, HWM,  │   OnWrite      │  viewBottom,     │
     │  cursor (gi,col) │ ─ Bottom  ──►  │  autoFollow      │
     └──────────────────┘                └──────────────────┘
               │                                    │
               │  WriteCell(gi, col, cell)          │  VisibleRange() → (top, bot)
               ▼                                    │
     ┌────────────────────────────────────────┐     │
     │            sparse.Store                │ ◄───┘ projects cells via
     │  map[int64]*storeLine  (globalIdx →    │      Store.Get(gi, col)
     │  []Cell). Gaps = blank. No viewport,   │
     │  no scrollback/viewport split.         │
     └────────────────────────────────────────┘
                         │
                         │ sparseLineStoreAdapter.GetLine(gi)
                         ▼
     ┌────────────────────────────────────────┐
     │       AdaptivePersistence              │
     │  WriteThrough  < 10 w/s                │
     │  Debounced    10-100 w/s (adaptive)    │
     │  BestEffort   > 100 w/s (idle flush)   │
     └────────────────────────────────────────┘
                         │
                         ▼
     ┌────────────────────────────────────────┐
     │     WriteAheadLog  +  PageStore        │
     │  WAL journals writes first, PageStore  │
     │  stores committed pages (TXHIST02      │
     │  chunked). Recovery replays WAL.       │
     └────────────────────────────────────────┘
```

The key architectural move from the previous three-level model: **there is no scrollback-vs-viewport distinction inside the store**. Every cell lives at some globalIdx. WriteWindow moves writeTop forward on newline; ViewWindow either follows writeBottom (autoFollow) or pins viewBottom in place while the user scrolls. Reads from either window are O(1) map lookups on Store.

### Key Properties

| Layer | Concurrency | Size bound | Width-aware | Persistence |
|-------|-------------|------------|-------------|-------------|
| sparse.Store | RWMutex | Unbounded in memory (bounded in practice by PageStore eviction) | Width set at construction; lines extend on demand | No (sparse.Store is RAM-only) |
| WriteWindow | Mutex | Fixed to terminal height; HWM tracks high-water | Yes (uses width for cursor bounds) | No (writeTop/cursor persisted separately as metadata) |
| ViewWindow | Mutex | Fixed to terminal height | Yes | No (user navigation state) |
| AdaptivePersistence | Internal rate-limited | Pending queue (bounded by flushing) | No | Forwards to WAL |
| WAL + PageStore | File locking | Unlimited on disk | No | Yes |

### Data Structures

**`sparse.Store`** (`apps/texelterm/parser/sparse/store.go`) — globalIdx → cells, with blank reads for gaps:

```go
type Store struct {
    mu         sync.RWMutex
    width      int
    lines      map[int64]*storeLine
    contentEnd int64  // highest globalIdx ever written; -1 means empty
}
```

**`sparse.WriteWindow`** (`apps/texelterm/parser/sparse/write_window.go`) — TUI-facing cursor anchor:

```go
type WriteWindow struct {
    mu              sync.Mutex
    store           *Store
    width, height   int
    writeTop        int64  // top globalIdx of the addressable viewport
    cursorGlobalIdx int64
    cursorCol       int
    writeBottomHWM  int64  // high-water mark; expansion never retreats past this
}
```

**`sparse.ViewWindow`** (`apps/texelterm/parser/sparse/view_window.go`) — user-facing scroll anchor:

```go
type ViewWindow struct {
    mu         sync.Mutex
    width      int
    height     int
    viewBottom int64
    autoFollow bool
}
```

**`sparse.Terminal`** (`apps/texelterm/parser/sparse/terminal.go`) composes the three. It satisfies the parser-package `MainScreen` interface (see `apps/texelterm/parser/main_screen.go`). The parser → sparse → parser import cycle is avoided by declaring `MainScreen` in the parser package.

**`AdaptivePersistence`** (`apps/texelterm/parser/adaptive_persistence.go`) — rate-adjusted disk writer. The concrete `LineStore` it consumes is `sparseLineStoreAdapter` in `vterm_main_screen.go`, which bridges `MainScreen.ReadLine` to the `*LogicalLine` format the persistence layer expects.

**`WriteAheadLog`** (`apps/texelterm/parser/write_ahead_log.go`) + **`PageStore`** (`apps/texelterm/parser/page_store.go`) — journaled durable storage. See the [sparse pagestore design](superpowers/specs/2026-04-07-sparse-pagestore-design.md) for layout details.

### Usage

```go
// Enable sparse main screen with WAL-backed persistence
err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
    TerminalID: "pane-<uuid>",
})
defer v.CloseMemoryBuffer()
```

`EnableMemoryBufferWithDisk` (in `vterm_main_screen.go`):
1. Constructs the `sparse.Terminal` via `MainScreenFactory` (registered in `parser/sparse/register.go`)
2. Opens the WAL, which recovers `writeTop`/`cursor`/`PromptStartLine`/`CWD` from the last flushed metadata
3. Replays committed pages from PageStore into `sparse.Store` via `LoadFromPageStore`
4. Wires `AdaptivePersistence` between the sparse adapter and the WAL

On `CloseMemoryBuffer`, pending viewport lines are flushed, final metadata is written with `Sync()`, and the WAL is closed cleanly (see `project_scrollback_close_clamp` memory for the clamp protocol).

### Files

Core sparse types:
- `apps/texelterm/parser/sparse/store.go` — globalIdx → cells map
- `apps/texelterm/parser/sparse/write_window.go` — cursor + writeTop anchor
- `apps/texelterm/parser/sparse/view_window.go` — viewBottom + autoFollow
- `apps/texelterm/parser/sparse/terminal.go` — composition type
- `apps/texelterm/parser/sparse/persistence.go` — PageStore ↔ Store bridging
- `apps/texelterm/parser/sparse/register.go` — installs `MainScreenFactory`

Parser integration:
- `apps/texelterm/parser/main_screen.go` — `MainScreen` interface + factory hook
- `apps/texelterm/parser/vterm_main_screen.go` — VTerm wiring + `sparseLineStoreAdapter`
- `apps/texelterm/parser/legacy_stubs.go` — retained stubs (`MemoryBufferOptions`, `EvictedLine`) kept until the last callers migrate

Persistence:
- `apps/texelterm/parser/adaptive_persistence.go` — rate-adjusted writer
- `apps/texelterm/parser/write_ahead_log.go` — WAL with recovery
- `apps/texelterm/parser/page_store.go` — chunked TXHIST02 backing store
- `apps/texelterm/parser/logical_line.go` — width-independent cell container (used at the persistence boundary)

### Design notes

- **No reflow on resize.** The store is width-set-at-construction; resize changes what the write/view windows project, not the underlying cells. TUIs that rewrite on resize (most of them) emit new content at new globalIdxs. The sparse model accepts TUI-side duplicates rather than hacking to suppress them (see `feedback_sparse_resize_tui_duplicates`).
- **writeBottomHWM** guarantees that expansion never retreats writeTop into content the user saw. Shrinking may temporarily drop writeBottom below HWM when the cursor absorbs empty trailing rows.
- **autoFollow** is the default. User scroll pins `viewBottom` and clears autoFollow; input (via `OnInput`) snaps back to the live edge.

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
