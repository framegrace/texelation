# App Storage Architecture

This document describes how texel apps persist state across app runs and server restarts using the centralized storage service.

## Overview

The storage service provides persistent key-value storage for texel apps with two scopes:

1. **App-Level Storage** - Shared across all instances of the same app type
2. **Per-Pane Storage** - Isolated to individual pane instances

```
┌─────────────────────────────────────────────────────────────────┐
│                      APP STORAGE SERVICE                        │
├─────────────────────────────────┬───────────────────────────────┤
│       App-Level Storage         │      Per-Pane Storage         │
│   (Shared across instances)     │   (Isolated per pane)         │
├─────────────────────────────────┼───────────────────────────────┤
│  ~/.texelation/storage/app/     │  ~/.texelation/storage/pane/  │
│    launcher.json                │    <pane-id>/                 │
│    texelterm.json               │      launcher.json            │
│    shared/favorites.json        │      texelterm.json           │
└─────────────────────────────────┴───────────────────────────────┘
```

All storage is JSON-based for easy debugging and manual editing.

---

## 1. Architecture

**Status**: Complete (2025-12-13)

### Component Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                         DESKTOP                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                   StorageService                         │    │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  │    │
│  │  │ App Scopes  │  │ Pane Scopes │  │ Shared Scopes   │  │    │
│  │  │ launcher    │  │ <id>/launch │  │ shared/favs     │  │    │
│  │  │ texelterm   │  │ <id>/term   │  │ shared/clips    │  │    │
│  │  └─────────────┘  └─────────────┘  └─────────────────┘  │    │
│  │                         │                                │    │
│  │              ┌──────────┴──────────┐                     │    │
│  │              │   In-Memory Cache   │                     │    │
│  │              │   (scope → key → value)                   │    │
│  │              └──────────┬──────────┘                     │    │
│  │                         │ Debounced Flush (2s)           │    │
│  │              ┌──────────┴──────────┐                     │    │
│  │              │     JSON Files      │                     │    │
│  │              │  ~/.texelation/storage/                   │    │
│  │              └─────────────────────┘                     │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
                              │
                    Interface Injection
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
┌───────────────┐   ┌───────────────┐   ┌───────────────┐
│   Launcher    │   │   TexelTerm   │   │   Other App   │
│ (AppStorage)  │   │ (PaneStorage) │   │ (Both)        │
└───────────────┘   └───────────────┘   └───────────────┘
```

### Injection Flow

```
┌─────────────────────────────────────────────────────────────────┐
│  1. App created via factory                                      │
│     app := launcher.New(registry)                                │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  2. Pane attaches app (pane.AttachApp)                          │
│     - Calls app.SetRefreshNotifier()                            │
│     - Calls app.SetPaneID() if PaneIDSetter                     │
│     - Calls app.SetAppStorage() if AppStorageSetter      ◄──NEW │
│     - Calls app.SetStorage() if StorageSetter            ◄──NEW │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  3. App receives storage, loads persisted data                   │
│     func (l *Launcher) SetAppStorage(storage AppStorage) {       │
│         l.storage = storage                                      │
│         data, _ := storage.Get("usageCounts")                    │
│         json.Unmarshal(data, &l.usageCounts)                     │
│     }                                                            │
└─────────────────────────────────────────────────────────────────┘
```

---

## 2. Interfaces

### StorageService (hosted by Desktop)

```go
type StorageService interface {
    // AppStorage returns storage shared across all instances of an app type
    AppStorage(appType string) AppStorage

    // PaneStorage returns storage isolated to a specific pane
    PaneStorage(appType string, paneID [16]byte) AppStorage

    // Flush forces all pending writes to disk
    Flush() error

    // Close flushes and releases resources
    Close() error
}
```

### AppStorage (scoped accessor)

```go
type AppStorage interface {
    // Get retrieves a value. Returns nil if key doesn't exist.
    Get(key string) (json.RawMessage, error)

    // Set stores a value. Any JSON-serializable type accepted.
    Set(key string, value interface{}) error

    // Delete removes a key. No error if missing.
    Delete(key string) error

    // List returns all keys in this scope.
    List() ([]string, error)

    // Clear removes all keys (reset functionality).
    Clear() error

    // Scope returns the scope identifier for debugging.
    Scope() string
}
```

### Injection Interfaces (implemented by apps)

```go
// For app-level storage (shared across instances)
type AppStorageSetter interface {
    SetAppStorage(storage AppStorage)
}

