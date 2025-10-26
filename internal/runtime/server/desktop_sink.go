// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/desktop_sink.go
// Summary: Implements desktop sink capabilities for the server runtime.
// Usage: Used by texel-server to coordinate desktop sink when hosting apps and sessions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"github.com/gdamore/tcell/v2"

	"sync"
	"texelation/protocol"
	"texelation/texel"
)

// DesktopSink forwards key events to a local Desktop instance.
type DesktopSink struct {
	desktop   *texel.DesktopEngine
	publisher *DesktopPublisher
	mu        sync.Mutex
}

func NewDesktopSink(desktop *texel.DesktopEngine) *DesktopSink {
	return &DesktopSink{desktop: desktop}
}

func (d *DesktopSink) HandleKeyEvent(session *Session, event protocol.KeyEvent) {
	if d.desktop == nil {
		return
	}
	key := tcell.Key(event.KeyCode)
	mod := tcell.ModMask(event.Modifiers)
	d.desktop.InjectKeyEvent(key, event.RuneValue, mod)
	if d.publisher != nil {
		_ = d.publisher.Publish()
	}
}

func (d *DesktopSink) HandleMouseEvent(session *Session, event protocol.MouseEvent) {
	if d.desktop == nil {
		return
	}
	d.desktop.InjectMouseEvent(int(event.X), int(event.Y), tcell.ButtonMask(event.ButtonMask), tcell.ModMask(event.Modifiers))
	if d.publisher != nil {
		_ = d.publisher.Publish()
	}
}

func (d *DesktopSink) HandleClipboardSet(session *Session, event protocol.ClipboardSet) {
	if d.desktop == nil {
		return
	}
	d.desktop.HandleClipboardSet(event.MimeType, event.Data)
}

func (d *DesktopSink) HandleClipboardGet(session *Session, event protocol.ClipboardGet) []byte {
	if d.desktop == nil {
		return nil
	}
	return d.desktop.HandleClipboardGet(event.MimeType)
}

func (d *DesktopSink) HandleThemeUpdate(session *Session, event protocol.ThemeUpdate) {
	if d.desktop == nil {
		return
	}
	d.desktop.HandleThemeUpdate(event.Section, event.Key, event.Value)
}

func (d *DesktopSink) HandlePaneFocus(session *Session, focus protocol.PaneFocus) {}

func (d *DesktopSink) HandlePaste(session *Session, paste protocol.Paste) {
	if d.desktop == nil || len(paste.Data) == 0 {
		return
	}
	d.desktop.HandlePaste(paste.Data)
	if d.publisher != nil {
		_ = d.publisher.Publish()
	}
}

func (d *DesktopSink) Desktop() *texel.DesktopEngine {
	return d.desktop
}

func (d *DesktopSink) SetPublisher(publisher *DesktopPublisher) {
	d.publisher = publisher
	if d.desktop == nil {
		return
	}
	if publisher == nil {
		d.desktop.SetRefreshHandler(nil)
		return
	}
	d.desktop.SetRefreshHandler(d.publish)
}

func (d *DesktopSink) Publish() {
	if d.publisher != nil {
		_ = d.publisher.Publish()
	}
}

func (d *DesktopSink) publish() {
	d.mu.Lock()
	publisher := d.publisher
	d.mu.Unlock()
	if publisher == nil {
		return
	}
	_ = publisher.Publish()
}

func (d *DesktopSink) Snapshot() (protocol.TreeSnapshot, error) {
	if d.desktop == nil {
		return protocol.TreeSnapshot{}, nil
	}
	capture := d.desktop.CaptureTree()
	return treeCaptureToProtocol(capture), nil
}
