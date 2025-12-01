// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/app.go
// Summary: Implements app capabilities for the core desktop engine.
// Usage: Used throughout the project to implement app inside the desktop and panes.

package texel

import "github.com/gdamore/tcell/v2"

// MessageType defines the type of a message sent to an app.
type MessageType int

const (
	// MsgStateUpdate is sent when screen-level state changes.
	MsgStateUpdate MessageType = iota
)

// Message is a generic message that can be sent to an app.
type Message struct {
	Type    MessageType
	Payload interface{}
}

// App defines the interface for any application that can be rendered within a Pane.
// It abstracts the content source, whether it's an external command (PTY)
// or an internal widget (like a clock).
type App interface {
	// Run starts the application's logic, e.g., launching a command or starting a timer.
	Run() error
	// Stop terminates the application's logic.
	Stop()
	// Resize informs the application that the pane's dimensions have changed.
	Resize(cols, rows int)
	// Render returns the application's current visual state as a 2D buffer of Cells.
	Render() [][]Cell
	// GetTitle returns the title of the application.
	GetTitle() string
	HandleKey(ev *tcell.EventKey)
	SetRefreshNotifier(refreshChan chan<- bool)
}

// PasteHandler is implemented by apps that can consume bulk paste payloads.
type PasteHandler interface {
	HandlePaste(data []byte)
}

// SnapshotProvider is implemented by apps that can describe how to restore themselves.
type SnapshotProvider interface {
	SnapshotMetadata() (appType string, config map[string]interface{})
}

// SnapshotFactory constructs an app instance from persisted metadata.
type SnapshotFactory func(title string, config map[string]interface{}) App

// SelectionHandler is implemented by apps that want to handle mouse selections directly.
// Coordinates are pane-local (0-based). SelectionStart should return true if the app
// will manage the selection; returning false allows the workspace fallback to run.
type SelectionHandler interface {
	SelectionStart(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) bool
	SelectionUpdate(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask)
	SelectionFinish(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) (mime string, data []byte, ok bool)
	SelectionCancel()
}

// SelectionDeclarer allows apps to indicate whether they currently support handling selections.
// This is primarily used by wrapper types (like pipelines) that only delegate when an inner app
// provides selection handling.
type SelectionDeclarer interface {
	SelectionEnabled() bool
}

// MouseWheelHandler is implemented by apps that want to react to mouse wheel input.
// deltaX and deltaY indicate wheel steps (positive values scroll right/down).
type MouseWheelHandler interface {
	HandleMouseWheel(x, y, deltaX, deltaY int, modifiers tcell.ModMask)
}

// MouseWheelDeclarer allows apps (or wrappers) to indicate whether they currently handle mouse wheel events.
type MouseWheelDeclarer interface {
	MouseWheelEnabled() bool
}

// AppReplacer allows an app to replace itself with another app in the same pane.
// This is primarily used by launcher apps to spawn the selected app in their place.
type AppReplacer interface {
	ReplaceWithApp(name string, config map[string]interface{})
	Close()
}

// ReplacerReceiver is implemented by apps that want to receive an AppReplacer.
// The pane will call SetReplacer during AttachApp if the app implements this interface.
type ReplacerReceiver interface {
	SetReplacer(replacer AppReplacer)
}

// CloseRequester is implemented by apps that want to intercept closure requests
// (from pane close or replacement) to show a confirmation UI.
type CloseRequester interface {
	// RequestClose is called when the container wants to close the app.
	// Returns true if the app is ready to close immediately.
	// Returns false if the app has intercepted the request (e.g., to show confirmation).
	// If false is returned, the app is responsible for calling AppReplacer.Close()
	// (or equivalent) when it is eventually ready to close.
	RequestClose() bool
}
