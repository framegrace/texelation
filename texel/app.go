package texel

import "github.com/gdamore/tcell/v2"

// MessageType defines the type of a message sent to an app.
type MessageType int

const (
	// MsgStateUpdate is sent when screen-level state changes.
	MsgStateUpdate MessageType = iota
)

// StatePayload is the data associated with a MsgStateUpdate.
type StatePayload struct {
	InControlMode bool
	SubMode       rune
	ActiveTitle   string
}

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
	// HandleMessage handles a generic message from the screen.
	HandleMessage(msg Message)
}
