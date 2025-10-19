package clientruntime

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"
)

// PanicLogger captures panic stack traces and optionally persists them to disk.
type PanicLogger struct {
	path string
	mu   sync.Mutex
}

// NewPanicLogger constructs a panic logger that writes to the provided path if non-empty.
func NewPanicLogger(path string) *PanicLogger {
	return &PanicLogger{path: path}
}

// Recover should be deferred in goroutines to capture panics.
func (p *PanicLogger) Recover(context string) {
	if r := recover(); r != nil {
		p.logPanic(context, r)
		os.Exit(2)
	}
}

// Go starts fn in a goroutine with panic recovery bound to the provided context string.
func (p *PanicLogger) Go(context string, fn func()) {
	go func() {
		defer p.Recover(context)
		fn()
	}()
}

func (p *PanicLogger) logPanic(context string, r interface{}) {
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, true)
	stack := buf[:n]
	msg := fmt.Sprintf("panic in %s: %v\n%s", context, r, stack)
	log.Print(msg)
	fmt.Fprintln(os.Stderr, msg)
	if p.path == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	f, err := os.OpenFile(p.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("panic: unable to write panic log: %v", err)
		return
	}
	defer f.Close()
	ts := time.Now().Format(time.RFC3339Nano)
	fmt.Fprintf(f, "[%s] panic in %s: %v\n%s\n", ts, context, r, stack)
}
