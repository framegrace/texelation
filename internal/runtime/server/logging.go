package server

import (
	"io"
	"log"
)

var (
	debugLog       = log.New(io.Discard, "", log.LstdFlags)
	defaultLogDest = log.Writer()
)

// SetVerboseLogging toggles verbose server logging.
// When disabled (default), debug output is discarded.
func SetVerboseLogging(enable bool) {
	if enable {
		log.SetOutput(defaultLogDest)
		debugLog.SetOutput(defaultLogDest)
	} else {
		log.SetOutput(io.Discard)
		debugLog.SetOutput(io.Discard)
	}
}
