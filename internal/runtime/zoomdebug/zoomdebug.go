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
