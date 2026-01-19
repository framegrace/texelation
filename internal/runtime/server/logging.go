package server

import (
	"io"
	"log"
	"os"
)

var (
	debugLog = log.New(io.Discard, "", log.LstdFlags)
)

// SetVerboseLogging toggles verbose server logging.
// When disabled (default), debug output is discarded but important
// messages (errors, boot info) still go to stderr.
func SetVerboseLogging(enable bool) {
	if enable {
		// Verbose mode: all logs to stderr
		log.SetOutput(os.Stderr)
		debugLog.SetOutput(os.Stderr)
	} else {
		// Normal mode: important logs to stderr, debug logs discarded
		log.SetOutput(os.Stderr)
		debugLog.SetOutput(io.Discard)
	}
}
