package server

import "texelation/protocol"

// EventSink receives events associated with a session.
type EventSink interface {
	HandleKeyEvent(session *Session, event protocol.KeyEvent)
	HandleMouseEvent(session *Session, event protocol.MouseEvent)
	HandleClipboardSet(session *Session, event protocol.ClipboardSet)
	HandleClipboardGet(session *Session, event protocol.ClipboardGet) []byte
	HandleThemeUpdate(session *Session, event protocol.ThemeUpdate)
}

// SnapshotProvider exposes a full tree snapshot for connected clients.
type SnapshotProvider interface {
	Snapshot() (protocol.TreeSnapshot, error)
}

// nopSink discards events when no sink is provided.
type nopSink struct{}

func (nopSink) HandleKeyEvent(*Session, protocol.KeyEvent)                {}
func (nopSink) HandleMouseEvent(*Session, protocol.MouseEvent)            {}
func (nopSink) HandleClipboardSet(*Session, protocol.ClipboardSet)        {}
func (nopSink) HandleClipboardGet(*Session, protocol.ClipboardGet) []byte { return nil }
func (nopSink) HandleThemeUpdate(*Session, protocol.ThemeUpdate)          {}

func (nopSink) Snapshot() (protocol.TreeSnapshot, error) {
	return protocol.TreeSnapshot{}, nil
}
