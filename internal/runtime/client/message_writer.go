// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/message_writer.go
// Summary: Async message writer that decouples the read loop from socket
// back-pressure. Replaces the bare writeMu+conn pattern that deadlocked
// when the read loop tried to issue a fetch-range write while the kernel
// send buffer was full (see commit log around the resize-storm freeze).
//
// Invariant: the caller of Send/TrySend never holds the OS write side; a
// single dedicated goroutine drains the queue. This means the readLoop can
// keep draining incoming frames even when the write side is congested,
// breaking the symmetric "both sides blocked on Write" deadlock.

package clientruntime

import (
	"errors"
	"log"
	"net"
	"sync"
	"sync/atomic"

	"github.com/framegrace/texelation/internal/debuglog"
	"github.com/framegrace/texelation/protocol"
)

// queuedMessage is one pending wire message held by the writer queue. When
// flushed is non-nil the entry is a synchronization barrier — the writer
// goroutine closes it without writing anything. Used by tests via
// flushPending to wait for all preceding writes to land on the conn.
type queuedMessage struct {
	header  protocol.Header
	payload []byte
	flushed chan struct{}
}

// errWriterClosed is returned by Send when the writer has shut down (either
// the writer goroutine returned after a write error, or Close was called).
var errWriterClosed = errors.New("client: message writer closed")

// messageWriter serializes writes to a net.Conn through a buffered channel
// drained by a single goroutine. All writeMessage call sites enqueue here
// instead of holding a mutex across socket I/O.
type messageWriter struct {
	conn     net.Conn
	queue    chan queuedMessage
	shutdown chan struct{}         // closed by Close to signal goroutine + senders
	done     chan struct{}         // closed when the writer goroutine has exited
	err      atomic.Pointer[error] // last write error, set before done is closed

	closeOnce sync.Once
}

// newMessageWriter starts a writer goroutine that drains queued messages and
// writes them serially to conn. bufSize sets the queue capacity: large enough
// that bursts (a flashy resize) absorb without back-pressure on senders, but
// small enough that a stuck socket surfaces as queue saturation rather than
// unbounded memory growth. 256 fits ~16 KiB of typical client→server traffic.
func newMessageWriter(conn net.Conn, bufSize int) *messageWriter {
	w := &messageWriter{
		conn:     conn,
		queue:    make(chan queuedMessage, bufSize),
		shutdown: make(chan struct{}),
		done:     make(chan struct{}),
	}
	go w.run()
	return w
}

// run drains the queue serially. Exits on shutdown signal OR on a terminal
// write error. We never close the queue (callers may race with shutdown);
// the shutdown channel is the explicit termination signal that makes Send
// return an error rather than panicking on send-to-closed-channel.
func (w *messageWriter) run() {
	defer close(w.done)
	for {
		select {
		case msg := <-w.queue:
			if msg.flushed != nil {
				close(msg.flushed)
				continue
			}
			debuglog.Printf("client tx type=%d seq=%d len=%d", msg.header.Type, msg.header.Sequence, len(msg.payload))
			if err := protocol.WriteMessage(w.conn, msg.header, msg.payload); err != nil {
				w.err.Store(&err)
				log.Printf("client write failed: %v", err)
				return
			}
		case <-w.shutdown:
			return
		}
	}
}

// Send enqueues a message, blocking on a full queue until space frees up or
// the writer shuts down. Use from goroutines that are NOT on the read path
// (eventPoll, ackLoop, pingLoop, scheduleResize). Returns an error if the
// writer is no longer running.
func (w *messageWriter) Send(header protocol.Header, payload []byte) error {
	// Fast-path: refuse new sends once the writer goroutine has set a
	// terminal error. This matters for callers (flushFrame) that gate
	// state-machine transitions on the send result — after a write
	// failure they need to skip the pane rather than lock in a
	// reservation that will never get sent.
	if ep := w.err.Load(); ep != nil {
		return *ep
	}
	select {
	case w.queue <- queuedMessage{header: header, payload: payload}:
		return nil
	case <-w.shutdown:
		return errWriterClosed
	case <-w.done:
		if ep := w.err.Load(); ep != nil {
			return *ep
		}
		return errWriterClosed
	}
}

// TrySend enqueues a message non-blocking. Returns false if the queue is
// full OR if the writer has shut down due to a previous write error. Use
// from readLoop and any code path where blocking on the write side would
// back up the read pump and deadlock the protocol. Dropped messages must
// be idempotent or self-recovering (fetch-range requests are re-issued on
// the next flushFrame; pongs are best-effort — if we fail to pong, the
// server will time us out and close, which is the right thing).
func (w *messageWriter) TrySend(header protocol.Header, payload []byte) bool {
	if ep := w.err.Load(); ep != nil {
		return false
	}
	select {
	case <-w.shutdown:
		return false
	default:
	}
	select {
	case w.queue <- queuedMessage{header: header, payload: payload}:
		return true
	default:
		return false
	}
}

// Close stops the writer goroutine and waits for it to exit. Safe to call
// multiple times and safe under concurrent Send/TrySend (we close shutdown
// rather than the queue, so racing senders return errWriterClosed instead
// of panicking on send-to-closed-channel). After Close returns, the writer
// goroutine has exited and any messages still in the queue are dropped.
func (w *messageWriter) Close() {
	w.closeOnce.Do(func() {
		close(w.shutdown)
	})
	<-w.done
}

// flushPending blocks until all messages enqueued before the call have been
// drained by the writer goroutine. Test-only synchronization barrier — the
// production code never reads from the conn we write to, so it has no need
// for this. The flush sentinel travels through the queue in FIFO order, so
// when it lands on the writer side every prior queuedMessage has already
// been written. Returns immediately if the writer has shut down.
func (w *messageWriter) flushPending() {
	flushed := make(chan struct{})
	select {
	case w.queue <- queuedMessage{flushed: flushed}:
	case <-w.shutdown:
		return
	case <-w.done:
		return
	}
	select {
	case <-flushed:
	case <-w.done:
	}
}
