package server

import "texelation/protocol"

// EventSink receives key events associated with a session. Future revisions can
// expand this to handle mouse, clipboard, etc.
type EventSink interface {
	HandleKeyEvent(session *Session, event protocol.KeyEvent)
}

// nopSink discards events when no sink is provided.
type nopSink struct{}

func (nopSink) HandleKeyEvent(session *Session, event protocol.KeyEvent) {}
