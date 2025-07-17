package statusbar

import (
	"fmt"
	"strings"
	"sync"
	"texelation/apps/clock"
	"texelation/texel"

	"github.com/gdamore/tcell/v2"
)

// Powerline characters for creating the tab effect.
// Note: These require a Powerline-patched font or a Nerd Font to render correctly.
const (
	rightTabSeparator     = '' // Left half circle thick separator
	leftTabSeparator      = '' // Right half circle thick separator
	leftTabLineSeparator  = ''
	rightTabLineSeparator = ''
)

// StatusBarApp displays screen state information.
type StatusBarApp struct {
	width, height int
	mu            sync.RWMutex
	refreshChan   chan<- bool

	// State from Desktop
	allWorkspaces  []int
	workspaceID    int
	inControlMode  bool
	subMode        rune
	activeTitle    string
	desktopBgColor tcell.Color

	// Internal Clock
	clockApp  texel.App
	stopClock chan struct{}
}

// New creates a new StatusBarApp.
func New() texel.App {
	return &StatusBarApp{
		clockApp:      clock.NewClockApp(),
		stopClock:     make(chan struct{}),
		allWorkspaces: []int{1}, // Default to 1 workspace initially
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
			a.allWorkspaces = payload.AllWorkspaces
			a.workspaceID = payload.WorkspaceID
			a.inControlMode = payload.InControlMode
			a.subMode = payload.SubMode
			a.activeTitle = payload.ActiveTitle
			a.desktopBgColor = payload.DesktopBgColor
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

	// Define color schemes
	styleBase := tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite)
	// Active tab has desktop background, making it look "cut out"
	styleActiveTab := tcell.StyleDefault.Background(a.desktopBgColor).Foreground(tcell.ColorWhite)
	// Inactive tab is a darker gray
	styleInactiveTab := tcell.StyleDefault.Background(tcell.ColorGray).Foreground(tcell.ColorBlack)
	styleControlMode := tcell.StyleDefault.Background(tcell.ColorSaddleBrown).Foreground(tcell.ColorWhite)

	// Fill the entire bar with the base style first
	for i := 0; i < a.width; i++ {
		buf[0][i] = texel.Cell{Ch: ' ', Style: styleBase}
	}

	// --- Left-aligned content (Tabs) ---
	col := 0
	for _, wsID := range a.allWorkspaces {
		isCurrentWs := (wsID == a.workspaceID)

		var tabStyle tcell.Style
		if isCurrentWs {
			tabStyle = styleActiveTab
		} else {
			tabStyle = styleInactiveTab
		}
		// Corrected: Use Decompose to get colors from a style
		_, tabBg, _ := tabStyle.Decompose()
		_, baseBg, _ := styleBase.Decompose()
		separatorStyle := tcell.StyleDefault.Foreground(tabBg).Background(baseBg)

		// Draw left separator
		if col < a.width {
			buf[0][col] = texel.Cell{Ch: leftTabSeparator, Style: separatorStyle}
			col++
		}

		// Draw tab text
		wsName := fmt.Sprintf(" %d ", wsID)
		for _, r := range wsName {
			if col < a.width {
				buf[0][col] = texel.Cell{Ch: r, Style: tabStyle}
				col++
			}
		}

		// Draw right separator
		if col < a.width && isCurrentWs {
			buf[0][col] = texel.Cell{Ch: rightTabSeparator, Style: separatorStyle}
			col++
		}

		// Add a space between tabs
		if col < a.width {
			buf[0][col] = texel.Cell{Ch: ' ', Style: styleBase}
			col++
		}
	}

	tabsEndCol := col

	// --- Right-aligned content (Clock) ---
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
	rightCol := a.width - len(rightStr)

	// Draw right-aligned string, ensuring it doesn't overwrite other content
	if rightCol > tabsEndCol {
		for i, r := range rightStr {
			buf[0][rightCol+i] = texel.Cell{Ch: r, Style: styleBase}
		}
	}

	// --- Center-aligned content (Mode & Title) ---
	var modeStr string
	var modeStyle tcell.Style
	if a.inControlMode {
		if a.subMode != 0 {
			modeStr = fmt.Sprintf(" [CTRL-A, %c, ?] ", a.subMode)
		} else {
			modeStr = " [CONTROL] "
		}
		modeStyle = styleControlMode
	} else {
		modeStr = " [INPUT] "
		modeStyle = styleBase
	}
	titleStr := fmt.Sprintf(" %s ", a.activeTitle)

	// Draw mode string, starting after the tabs with some padding
	centerCol := tabsEndCol + 2
	for _, r := range modeStr {
		if centerCol < a.width && centerCol < rightCol {
			buf[0][centerCol] = texel.Cell{Ch: r, Style: modeStyle}
			centerCol++
		}
	}
	// Draw title string right after the mode string
	for _, r := range titleStr {
		if centerCol < a.width && centerCol < rightCol {
			buf[0][centerCol] = texel.Cell{Ch: r, Style: styleBase}
			centerCol++
		}
	}

	return buf
}
