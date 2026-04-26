// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/session.go
// Summary: Implements session capabilities for the server runtime.
// Usage: Used by texel-server to coordinate session when hosting apps and sessions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/framegrace/texelation/internal/persistence/atomicjson"
	"github.com/framegrace/texelation/protocol"
)

var (
	ErrSessionClosed = errors.New("server: session closed")
)

var sessionStatsReporter func(SessionStats)

// SetSessionStatsReporter wires a hook invoked whenever session stats change.
func SetSessionStatsReporter(reporter func(SessionStats)) {
	sessionStatsReporter = reporter
}

// SetSessionStatsObserver registers an observer for session stats.
func SetSessionStatsObserver(observer SessionStatsObserver) {
	if observer == nil {
		sessionStatsReporter = nil
		return
	}
	sessionStatsReporter = observer.ObserveSessionStats
}

// DiffPacket holds a serialised buffer delta ready to be sent to clients.
type DiffPacket struct {
	Sequence uint64
	Payload  []byte
	Message  protocol.Header
}

// storedMeta is the in-memory mirror of the Plan F session-level metadata
// fields. Held alongside viewports and updated via dedicated hooks so the
// writer can build a complete StoredSession on each Update.
type storedMeta struct {
	pinned         bool
	label          string
	paneCount      int
	firstPaneTitle string
}

// Session manages pane buffers and queued diffs for a single client connection.
type Session struct {
	id             [16]byte
	mu             sync.Mutex
	nextSequence   uint64
	diffs          []DiffPacket
	lastSnapshot   time.Time
	closed         bool
	maxDiffs       int
	droppedDiffs   uint64
	lastDroppedSeq uint64
	viewports      *ClientViewports
	revisionsMu    sync.Mutex
	revisions      map[[16]byte]uint32
	// Plan D2: cross-restart persistence. Nil-safe: when nil (no
	// disk path resolved), all hook calls are no-ops.
	writer     *atomicjson.Store[StoredSession]
	storedMu   sync.Mutex
	storedMeta storedMeta // updated by RecordPaneActivity, written through writer
}

func NewSession(id [16]byte, maxDiffs int) *Session {
	if maxDiffs < 0 {
		maxDiffs = 0
	}
	return &Session{
		id:        id,
		diffs:     make([]DiffPacket, 0, 128),
		maxDiffs:  maxDiffs,
		viewports: NewClientViewports(),
		revisions: make(map[[16]byte]uint32),
	}
}

// AttachWriter wires up cross-restart persistence for this session. May
// be called once at session creation (for fresh sessions) or after
// rehydration (for sessions reconstructed from disk via Manager). Safe
// to call before any Apply*/Enqueue* hooks.
func (s *Session) AttachWriter(path string, debounce time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer != nil {
		return
	}
	s.writer = atomicjson.NewStore[StoredSession](path, debounce)
}

// schedulePersist builds a StoredSession snapshot from current state
// and hands it to the writer.
//
// PRECONDITION: caller MUST NOT hold s.mu. This function acquires
// s.mu briefly to read s.writer, then s.storedMu, then
// s.viewports.mu (via Snapshot's RLock); holding s.mu at entry would
// invert lock order against any future code that takes s.mu while
// inside the publisher or viewport plumbing.
//
// Lock-discipline note: s.writer is read UNDER s.mu (snapshot the
// pointer, then drop the lock). A naive read like `if s.writer == nil`
// outside the lock would race with Session.Close, which nils s.writer
// under s.mu — an unprotected read could observe non-nil at the check
// then deref a niled-out value during Update.
//
// Safe to call from any goroutine. Nil-safe (no-op when writer absent).
func (s *Session) schedulePersist() {
	s.mu.Lock()
	w := s.writer
	s.mu.Unlock()
	if w == nil {
		return
	}

	s.storedMu.Lock()
	meta := s.storedMeta
	s.storedMu.Unlock()

	vps := s.viewports.Snapshot()
	stored := StoredSession{
		SchemaVersion:  StoredSessionSchemaVersion,
		SessionID:      s.id,
		LastActive:     time.Now().UTC(),
		Pinned:         meta.pinned,
		Label:          meta.label,
		PaneCount:      meta.paneCount,
		FirstPaneTitle: meta.firstPaneTitle,
		PaneViewports:  make([]StoredPaneViewport, 0, len(vps)),
	}
	for paneID, v := range vps {
		stored.PaneViewports = append(stored.PaneViewports, StoredPaneViewport{
			PaneID:        paneID,
			AltScreen:     v.AltScreen,
			AutoFollow:    v.AutoFollow,
			ViewBottomIdx: v.ViewBottomIdx,
			Rows:          v.Rows,
			Cols:          v.Cols,
		})
	}
	w.Update(stored)
}