// For per-pane storage (isolated per instance)
type StorageSetter interface {
    SetStorage(storage AppStorage)
}
```

---

## 3. Usage Examples

### Basic Usage (Launcher Example)

```go
type Launcher struct {
    storage     texel.AppStorage
    usageCounts map[string]int
}

// 1. Implement the setter interface
func (l *Launcher) SetAppStorage(storage texel.AppStorage) {
    l.storage = storage
    l.loadUsageCounts()  // Load on attach
}

// 2. Implement SnapshotMetadata for correct app type
func (l *Launcher) SnapshotMetadata() (string, map[string]interface{}) {
    return "launcher", nil
}

// 3. Load data
func (l *Launcher) loadUsageCounts() {
    if l.storage == nil {
        return
    }
    data, _ := l.storage.Get("usageCounts")
    if data != nil {
        json.Unmarshal(data, &l.usageCounts)
    }
}

// 4. Save data (auto-flushes to disk after 2s)
func (l *Launcher) saveUsageCounts() {
    if l.storage == nil {
        return
    }
    l.storage.Set("usageCounts", l.usageCounts)
}
```

### Cross-App Sharing (Convention-Based)

Apps can share data by using agreed-upon namespace prefixes:

```go
// App A writes to shared namespace
func (a *AppA) SetAppStorage(storage texel.AppStorage) {
    // Get shared storage using convention
    a.shared = a.desktop.Storage().AppStorage("shared/favorites")
    a.shared.Set("topApps", []string{"texelterm", "htop"})
}

// App B reads from same namespace
func (b *AppB) SetAppStorage(storage texel.AppStorage) {
    shared := b.desktop.Storage().AppStorage("shared/favorites")
    data, _ := shared.Get("topApps")
    // Use the shared data...
}
```

**Namespace Conventions:**
- `shared/` - Cross-app shared data
- `shared/clipboard` - Shared clipboard history
- `shared/favorites` - Shared favorite apps
- `<apptype>` - App's private namespace (e.g., `launcher`, `texelterm`)

### Per-Pane Storage

```go
type MyApp struct {
    paneStorage texel.AppStorage  // Isolated per pane instance
}

func (a *MyApp) SetStorage(storage texel.AppStorage) {
    a.paneStorage = storage
    // Each pane gets its own storage at:
    // ~/.texelation/storage/pane/<pane-id>/myapp.json
}
```

---

## 4. File Structure

```
~/.texelation/
└── storage/
    ├── app/                          # App-level storage
    │   ├── launcher.json             # Launcher's shared data
    │   ├── texelterm.json            # TexelTerm's shared data
    │   └── shared/
    │       ├── favorites.json        # Cross-app favorites
    │       └── clipboard.json        # Cross-app clipboard
    │
    └── pane/                         # Per-pane storage
        ├── 0102030405060708.../      # Pane ID (hex)
        │   ├── launcher.json
        │   └── texelterm.json
        └── a1b2c3d4e5f6.../
            └── myapp.json
```

### JSON Format Example

`~/.texelation/storage/app/launcher.json`:
```json
{
  "usageCounts": {
    "texelterm": 47,
    "htop": 12,
    "help": 3
  },
  "lastOpened": "2025-12-13T10:30:00Z"
}
```

---

## 5. Implementation Details

### Thread Safety

All operations are protected by `sync.RWMutex`:
- Multiple readers allowed (Get, List)
- Single writer (Set, Delete, Clear)

### Debounced Flush

Writes are cached in memory and flushed to disk after 2 seconds of inactivity:

```
Set("key1", val) ──┐
                   │
