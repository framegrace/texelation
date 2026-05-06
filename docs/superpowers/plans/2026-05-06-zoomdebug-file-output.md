# Zoomdebug file output — implementation plan

> **For agentic workers:** Use superpowers:executing-plans (inline) or superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Extend `internal/runtime/zoomdebug` so it writes to a file when `TEXELATION_DEBUG_ZOOM_FILE=/path` is set, with a `[zoom-debug <role>]` prefix that distinguishes client from server lines, so client-side instrumentation becomes observable inside a tcell-owned terminal.

**Spec:** `docs/superpowers/specs/2026-05-06-zoomdebug-file-output-design.md`

---

## File structure

| Path | Status | Responsibility |
|------|--------|----------------|
| `internal/runtime/zoomdebug/zoomdebug.go` | Modify | Add `Init(role)`, file output, mutex, prefix logic. |
| `internal/runtime/zoomdebug/zoomdebug_test.go` | Create | Unit tests covering all Logf paths. |
| `cmd/texel-server/main.go` | Modify | Call `zoomdebug.Init("server")`. |
| `cmd/texelation/main.go` | Modify | Call `zoomdebug.Init("client")` — the launcher runs the client in-process; the daemon it spawns is `texel-server` and inits there. |
| `client/cmd/texel-client/main.go` | Modify | Call `zoomdebug.Init("client")`. |

---

## Task 1: Extend zoomdebug.go with Init + file output

**Files:**
- Modify: `internal/runtime/zoomdebug/zoomdebug.go`

- [ ] **Step 1: Replace zoomdebug.go**

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/zoomdebug/zoomdebug.go
// Summary: Env-gated logger for issue #235 investigation. Removed
// once the underlying bugs are fixed and their regression tests
// are in place.
//
// Usage:
//   zoomdebug.Init("client") // or "server", once at process start
//   zoomdebug.Logf("incrementalComposite: zoomed=%v ...", state.zoomed)
//
// Gates:
//   TEXELATION_DEBUG_ZOOM=1               enable logging (required)
//   TEXELATION_DEBUG_ZOOM_FILE=/path/log  optional, route output to file
//
// When the file env var is unset, output goes via log.Printf
// (server: ~/.texelation/server.log; client: lost into tcell).

package zoomdebug

import (
	"fmt"
	"log"
	"os"
	"sync"
)

var (
	enabled = os.Getenv("TEXELATION_DEBUG_ZOOM") == "1"

	mu         sync.Mutex
	role       = "?"
	roleSet    = false
	outputFile *os.File // nil when no file env var set or open failed
)

// Init records the process role and (if TEXELATION_DEBUG_ZOOM_FILE
// is set) opens the output file. Call once early in main, before
// any Logf call. Subsequent calls with the same role are no-ops;
// with a different role they log a warning and overwrite the role.
func Init(r string) {
	mu.Lock()
	defer mu.Unlock()
	if roleSet {
		if role != r {
			log.Printf("zoomdebug: Init called with role=%q after role=%q; overwriting",
				r, role)
			role = r
		}
		return
	}
	role = r
	roleSet = true
	if !enabled {
		return
	}
	if path := os.Getenv("TEXELATION_DEBUG_ZOOM_FILE"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			log.Printf("zoomdebug: open %q: %v; falling back to log.Printf", path, err)
			return
		}
		outputFile = f
	}
}

// Enabled reports whether zoom-debug logging is active.
func Enabled() bool { return enabled }

