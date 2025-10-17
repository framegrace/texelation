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