Set("key2", val) ──┼── 2s timer ──► Flush to disk
                   │
Set("key3", val) ──┘
```

This batches rapid writes (e.g., during app initialization) into a single disk write.

### Lazy Loading

Scope data is loaded from disk on first access:

```go
func (s *scopedStorage) Get(key string) (json.RawMessage, error) {
    s.service.ensureLoaded(s.scope, s.filePath)  // Load if not cached
    return s.service.cache[s.scope][key], nil
}
```

### Error Handling

- Missing keys return `nil`, no error
- Corrupted files are replaced with empty storage
- Disk errors are logged but don't crash the app

---

## 6. Key Files

| File | Purpose |
|------|---------|
| `texel/storage.go` | Interface definitions |
| `texel/storage_service.go` | File-backed implementation |
| `texel/storage_test.go` | Unit tests (14 tests) |
| `texel/desktop_engine_core.go` | Service initialization and Close() |
| `texel/pane.go` | Injection in AttachApp() |

---

## 7. Future Enhancements

### Permissions System (Planned)

Add access control for cross-app storage:

```go
type Permission struct {
    AppType   string   // "launcher", "*" for any
    Namespace string   // "shared/favorites", "shared/*"
    Access    string   // "read", "write", "readwrite"
}

var defaultPermissions = []Permission{
    // All apps can read shared data
    {"*", "shared/*", "read"},
    // Only specific apps can write
    {"launcher", "shared/favorites", "readwrite"},
}
```

**Implementation approach:**
- Track caller app type when creating storage accessors
- Check permissions in Get/Set methods
- No interface changes needed - enforced internally

### Manifest-Based Permissions (Planned)

Declare permissions in app manifest:

```json
{
  "name": "myapp",
  "permissions": {
    "storage": [
      "shared/favorites:read",
      "shared/myapp-data:readwrite"
    ]
  }
}
```

### Storage Change Notifications (Planned)

Notify apps when shared storage changes:

```go
type StorageObserver interface {
    OnStorageChange(scope, key string, newValue json.RawMessage)
}

// Apps subscribe to changes
storage.Subscribe("shared/favorites", observer)
```

This bridges to the future message passing system.

### Integration with Message Passing (Planned)

Storage and messaging serve complementary purposes:

| Storage Service | Message Passing |
|-----------------|-----------------|
| Persistent (survives restarts) | Ephemeral (runtime only) |
| App's saved state | Real-time inter-app communication |
| Disk I/O | In-memory, fast |

Future integration points:
- Storage changes publish to message bus
- Apps request storage via messages (with permissions)
- Message payloads reference storage keys

### Versioning (Future)

Track historical values for undo/analytics:

```go
type VersionedStorage interface {
    AppStorage
    GetVersion(key string, version int) (json.RawMessage, error)
    ListVersions(key string) ([]VersionInfo, error)
}
```

### Encryption (Future)

Encrypt sensitive storage scopes:

```json
{
  "storage": {
    "encryption": {
      "enabled": true,
      "scopes": ["shared/credentials", "myapp/secrets"]
    }
  }
}
```

---

## 8. Testing

### Unit Tests

14 tests covering:
- Get/Set/Delete/List/Clear operations
- App-level vs pane-level isolation
- Persistence across service restart
- Concurrent access thread safety
- Complex types (maps, slices)
- Edge cases (nil, empty, missing keys)

Run tests:
```bash
go test ./texel/ -run TestStorage -v
```

### Integration Testing

Test with real apps:
1. Start server
2. Open launcher, select apps multiple times
3. Restart server
4. Verify launcher shows apps sorted by usage

### Manual Verification

Check stored data:
```bash
cat ~/.texelation/storage/app/launcher.json
```

---

## 9. Migration Notes

### For New Apps

1. Add storage field to struct
2. Implement `AppStorageSetter` and/or `StorageSetter`
3. Implement `SnapshotMetadata()` for correct app type
4. Load data in setter, save after changes

### For Existing Apps

Storage is opt-in. Apps without setter interfaces continue to work unchanged.
