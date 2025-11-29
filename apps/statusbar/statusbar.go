// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/statusbar/statusbar.go
// Summary: Implements statusbar capabilities for the status bar application.
// Usage: Added to desktops to render workspace and mode metadata.
// Notes: Works in both local and remote deployments.

package statusbar

import (
	"fmt"
	"strings"
	"sync"
	"texelation/apps/clock"
	"texelation/texel"
	"texelation/texel/theme"

	"github.com/gdamore/tcell/v2"
)

// Powerline characters for creating the tab effect.
// Note: These require a Powerline-patched font or a Nerd Font to render correctly.
const (
	rightTabSeparator     = '' // Left half circle thick separator
	leftTabSeparator      = '' // Right half circle thick separator
	leftLineTabSeparator  = ''
	rightLineTabSeparator = ''
	keyboardIcon          = "  "
	ctrlIcon              = "  "
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
	tm := theme.Get()
	// Base background (Mantle)
	defbgColor := tm.GetSemanticColor("bg.mantle").TrueColor()
	deffgColor := tm.GetSemanticColor("text.primary").TrueColor()

	if a.inControlMode {
		// Control Mode -> Danger (Red)
		defbgColor = tm.GetSemanticColor("action.danger").TrueColor()
		deffgColor = tm.GetSemanticColor("text.inverse").TrueColor()
	}

	styleBase := tcell.StyleDefault.Background(defbgColor).Foreground(deffgColor)
	
	// Active Tab: seamless with desktop (bg.base)
	activeTabBg := a.desktopBgColor.TrueColor() // Should be bg.base
	activeTabFg := tm.GetSemanticColor("text.primary").TrueColor()
	styleActiveTab := tcell.StyleDefault.Background(activeTabBg).Foreground(activeTabFg)

	// Inactive Tab: darker (bg.mantle or bg.crust?)
	inactiveTabBg := tm.GetSemanticColor("bg.crust").TrueColor() 
	inactiveTabFg := tm.GetSemanticColor("text.muted").TrueColor()
	styleInactiveTab := tcell.StyleDefault.Background(inactiveTabBg).Foreground(inactiveTabFg)

	styleActiveTabStart := tcell.StyleDefault.Background(defbgColor).Foreground(activeTabBg)
	styleInactiveTabStart := tcell.StyleDefault.Background(defbgColor).Foreground(inactiveTabBg)

	// Fill the entire bar with the base style first
	for i := 0; i < a.width; i++ {
		buf[0][i] = texel.Cell{Ch: ' ', Style: styleBase}
	}

	// Find the index of the active workspace
	activeIndex := -1
	for i, wsID := range a.allWorkspaces {
		if wsID == a.workspaceID {
			activeIndex = i
			break
		}
	}

	// --- Left-aligned content (Tabs) ---
	// Draw first char
	if activeIndex == 0 {
		buf[0][0] = texel.Cell{Ch: leftTabSeparator, Style: styleActiveTabStart}
	} else {
		buf[0][0] = texel.Cell{Ch: leftTabSeparator, Style: styleInactiveTabStart}
	}
	col := 1
	for i, wsID := range a.allWorkspaces {
		currentIsActive := (i == activeIndex)
		var currentStyle tcell.Style
		if currentIsActive {
			currentStyle = styleActiveTab
		} else {
			currentStyle = styleInactiveTab
		}
		_, currentBg, _ := currentStyle.Decompose()

		// Draw tab text
		wsName := fmt.Sprintf(" %d ", wsID)
		for _, r := range wsName {
			if col < a.width {
				buf[0][col] = texel.Cell{Ch: r, Style: currentStyle}
				col++
			}
		}

		// Draw the separator between this tab and the next one
		if col < a.width {
			var nextStyle tcell.Style
			nextIsActive := (i+1 == activeIndex)

			if i+1 < len(a.allWorkspaces) {
				if nextIsActive {
					nextStyle = styleActiveTab
				} else {
					nextStyle = styleInactiveTab
				}
			} else {
				nextStyle = styleBase
			}
			_, nextBg, _ := nextStyle.Decompose()

			var separatorChar rune
			var separatorStyle tcell.Style

			if currentIsActive {
				// The active tab's right edge cuts into the next tab
				separatorChar = rightTabSeparator
				separatorStyle = tcell.StyleDefault.Foreground(currentBg).Background(nextBg)
			} else if nextIsActive {
				// The next tab (which is active) cuts into the current tab
				separatorChar = leftTabSeparator
				separatorStyle = tcell.StyleDefault.Foreground(nextBg).Background(currentBg)
			} else {
				// Inactive tabs just sit next to each other, separated by the base color
				if i < activeIndex {
					separatorChar = leftLineTabSeparator //' '
					separatorStyle = styleInactiveTab    // styleBase
				} else {
					if i == len(a.allWorkspaces)-1 {
						separatorChar = rightTabSeparator
						separatorStyle = styleInactiveTabStart
					} else {
						separatorChar = rightLineTabSeparator
						separatorStyle = styleInactiveTab // styleBase
					}
				}
			}
			buf[0][col] = texel.Cell{Ch: separatorChar, Style: separatorStyle}
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
	modeStyle := styleBase
	if a.inControlMode {
		if a.subMode != 0 {
			modeStr = fmt.Sprintf(" [CTRL-A, %c, ?] ", a.subMode)
		} else {
			modeStr = ctrlIcon // " [CONTROL] "
		}
	} else {
		modeStr = keyboardIcon //" [INPUT] "
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
