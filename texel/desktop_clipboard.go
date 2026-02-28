// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/desktop_clipboard.go
// Summary: Clipboard operations for the desktop engine.

package texel

import "github.com/framegrace/texelation/internal/debuglog"

// SetClipboard implements ClipboardService for apps running in the desktop.
// Also marks the clipboard as pending for broadcast to clients.
func (d *DesktopEngine) SetClipboard(mime string, data []byte) {
	d.clipboardMu.Lock()
	defer d.clipboardMu.Unlock()
	if d.clipboard == nil {
		d.clipboard = make(map[string][]byte)
	}
	d.clipboardMime = mime
	d.clipboard[mime] = append([]byte(nil), data...)
	d.clipboardPending = true
	debuglog.Printf("CLIPBOARD DEBUG: Desktop.SetClipboard called: mime=%s, len=%d, pending=%v", mime, len(data), d.clipboardPending)
}

// GetClipboard implements ClipboardService for apps running in the desktop.
func (d *DesktopEngine) GetClipboard() (string, []byte, bool) {
	d.clipboardMu.Lock()
	defer d.clipboardMu.Unlock()
	if d.clipboard == nil || d.clipboardMime == "" {
		return "", nil, false
	}
	data := d.clipboard[d.clipboardMime]
	if data == nil {
		return "", nil, false
	}
	return d.clipboardMime, append([]byte(nil), data...), true
}

// HandleClipboardSet is a legacy alias for SetClipboard.
func (d *DesktopEngine) HandleClipboardSet(mime string, data []byte) {
	d.SetClipboard(mime, data)
}

// HandleClipboardGet retrieves clipboard content by MIME type.
func (d *DesktopEngine) HandleClipboardGet(mime string) []byte {
	d.clipboardMu.Lock()
	defer d.clipboardMu.Unlock()
	if d.clipboard == nil {
		return nil
	}
	return append([]byte(nil), d.clipboard[mime]...)
}

// PopPendingClipboard returns clipboard data if it has changed since last pop.
// Used by the server to send clipboard updates to connected clients.
func (d *DesktopEngine) PopPendingClipboard() (string, []byte, bool) {
	d.clipboardMu.Lock()
	defer d.clipboardMu.Unlock()
	if !d.clipboardPending {
		return "", nil, false
	}
	d.clipboardPending = false
	if d.clipboard == nil || d.clipboardMime == "" {
		return "", nil, false
	}
	data := d.clipboard[d.clipboardMime]
	if data == nil {
		return "", nil, false
	}
	return d.clipboardMime, append([]byte(nil), data...), true
}

// HandlePaste routes paste data to the active pane via the event loop.
func (d *DesktopEngine) HandlePaste(data []byte) {
	if len(data) == 0 {
		return
	}
	copied := make([]byte, len(data))
	copy(copied, data)
	d.SendEvent(desktopEvent{kind: pasteEventKind, paste: copied})
}

// handlePasteInternal routes paste data to the active pane (called from event loop).
func (d *DesktopEngine) handlePasteInternal(data []byte) {
	if len(data) == 0 || d.inControlMode {
		return
	}
	if d.zoomedPane != nil && d.zoomedPane.Pane != nil {
		d.zoomedPane.Pane.handlePaste(data)
		return
	}
	if d.activeWorkspace != nil {
		d.activeWorkspace.handlePaste(data)
	}
}