// RecordPaneActivity updates the session-level pane metadata used by
// Plan F's session-discovery picker. Triggers a debounced write.
func (s *Session) RecordPaneActivity(paneCount int, firstPaneTitle string) {
	s.storedMu.Lock()
	s.storedMeta.paneCount = paneCount
	s.storedMeta.firstPaneTitle = firstPaneTitle
	s.storedMu.Unlock()
	s.schedulePersist()
}

// FlushPersistForTest forces the writer to flush any pending state
// synchronously. Tests use this instead of time.Sleep to avoid debounce
// flakes. Production code does NOT call this — Close already flushes.
func (s *Session) FlushPersistForTest() {
	s.mu.Lock()
	w := s.writer
	s.mu.Unlock()
	if w != nil {
		w.Flush()
	}
}

// NextRevision returns the next monotonic revision counter for a pane in this
// session. The counter is shared across publisher lifetimes so that successive
// publishers (one per client connection on the same session) emit strictly
// increasing revisions, preventing the client's BufferCache from rejecting
// a freshly-created publisher's first deltas as stale.
func (s *Session) NextRevision(paneID [16]byte) uint32 {
	s.revisionsMu.Lock()
	defer s.revisionsMu.Unlock()
	rev := s.revisions[paneID] + 1
	s.revisions[paneID] = rev
	return rev
}

// RevisionFor returns the latest revision stamped for paneID, or 0 if the
// pane has not been published yet under this session.
func (s *Session) RevisionFor(paneID [16]byte) uint32 {
	s.revisionsMu.Lock()
	defer s.revisionsMu.Unlock()
	return s.revisions[paneID]
}

// ApplyViewportUpdate records the client's current viewport for a pane.
func (s *Session) ApplyViewportUpdate(u protocol.ViewportUpdate) {
	s.viewports.Apply(u)
	s.schedulePersist()
}

// ApplyResume seeds per-pane viewports from a ResumeRequest payload. Called
// by the connection handler before the first post-resume snapshot so the
// publisher clips correctly on the initial emit.
func (s *Session) ApplyResume(states []protocol.PaneViewportState, paneExists func(id [16]byte) bool) {
	s.viewports.ApplyResume(states, paneExists)
	s.schedulePersist()
}

// Viewport returns the client-reported viewport for the given pane, or
// false if the client has not sent one yet.
func (s *Session) Viewport(paneID [16]byte) (ClientViewport, bool) {
	return s.viewports.Get(paneID)
}

func (s *Session) setMaxDiffs(limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 0 {
		limit = 0
	}
	s.maxDiffs = limit
	if limit > 0 && len(s.diffs) > limit {
		drop := len(s.diffs) - limit
		s.recordDrop(drop)
		s.diffs = append([]DiffPacket(nil), s.diffs[drop:]...)
	}
}

func (s *Session) ID() [16]byte {
	return s.id
}

