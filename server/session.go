package server

import (
	"errors"
	"sync"
	"time"

	"texelation/protocol"
)

var (
	ErrSessionClosed = errors.New("server: session closed")
)

// DiffPacket holds a serialised buffer delta ready to be sent to clients.
type DiffPacket struct {
	Sequence uint64
	Payload  []byte
	Message  protocol.Header
}

// Session manages pane buffers and queued diffs for a single client connection.
type Session struct {
	id           [16]byte
	mu           sync.Mutex
	nextSequence uint64
	diffs        []DiffPacket
	lastSnapshot time.Time
	closed       bool
	maxDiffs     int
}

func NewSession(id [16]byte, maxDiffs int) *Session {
	if maxDiffs < 0 {
		maxDiffs = 0
	}
	return &Session{id: id, diffs: make([]DiffPacket, 0, 128), maxDiffs: maxDiffs}
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
	defer s.mu.Unlock()
	s.closed = true
	s.diffs = nil
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
