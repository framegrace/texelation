// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/zoomdebug/zoomdebug.go
// Summary: Env-gated logger for issue #235 investigation. Removed
// once the underlying bugs are fixed and their regression tests
// are in place.
//
// Usage:
//   zoomdebug.Logf("incrementalComposite: zoomed=%v zoomPane=%x panes=%d",
//       state.zoomed, state.zoomedPane[:4], len(panes))
//
// Gate: set TEXELATION_DEBUG_ZOOM=1 to enable. Default-off builds
// pay only an os.Getenv at process start plus a boolean check per
// call site.

package zoomdebug

import (
	"log"
	"os"
)

var enabled = os.Getenv("TEXELATION_DEBUG_ZOOM") == "1"

// Enabled reports whether zoom-debug logging is active. Call sites
// can use this to skip expensive formatting when disabled.
func Enabled() bool { return enabled }

// Logf writes a [zoom-debug] prefixed log line via the standard
// logger when the env gate is set. No-op otherwise.
func Logf(format string, args ...any) {
	if !enabled {
		return
	}
	log.Printf("[zoom-debug] "+format, args...)
}
