# Terminal History Design

## Overview

This document describes the design for infinite terminal history in texelterm, combining a large in-memory buffer with file-based persistent storage. The design is built with encryption and privacy controls in mind from the start.

## Goals

1. **Large in-memory buffer**: Configurable size (default 100,000 lines) for fast scrollback
2. **Infinite file-based storage**: All history persisted to disk
3. **Session identification**: Unique IDs and metadata for future history management app
4. **Performance**: Fast access to recent history, lazy loading for older content
5. **Privacy & Security**: Encryption-ready architecture, sensitive data opt-out
6. **Configurability**: User-controllable memory limits, persistence, and privacy settings

## Architecture

### Components

```
┌─────────────────────────────────────────────┐
│           TexelTerm (term.go)               │
│  - Manages terminal lifecycle               │
│  - Reads history config from theme          │
│  - Handles privacy markers                  │
└─────────────────┬───────────────────────────┘
                  │
                  v
┌─────────────────────────────────────────────┐
│         VTerm (parser/vterm.go)             │
│  - Delegates to HistoryManager              │
│  - Detects privacy mode markers             │
└─────────────────┬───────────────────────────┘
                  │
                  v
┌─────────────────────────────────────────────┐
│    HistoryManager (parser/history.go)       │
│  - In-memory circular buffer (100K default) │
│  - Session metadata                         │
│  - Privacy mode tracking                    │
│  - Coordinates with HistoryStore            │
└─────────────────┬───────────────────────────┘
                  │
                  v
┌─────────────────────────────────────────────┐
│   HistoryStore (parser/history_store.go)    │
│  - File-based persistence                   │
│  - Append-only log format                   │
│  - Compression + Encryption layer           │
│  - Session indexing                         │
└─────────────────────────────────────────────┘
```

### Data Flow

1. **New terminal output** → VTerm (privacy check) → HistoryManager → HistoryStore (encrypt + async write)
2. **Scrollback (recent)** → HistoryManager (in-memory) → VTerm → UI
3. **Scrollback (old)** → HistoryManager → HistoryStore (decrypt + lazy load) → VTerm → UI

## Implementation Details

### 1. HistoryManager

Manages in-memory buffer and coordinates persistence:

```go
type HistoryManager struct {
    // In-memory circular buffer
    buffer     [][]Cell
    maxSize    int           // Configurable (default 100000)
    head       int           // Circular buffer head
    length     int           // Current number of lines

    // Session metadata
    sessionID  string        // Unique session identifier (UUID)
    startTime  time.Time
    command    string        // Shell command
    workingDir string        // CWD at start

    // Privacy control
    privacyMode      bool      // Temporarily disable persistence
    privacyModeDepth int       // Track nested privacy blocks
    redactPatterns   []string  // Regex patterns to redact

    // Persistence
    store      *HistoryStore
    enabled    bool          // Enable/disable file persistence

    // Synchronization
    mu         sync.RWMutex
}
```

**Key methods**:
- `NewHistoryManager(config HistoryConfig) *HistoryManager`
- `AppendLine(line []Cell)` - Add line to buffer, persist if not in privacy mode
- `GetLine(index int) []Cell` - Get line from memory or load from disk
- `Length() int` - Total lines (memory + disk)
- `EnablePrivacyMode()` - Stop persisting new lines
- `DisablePrivacyMode()` - Resume persisting
- `Flush()` - Force write pending lines to disk
- `Close()` - Finalize session and close files

**Privacy mode markers** (OSC sequences):
- `OSC 1337;HistoryPrivacy=on` - Enable privacy mode
- `OSC 1337;HistoryPrivacy=off` - Disable privacy mode
- Shell integration can send these before/after sensitive commands

### 2. HistoryStore

Handles file-based persistence with encryption support:

```go
type HistoryStore struct {
    // File paths
    baseDir     string       // ~/.local/share/texelation/history
    sessionFile string       // <sessionID>.hist.enc
    indexFile   string       // <sessionID>.idx.enc
    metaFile    string       // <sessionID>.meta (not encrypted)

    // Write pipeline: file → buffer → gzip → encryption
    file        *os.File
    encWriter   io.WriteCloser  // Encryption layer (or pass-through)
    gzipWriter  *gzip.Writer
    bufWriter   *bufio.Writer

    // Encryption (prepared for future use)
    encryptionKey []byte      // Derived from user key/password
    encryptionEnabled bool

    // Index tracking
    lineOffsets []int64      // Offsets in encrypted stream

    // Stats
    lineCount    int
    bytesWritten int64

    mu          sync.Mutex
}
```

**File formats**:

1. **History file** (`<sessionID>.hist.enc`):
   - Layered format: Encryption(Gzip(Binary data))
   - Binary format per line: `[length:4 bytes][cell data:bytes]`
   - Cell encoding: `[rune:4 bytes][fg:4][bg:4][attr:1][wrapped:1]` = 14 bytes/cell
   - Append-only for performance
   - File header: `TXHIST01[flags:4]` (version + encryption flag)

2. **Index file** (`<sessionID>.idx.enc`):
   - Encrypted if main file is encrypted
   - Binary format: array of `[offset:8 bytes]`
   - Enables fast seeking in compressed/encrypted stream

