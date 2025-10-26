// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/desktop_sink.go
// Summary: Implements desktop sink capabilities for the server runtime.
// Usage: Used by texel-server to coordinate desktop sink when hosting apps and sessions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/protocol"
	"texelation/texel"
)

var publishFallbackDelay = 12 * time.Millisecond

// DesktopSink forwards key events to a local Desktop instance.
type DesktopSink struct {
	desktop   *texel.DesktopEngine
	publisher *DesktopPublisher
	scheduler *publishScheduler
	mu        sync.Mutex
}

func NewDesktopSink(desktop *texel.DesktopEngine) *DesktopSink {
	return &DesktopSink{
		desktop:   desktop,
		scheduler: newPublishScheduler(publishFallbackDelay),
	}
}

func (d *DesktopSink) HandleKeyEvent(session *Session, event protocol.KeyEvent) {
	if d.desktop == nil {
		return
	}
	key := tcell.Key(event.KeyCode)
	mod := tcell.ModMask(event.Modifiers)
	d.desktop.InjectKeyEvent(key, event.RuneValue, mod)
	if paneID, ok := d.markActivePaneDirty(); ok {
		d.scheduleFallback(paneID)
	}
}

func (d *DesktopSink) HandleMouseEvent(session *Session, event protocol.MouseEvent) {
	if d.desktop == nil {
		return
	}
	d.desktop.InjectMouseEvent(int(event.X), int(event.Y), tcell.ButtonMask(event.ButtonMask), tcell.ModMask(event.Modifiers))
	if paneID, ok := d.markActivePaneDirty(); ok {
		d.scheduleFallback(paneID)
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
	if paneID, ok := d.markActivePaneDirty(); ok {
		d.scheduleFallback(paneID)
	}
}

func (d *DesktopSink) Desktop() *texel.DesktopEngine {
	return d.desktop
}

func (d *DesktopSink) SetPublisher(publisher *DesktopPublisher) {
	d.publisher = publisher
	if d.scheduler != nil {
		d.scheduler.SetPublisher(publisher)
	}
	if d.desktop == nil {
		return
	}
	if publisher == nil {
		d.desktop.SetRefreshHandler(nil)
		return
	}
	d.desktop.SetRefreshHandler(d.handleRefresh)
}

func (d *DesktopSink) Publish() {
	if d.scheduler != nil {
		d.scheduler.ForcePublish()
		return
	}
	if d.publisher != nil {
		_ = d.publisher.Publish()
	}
}

// PublishAll forces a publish after marking every pane dirty. Use for snapshot/tree updates.
func (d *DesktopSink) PublishAll() {
	if d.publisher != nil {
		d.publisher.MarkAllDirty()
	}
	d.Publish()
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

func (d *DesktopSink) handleRefresh() {
	if _, ok := d.markActivePaneDirty(); ok {
		d.publish()
		return
	}
	d.publish()
}

func (d *DesktopSink) scheduleFallback(paneID [16]byte) {
	if d.scheduler == nil || isZeroPaneID(paneID) {
		return
	}
	d.scheduler.RequestPublish(paneID)
}

func (d *DesktopSink) markActivePaneDirty() ([16]byte, bool) {
	if d.desktop == nil || d.publisher == nil {
		return [16]byte{}, false
	}
	paneID, ok := d.desktop.ActivePaneID()
	if !ok || isZeroPaneID(paneID) {
		return [16]byte{}, false
	}
	d.publisher.MarkPaneDirty(paneID)
	return paneID, true
}

func (d *DesktopSink) Snapshot() (protocol.TreeSnapshot, error) {
	if d.desktop == nil {
		return protocol.TreeSnapshot{}, nil
	}
	capture := d.desktop.CaptureTree()
	return treeCaptureToProtocol(capture), nil
}