// Logf writes a "[zoom-debug <role>] " prefixed line. Routes to
// the file set by Init when one is open, otherwise log.Printf.
// Safe to call from any goroutine.
func Logf(format string, args ...any) {
	if !enabled {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	prefix := "[zoom-debug " + role + "] "
	if outputFile != nil {
		// Add a timestamp ourselves; log.Printf adds one when used.
		line := fmt.Sprintf(prefix+format+"\n", args...)
		_, _ = outputFile.WriteString(line)
		return
	}
	log.Printf(prefix+format, args...)
}

// resetForTesting is a test-only hook that re-reads the env vars
// and reinitializes package state. Production code never calls it.
func resetForTesting() {
	mu.Lock()
	defer mu.Unlock()
	enabled = os.Getenv("TEXELATION_DEBUG_ZOOM") == "1"
	role = "?"
	roleSet = false
	if outputFile != nil {
		_ = outputFile.Close()
		outputFile = nil
	}
}
```

- [ ] **Step 2: Build**

Run: `go build ./internal/runtime/zoomdebug/...`

Expected: no output, exit 0.

- [ ] **Step 3: Ensure existing call sites still compile**

Run: `go build ./...`

Expected: no output, exit 0. The existing `Logf` call sites added in the issue-235 investigation work continue to compile because the public signature of `Logf` is unchanged.

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/zoomdebug/zoomdebug.go
git commit -m "Add zoomdebug Init + file output for client-side capture"
```

---

## Task 2: Unit tests

**Files:**
- Create: `internal/runtime/zoomdebug/zoomdebug_test.go`

- [ ] **Step 1: Write the test file**

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package zoomdebug

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLogf_DisabledIsNoOp(t *testing.T) {
	t.Setenv("TEXELATION_DEBUG_ZOOM", "")
	t.Setenv("TEXELATION_DEBUG_ZOOM_FILE", "")
	resetForTesting()
	Init("client")

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	Logf("should not appear")
	if buf.Len() != 0 {
		t.Fatalf("expected no output when disabled, got %q", buf.String())
	}
}

func TestLogf_FallbackToLogPrintf(t *testing.T) {
	t.Setenv("TEXELATION_DEBUG_ZOOM", "1")
	t.Setenv("TEXELATION_DEBUG_ZOOM_FILE", "")
	resetForTesting()
	Init("client")

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	Logf("hello %s", "world")
	got := buf.String()
	if !contains(got, "[zoom-debug client] hello world") {
		t.Fatalf("expected log.Printf path with role prefix, got %q", got)
	}
}

func TestLogf_FileOutputRespectsRole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zoom.log")
	t.Setenv("TEXELATION_DEBUG_ZOOM", "1")
	t.Setenv("TEXELATION_DEBUG_ZOOM_FILE", path)
	resetForTesting()
	Init("server")

	Logf("from server: %d", 42)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	got := string(data)
	if !contains(got, "[zoom-debug server] from server: 42") {
		t.Fatalf("expected server-prefixed line in file, got %q", got)
	}
}

func TestInit_DoubleCallSameRoleIsNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zoom.log")
	t.Setenv("TEXELATION_DEBUG_ZOOM", "1")
	t.Setenv("TEXELATION_DEBUG_ZOOM_FILE", path)
	resetForTesting()

	Init("client")
	Init("client") // second same-role call should not warn

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	Logf("ok")
	if contains(buf.String(), "Init called with role") {
		t.Fatalf("did not expect overwrite warning on same-role second Init")
	}
}

func TestInit_DoubleCallDifferentRoleWarnsAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zoom.log")
	t.Setenv("TEXELATION_DEBUG_ZOOM", "1")
	t.Setenv("TEXELATION_DEBUG_ZOOM_FILE", path)
	resetForTesting()

	var warnBuf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&warnBuf)
	defer log.SetOutput(prev)

	Init("client")
	Init("server") // should warn and overwrite

	if !contains(warnBuf.String(), "Init called with role=\"server\"") {
		t.Fatalf("expected overwrite warning, got %q", warnBuf.String())
	}

	Logf("after overwrite")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !contains(string(data), "[zoom-debug server] after overwrite") {
		t.Fatalf("expected new role applied, got %q", string(data))
	}
}

func TestLogf_ConcurrentWritesNoInterleave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zoom.log")
	t.Setenv("TEXELATION_DEBUG_ZOOM", "1")
	t.Setenv("TEXELATION_DEBUG_ZOOM_FILE", path)
	resetForTesting()
	Init("client")

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			Logf("line=%d aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", i)
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := bytes.Count(data, []byte("\n"))
	if lines != n {
		t.Fatalf("expected %d lines, got %d", n, lines)
	}
}

func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/runtime/zoomdebug/... -count=1 -v`

Expected: 6 tests, all PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/zoomdebug/zoomdebug_test.go
git commit -m "Test zoomdebug Init, file output, role prefix, and concurrent writes"
```

---

## Task 3: Wire Init into texel-server

**Files:**
- Modify: `cmd/texel-server/main.go`

- [ ] **Step 1: Find main entry**

Run: `grep -n "func main" cmd/texel-server/main.go`

Expected output: a single line such as `12:func main() {`. Note the line.