3. **Metadata file** (`<sessionID>.meta`):
   - JSON format (NOT encrypted - for indexing)
   ```json
   {
     "session_id": "550e8400-e29b-41d4-a716-446655440000",
     "start_time": "2025-12-04T10:30:00Z",
     "end_time": "2025-12-04T12:45:30Z",
     "command": "/bin/bash",
     "working_dir": "/home/user/projects",
     "hostname": "workstation",
     "username": "user",
     "line_count": 15234,
     "file_size": 2458624,
     "encrypted": true,
     "privacy_gaps": 3
   }
   ```

### 3. Session Identification

Each terminal session gets unique metadata:

```go
type SessionMetadata struct {
    SessionID     string    `json:"session_id"`    // UUID v4
    StartTime     time.Time `json:"start_time"`
    EndTime       time.Time `json:"end_time,omitempty"`
    Command       string    `json:"command"`       // Shell command
    WorkingDir    string    `json:"working_dir"`   // Initial CWD
    Hostname      string    `json:"hostname"`
    Username      string    `json:"username"`
    LineCount     int       `json:"line_count"`
    FileSize      int64     `json:"file_size"`
    Encrypted     bool      `json:"encrypted"`
    PrivacyGaps   int       `json:"privacy_gaps"`  // Number of privacy mode blocks
}
```

**Directory structure**:
```
~/.local/share/texelation/history/
├── 2025/
│   └── 12/
│       └── 04/
│           ├── <sessionID1>.hist.enc  # Encrypted if enabled
│           ├── <sessionID1>.idx.enc
│           ├── <sessionID1>.meta      # Always plaintext
│           ├── <sessionID2>.hist.enc
│           ├── <sessionID2>.idx.enc
│           └── <sessionID2>.meta
└── index.db              # Optional: SQLite index for future search app
```

### 4. Configuration

Theme configuration (`theme.json`):

```json
{
  "texelterm": {
    "history": {
      "memory_lines": 100000,
      "persist_enabled": true,
      "persist_dir": "~/.local/share/texelation/history",
      "compress": true,
      "encrypt": false,
      "flush_interval_ms": 5000,
      "privacy": {
        "respect_markers": true,
        "redact_patterns": [
          "password\\s*=\\s*\\S+",
          "token\\s*=\\s*\\S+"
        ]
      }
    }
  }
}
```

### 5. Privacy Features

**Privacy Mode**:
- When enabled, lines are kept in memory but NOT written to disk
- Can be controlled via OSC sequences (shell integration)
- Useful for commands with sensitive data (passwords, tokens, etc.)

**Redaction Patterns**:
- Regex patterns to automatically redact sensitive data before persistence
- Redacted portions replaced with `[REDACTED]`
- Examples: passwords, API keys, tokens

**Encryption** (Phase 2+):
- ChaCha20-Poly1305 or AES-256-GCM
- Key derivation from user password (PBKDF2 or Argon2)
- Per-session random salt/nonce
- Key stored in system keyring (not in plaintext)

## Implementation Phases

### Phase 1: Core Infrastructure
- Create HistoryManager with configurable in-memory buffer
- Implement basic file persistence (HistoryStore)
- Session ID generation and metadata
- Simple binary format for cells
- Gzip compression
- **File format with encryption flag** (placeholder)
- **Privacy mode support** (OSC sequences)

### Phase 2: Privacy & Security
- Implement encryption layer (ChaCha20-Poly1305)
- Key management integration
- Redaction patterns
- Privacy gap tracking in metadata

### Phase 3: VTerm Integration
- Update VTerm to use HistoryManager
- Replace circular buffer implementation
- Read configuration from theme
- Handle session lifecycle (start/stop)
- Parse privacy mode OSC sequences

### Phase 4: Testing & Polish
- Unit tests for HistoryManager and HistoryStore
- Integration tests with VTerm
- Error handling and recovery
- Performance benchmarks
- Regression tests

## Future Enhancements

- History management app (search, browse, replay)
- Full-text search with encrypted content
- Configurable retention policies
- Export/import functionality
- Shell integration helpers (functions to mark sensitive commands)
- Privacy mode auto-detection (command prefixes like `export SECRET=`)

## Performance Considerations

1. **Memory usage**: 100K lines × ~80 cols × ~16 bytes/cell ≈ 128 MB per terminal
2. **Disk I/O**: Async buffered writes, flushed every 5 seconds
3. **Compression**: ~60-70% size reduction
4. **Encryption overhead**: ~5-10% performance impact (acceptable)
5. **Write performance**: Append-only, no seeks during normal operation

## Error Handling

- Disk write failure: continue in-memory, log error
- Encryption key unavailable: fall back to unencrypted or memory-only
- Corrupt files: log error, start fresh session
- Auto-retry failed writes (with exponential backoff)

## Security & Privacy

- **Encryption**: AES-256-GCM or ChaCha20-Poly1305 (authenticated encryption)
- **Key management**: System keyring integration (prepared architecture)
- **Privacy markers**: OSC 1337 sequences for shell integration
- **Redaction**: Regex-based auto-redaction of sensitive patterns
- **Metadata**: Session metadata NOT encrypted (enables search/indexing)
- **File permissions**: 0600 (user read/write only)

## Compatibility Notes

The design ensures:
1. **Encryption compatibility**: File format includes version and encryption flags
2. **Privacy compatibility**: Privacy mode tracking in metadata
3. **Upgrade path**: Unencrypted files can be batch-encrypted later
4. **Downgrade path**: Can disable encryption and read existing files
