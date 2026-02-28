// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/debuglog/debuglog.go
// Summary: Conditional debug logging controlled by TEXELATION_DEBUG env var.
// Usage: Replace non-essential log.Printf calls with debuglog.Printf.

package debuglog

import (
	"io"
	"log"
	"os"
)

var logger = log.New(io.Discard, "", log.LstdFlags|log.Lmicroseconds)

func init() {
	if os.Getenv("TEXELATION_DEBUG") != "" {
		logger.SetOutput(os.Stderr)
	}
}

// Printf logs a debug message. Output is discarded unless TEXELATION_DEBUG is set.
func Printf(format string, v ...interface{}) {
	logger.Printf(format, v...)
}

// Println logs a debug message. Output is discarded unless TEXELATION_DEBUG is set.
func Println(v ...interface{}) {
	logger.Println(v...)
}

// Enabled reports whether debug logging is active.
func Enabled() bool {
	return logger.Writer() != io.Discard
}