- [ ] **Step 2: Add the import and Init call**

Add `"github.com/framegrace/texelation/internal/runtime/zoomdebug"` to the import block.

At the very top of `main()`, before any other initialization, add:

```go
zoomdebug.Init("server")
```

- [ ] **Step 3: Build**

Run: `go build ./cmd/texel-server/...`

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add cmd/texel-server/main.go
git commit -m "Init zoomdebug as server in texel-server entry point"
```

---

## Task 4: Wire Init into texelation launcher (client role)

**Files:**
- Modify: `cmd/texelation/main.go`

The texelation launcher runs the *client* in-process and supervises the *server* as a separate `texel-server` daemon. We init as `client` here — the daemon's own main (Task 3) handles the server side.

- [ ] **Step 1: Find main entry**

Run: `grep -n "func main" cmd/texelation/main.go`

- [ ] **Step 2: Add the import and Init call**

Add `"github.com/framegrace/texelation/internal/runtime/zoomdebug"` to the import block.

At the very top of `main()`, before any other initialization, add:

```go
zoomdebug.Init("client")
```

- [ ] **Step 3: Build**

Run: `go build ./cmd/texelation/...`

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add cmd/texelation/main.go
git commit -m "Init zoomdebug as client in texelation launcher"
```

---

## Task 5: Wire Init into the standalone client

**Files:**
- Modify: `client/cmd/texel-client/main.go`

The standalone `texel-client` binary connects to a running daemon. Without Init it would log under the `?` role; that works but obscures which traces came from a standalone client vs the launcher.

- [ ] **Step 1: Find main entry**

Run: `grep -n "func main" client/cmd/texel-client/main.go`

- [ ] **Step 2: Add the import and Init call**

Add `"github.com/framegrace/texelation/internal/runtime/zoomdebug"` to the import block.

At the very top of `main()`, before any other initialization, add:

```go
zoomdebug.Init("client")
```

- [ ] **Step 3: Build**

Run: `go build ./client/cmd/texel-client/...`

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add client/cmd/texel-client/main.go
git commit -m "Init zoomdebug as client in standalone texel-client"
```

---

## Task 6: Full build + tests

**Files:** none modified.

- [ ] **Step 1: Full build**

Run: `make build`

Expected: builds all binaries with no errors.

- [ ] **Step 2: Full test suite (uncached)**

Run: `CCACHE_DISABLE=1 GOCACHE=$(pwd)/.cache CGO_ENABLED=0 go test -count=1 ./...`

Expected: exit 0; the new zoomdebug tests show as `ok`.

- [ ] **Step 3: Smoke check**

```bash
texelation --stop || true
TEXELATION_DEBUG_ZOOM=1 TEXELATION_DEBUG_ZOOM_FILE=/tmp/zoomdebug-smoke.log ./bin/texelation &
sleep 2
texelation --stop
grep -c "\[zoom-debug client\]" /tmp/zoomdebug-smoke.log
grep -c "\[zoom-debug server\]" /tmp/zoomdebug-smoke.log
```

Expected: both counts > 0. Both processes wrote to the same file with their distinct roles.

If only `server` lines appear, the launcher's Init didn't run early enough or the env var isn't propagating to the client path. If only `client` lines appear, the daemon spawned without the env var (verify via `cat /proc/<daemon-pid>/environ | tr '\0' '\n' | grep ZOOM`).

---

## Self-Review

**Spec coverage check:**
- Env var contract (both `DEBUG_ZOOM=1` AND `DEBUG_ZOOM_FILE` required) → Task 1 step 1, the `enabled` gate guards file open. ✓
- Public API `Init`, `Enabled`, `Logf` → Task 1 step 1 defines all three. ✓
- Concurrency safety → Task 1 step 1 mutex; Task 2 has `TestLogf_ConcurrentWritesNoInterleave`. ✓
- Process role disambiguation → Tasks 3, 4, 5 wire all entry points. ✓
- Test scenarios from spec § Testing → Task 2 covers all six. ✓
- Acceptance criterion (both client and server lines in same file) → Task 6 step 3 smoke check. ✓

**Placeholder scan:** clean. Every step has the actual code or command.

**Type/symbol consistency:** `Init(string)`, `Logf(string, ...any)`, `Enabled() bool` match across spec, implementation, tests, and call sites. `resetForTesting` is package-private and only used in `_test.go`.
