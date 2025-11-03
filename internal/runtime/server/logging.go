package server

import (
	"io"
	"log"
	"os"
)

var debugLog = log.New(io.Discard, "", log.LstdFlags)

// SetVerboseLogging toggles verbose server logging.
// When disabled (default), debug output is discarded.
func SetVerboseLogging(enable bool) {
	if enable {
		debugLog.SetOutput(os.Stderr)
	} else {
		debugLog.SetOutput(io.Discard)
	}
}
