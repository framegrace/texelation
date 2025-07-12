package statusbar

import (
	"fmt"
	"strings"
	"sync"
	"texelation/apps/clock"
	"texelation/texel"

	"github.com/gdamore/tcell/v2"
)

// StatusBarApp displays screen state information.
type StatusBarApp struct {
	width, height int
	mu            sync.RWMutex
	refreshChan   chan<- bool

	// State from Screen
	workspaceID   int
	inControlMode bool
	subMode       rune
	activeTitle   string

	// Internal Clock
	clockApp  texel.App
	stopClock chan struct{}
}

// New creates a new StatusBarApp.
func New() texel.App {
	return &StatusBarApp{
		clockApp:  clock.NewClockApp(),
		stopClock: make(chan struct{}),
	}
}

func (a *StatusBarApp) SetRefreshNotifier(refreshChan chan<- bool) {
	a.refreshChan = refreshChan
	a.clockApp.SetRefreshNotifier(refreshChan)
}

func (a *StatusBarApp) Run() error {
	go a.clockApp.Run()
	<-a.stopClock
	return nil
}

func (a *StatusBarApp) Stop() {
	a.clockApp.Stop()
	close(a.stopClock)
}

func (a *StatusBarApp) Resize(cols, rows int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.width, a.height = cols, rows
	a.clockApp.Resize(cols, rows)
}

func (a *StatusBarApp) GetTitle() string {
	return "Status Bar"
}

func (a *StatusBarApp) HandleKey(ev *tcell.EventKey) {}

// OnEvent handles state updates from the screen's dispatcher.
func (a *StatusBarApp) OnEvent(event texel.Event) {
	if event.Type == texel.EventStateUpdate {
		if payload, ok := event.Payload.(texel.StatePayload); ok {
			a.mu.Lock()
			a.workspaceID = payload.WorkspaceID
			a.inControlMode = payload.InControlMode
			a.subMode = payload.SubMode
			a.activeTitle = payload.ActiveTitle
			a.mu.Unlock()
		}
	}
}

func (a *StatusBarApp) Render() [][]texel.Cell {
	a.mu.RLock()
	defer a.mu.RUnlock()

	buf := make([][]texel.Cell, a.height)
	for i := range buf {
		buf[i] = make([]texel.Cell, a.width)
	}
	if a.height == 0 {
		return buf
	}

	var style tcell.Style
	if a.inControlMode {
		style = tcell.StyleDefault.Background(tcell.ColorSaddleBrown).Foreground(tcell.ColorWhite)
	} else {
		style = tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite)
	}

	// Left part: Mode and Title
	var modeStr string
	if a.inControlMode {
		if a.subMode != 0 {
			modeStr = fmt.Sprintf("[CTRL-A, %c, ?]", a.subMode)
		} else {
			modeStr = "[CONTROL]"
		}
	} else {
		modeStr = "[INPUT]"
	}

	wsStr := fmt.Sprintf("[WS: %d]", a.workspaceID)
	leftStr := fmt.Sprintf(" %s %s %s ", wsStr, modeStr, a.activeTitle)

	// Right part: Clock
	clockCells := a.clockApp.Render()
	clockStr := ""
	if len(clockCells) > 0 && len(clockCells[0]) > 0 {
		var sb strings.Builder
		for _, cell := range clockCells[0] {
			sb.WriteRune(cell.Ch)
		}
		clockStr = strings.TrimSpace(sb.String())
	}
	rightStr := fmt.Sprintf(" %s ", clockStr)

	// Draw background
	for i := 0; i < a.width; i++ {
		buf[0][i] = texel.Cell{Ch: ' ', Style: style}
	}

	// Draw left string
	col := 0
	for _, r := range leftStr {
		if col < a.width {
			buf[0][col] = texel.Cell{Ch: r, Style: style}
			col++
		}
	}

	// Draw right string
	col = a.width - len(rightStr)
	for _, r := range rightStr {
		if col >= 0 && col < a.width {
			buf[0][col] = texel.Cell{Ch: r, Style: style}
			col++
		}
	}

	return buf
}