// EnqueueDiff registers a new buffer delta for broadcast to clients.
func (s *Session) EnqueueDiff(delta protocol.BufferDelta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSessionClosed
	}

	payload, err := protocol.EncodeBufferDelta(delta)
	if err != nil {
		return err
	}

	seq := s.nextSequence + 1
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgBufferDelta,
		Flags:     protocol.FlagChecksum,
		SessionID: s.id,
		Sequence:  seq,
	}

	s.diffs = append(s.diffs, DiffPacket{
		Sequence: seq,
		Payload:  payload,
		Message:  hdr,
	})
	s.nextSequence = seq

	if s.maxDiffs > 0 && len(s.diffs) > s.maxDiffs {
		drop := len(s.diffs) - s.maxDiffs
		s.recordDrop(drop)
		s.diffs = append([]DiffPacket(nil), s.diffs[drop:]...)
	}
	return nil
}

// EnqueueImage registers an image protocol message for broadcast to clients.
func (s *Session) EnqueueImage(msgType uint8, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSessionClosed
	}

	seq := s.nextSequence + 1
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MessageType(msgType),
		Flags:     protocol.FlagChecksum,
		SessionID: s.id,
		Sequence:  seq,
	}

	s.diffs = append(s.diffs, DiffPacket{
		Sequence: seq,
		Payload:  payload,
		Message:  hdr,
	})
	s.nextSequence = seq

	if s.maxDiffs > 0 && len(s.diffs) > s.maxDiffs {
		drop := len(s.diffs) - s.maxDiffs
		s.recordDrop(drop)
		s.diffs = append([]DiffPacket(nil), s.diffs[drop:]...)
	}
	return nil
}

// Ack trims the diff history up to and including the provided sequence.
func (s *Session) Ack(sequence uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sequence == 0 {
		return
	}

	idx := 0
	for idx < len(s.diffs) && s.diffs[idx].Sequence <= sequence {
		idx++
	}
	if idx > 0 {
		s.diffs = s.diffs[idx:]
	}
}

// Pending returns a snapshot of queued diffs beginning after the provided
// sequence. The returned slice is safe to iterate without holding the lock.
func (s *Session) Pending(after uint64) []DiffPacket {
	s.mu.Lock()
	defer s.mu.Unlock()

	if after == 0 {
		out := make([]DiffPacket, len(s.diffs))
		copy(out, s.diffs)
		return out
	}

	for i, diff := range s.diffs {
		if diff.Sequence > after {
			out := make([]DiffPacket, len(s.diffs)-i)
			copy(out, s.diffs[i:])
			return out
		}
	}
	return nil
}

func (s *Session) Close() {
	s.mu.Lock()
	s.closed = true
	s.diffs = nil
	w := s.writer
	s.writer = nil
	s.mu.Unlock()
	if w != nil {
		w.Close() // flushes pending state synchronously
	}
}

func (s *Session) MarkSnapshot(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSnapshot = now
}

func (s *Session) LastSnapshot() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSnapshot
}

func (s *Session) recordDrop(drop int) {
	if drop <= 0 || drop > len(s.diffs) {
		return
	}
	s.droppedDiffs += uint64(drop)
	s.lastDroppedSeq = s.diffs[drop-1].Sequence
	log.Printf("session %x dropped %d diffs (last seq %d, pending %d)", s.id[:4], drop, s.lastDroppedSeq, len(s.diffs)-drop)
	if sessionStatsReporter != nil {
		sessionStatsReporter(s.statsLocked())
	}
}

// Stats returns a snapshot of session diff queue metrics.
func (s *Session) Stats() SessionStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statsLocked()
}

func (s *Session) statsLocked() SessionStats {
	return SessionStats{
		ID:               s.id,
		PendingCount:     len(s.diffs),
		NextSequence:     s.nextSequence,
		DroppedDiffs:     s.droppedDiffs,
		LastDroppedSeq:   s.lastDroppedSeq,
		LastSnapshotTime: s.lastSnapshot,
	}
}

// SessionStats summarises queued diff state for observability.
type SessionStats struct {
	ID               [16]byte
	PendingCount     int
	NextSequence     uint64
	DroppedDiffs     uint64
	LastDroppedSeq   uint64
	LastSnapshotTime time.Time
}
