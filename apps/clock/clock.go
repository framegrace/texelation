// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/clock/clock.go
// Summary: Implements clock capabilities for the clock status widget.
// Usage: Loaded into the desktop chrome to display time information.
// Notes: Uses shared texel primitives for drawing.

package clock

import (
	"fmt"
	"sync"
	"time"

	texelcore "github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth" // Added for correct wide-character handling
)

const (
	// Using the Nerd Font clock icon
	timePrefix = ""
)

type clockApp struct {
	width, height int
	currentTime   string
	mu            sync.RWMutex
	stop          chan struct{}
	refreshChan   chan<- bool
	buf           [][]texelcore.Cell
}

func NewClockApp() texelcore.App {
	return &clockApp{
		stop: make(chan struct{}),
	}
}

func (a *clockApp) HandleKey(ev *tcell.EventKey) {}

func (a *clockApp) SetRefreshNotifier(refreshChan chan<- bool) {
	a.refreshChan = refreshChan
}

func (a *clockApp) Run() error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	updateTime := func() {
		a.mu.Lock()
		a.currentTime = time.Now().Format("15:04:05")
		a.mu.Unlock()
	}
	updateTime()

	for {
		select {
		case <-ticker.C:
			updateTime()
			if a.refreshChan != nil {
				select {
				case a.refreshChan <- true:
				default:
				}
			}
		case <-a.stop:
			return nil
		}
	}
}

func (a *clockApp) Stop() {
	close(a.stop)
}

func (a *clockApp) Resize(cols, rows int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.width, a.height = cols, rows
}

func (a *clockApp) Render() [][]texelcore.Cell {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.width <= 0 || a.height <= 0 {
		return [][]texelcore.Cell{}
	}

	if len(a.buf) != a.height || (a.height > 0 && len(a.buf[0]) != a.width) {
		a.buf = make([][]texelcore.Cell, a.height)
		for y := 0; y < a.height; y++ {
			a.buf[y] = make([]texelcore.Cell, a.width)
		}
	}

	for i := range a.buf {
		for j := range a.buf[i] {
			a.buf[i][j] = texelcore.Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	}

	style := tcell.StyleDefault.Foreground(tcell.PaletteColor(6))

	str := fmt.Sprintf(timePrefix+"%s", a.currentTime)
	y := a.height / 2

	// Corrected: Use runewidth.StringWidth to get the correct visual width
	stringVisualWidth := runewidth.StringWidth(str)
	x := (a.width - stringVisualWidth) / 2

	if y < a.height && x >= 0 {
		// Corrected: Manually iterate and advance the column based on rune width
		col := x
		for _, ch := range str {
			if col < a.width {
				a.buf[y][col] = texelcore.Cell{Ch: ch, Style: style}
				col += runewidth.RuneWidth(ch) // Advance by the character's actual width
			}
		}
	}

	return a.buf
}

func (a *clockApp) GetTitle() string {
	return "Clock"
}
