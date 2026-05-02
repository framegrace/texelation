// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/boot/boot.go
// Summary: Pre-connect splash that owns the tcell screen during the
// supervisor + handshake window.

// Package boot renders a status splash on a pre-initialized tcell
// screen while the texelation supervisor brings the server up and the
// client runtime negotiates its handshake. The splash is intentionally
// simple — no widget tree, no theming, no input handling — because
// step 1 just needs to fill the dead air during a cold-start WAL
// replay (10s+ in the wild) with something legible. Plan F will grow
// this surface into a session picker; the runner already accepts an
// externally-owned screen so the upgrade can swap the App without
// touching the bootstrap.
package boot

import (
	"fmt"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	uicolor "github.com/framegrace/texelui/color"
)

// Stage names a step in the bootstrap pipeline. Stage strings are
// rendered verbatim under the title, so keep them short and stable.
type Stage string

const (
	StageStarting   Stage = "Starting server"
	StageWaiting    Stage = "Waiting for server"
	StageConnecting Stage = "Connecting"
	StageResuming   Stage = "Loading session"
	StageReady      Stage = "Ready"
	StageError      Stage = "Error"
)

// App is the splash render model. It is safe for concurrent reads
// (Render) and writes (SetStage / SetDetail) under its own mutex.
//
// App does not satisfy core.App from texelui — it is deliberately
// lighter so we can avoid pulling in the widget machinery for the
// pre-connect window. Plan F will likely promote this to a real
// texelui App when the session-picker list widget comes online.
type App struct {
	mu        sync.RWMutex
	title     string
	stage     Stage
	detail    string
	startedAt time.Time
	width     int
	height    int
}

// New returns a splash app showing the given window title (used as the
// box header). The first stage is rendered as soon as Render is called.
func New(title string) *App {
	return &App{
		title:     title,
		stage:     StageStarting,
		startedAt: time.Now(),
	}
}

// SetStage updates the headline status. It clears the detail line so a
// stale "(connect failed)" from a previous stage does not leak into
// the new one.
func (a *App) SetStage(s Stage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stage = s
	a.detail = ""
}

// SetDetail attaches a sub-line to the current stage. Use it for
// transient context that wouldn't earn its own Stage constant —
// "(2/3 panes)" during resume, an error string, etc.
func (a *App) SetDetail(detail string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.detail = detail
}

// Detail returns the current detail line. Exposed for tests.
func (a *App) Detail() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.detail
}

// Stage returns the current stage. Exposed for tests + the runner so
// the runner can decide when to stop without holding a render lock.
func (a *App) Stage() Stage {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.stage
}

// SetSize updates the canvas dimensions. The runner calls this on
// every resize event; tests call it directly.
func (a *App) SetSize(w, h int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.width = w
	a.height = h
}

// Frame holds a renderable splash frame: parallel rune + style grids,
// always identical in shape (height rows × width cols). The runner
// pushes them straight into screen.SetContent.
type Frame struct {
	Width  int
	Height int
	Runes  [][]rune
	Styles [][]tcell.Style
}

// RenderFrame produces the current splash frame. Returns a zero-sized
// Frame if the canvas is not yet sized (caller should skip the paint).
func (a *App) RenderFrame() Frame {
	a.mu.RLock()
	w, h := a.width, a.height
	title := a.title
	stage := string(a.stage)
	detail := a.detail
	elapsed := time.Since(a.startedAt)
	a.mu.RUnlock()

	if w <= 0 || h <= 0 {
		return Frame{}
	}

	frame := Frame{Width: w, Height: h}
	frame.Runes = make([][]rune, h)
	frame.Styles = make([][]tcell.Style, h)
	bg := tcell.StyleDefault
	for y := 0; y < h; y++ {
		frame.Runes[y] = make([]rune, w)
		frame.Styles[y] = make([]tcell.Style, w)
		for x := 0; x < w; x++ {
			frame.Runes[y][x] = ' '
			frame.Styles[y][x] = bg
		}
	}

	// Box geometry. The status text drives the inner width.
	// minInnerW = 36 keeps the outer box ≥ 40 cols (4 cols of border +
	// padding wrap the inner area), giving a padded title room to fit;
	// the upper bound is the canvas minus 4 cols of outside margin.
	const minInnerW = 36
	innerW := minInnerW
	if l := len(stage); l > innerW {
		innerW = l
	}
	if l := len(detail); l > innerW {
		innerW = l
	}
	if l := len(title); l > innerW {
		innerW = l
	}
	if maxInner := w - 4; maxInner > 0 && innerW > maxInner {
		innerW = maxInner
	}
	if innerW < 1 {
		return frame
	}

	// 5 inner rows: title, blank, stage, detail, elapsed. Detail is
	// always reserved (rendered blank when empty) so the layout doesn't
	// jitter between stages with and without detail text.
	innerH := 5
	boxW := innerW + 4 // border + padding on each side
	boxH := innerH + 2 // top/bottom border
	if boxW > w {
		boxW = w
	}
	if boxH > h {
		boxH = h
	}
	x0 := (w - boxW) / 2
	y0 := (h - boxH) / 2

	boxStyle := tcell.StyleDefault
	titleStyle := tcell.StyleDefault.Bold(true)
	// Pulse the stage text to convey "this is alive, still working"
	// during long waits like the cold-start WAL replay — see
	// stagePulse for the rate / brightness range. Error and Ready
	// stay static since they're terminal states and a breathing
	// red/green would read agitated rather than steady.
	stageStyle := tcell.StyleDefault.Foreground(stagePulse(elapsed)).Bold(true)
	detailStyle := tcell.StyleDefault.Foreground(tcell.ColorGray)
	elapsedStyle := tcell.StyleDefault.Foreground(tcell.ColorGray).Dim(true)
	if stage == string(StageError) {
		stageStyle = tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
		detailStyle = tcell.StyleDefault.Foreground(tcell.ColorRed)
	}
	if stage == string(StageReady) {
		stageStyle = tcell.StyleDefault.Foreground(tcell.ColorGreen).Bold(true)
	}

	// Box border. tcell line-drawing runes; Style() picks them up
	// regardless of theme.
	drawHLine := func(y, x1, x2 int, style tcell.Style) {
		for x := x1; x <= x2; x++ {
			if y < 0 || y >= h || x < 0 || x >= w {
				continue
			}
			frame.Runes[y][x] = tcell.RuneHLine
			frame.Styles[y][x] = style
		}
	}
	drawVLine := func(x, y1, y2 int, style tcell.Style) {
		for y := y1; y <= y2; y++ {
			if y < 0 || y >= h || x < 0 || x >= w {
				continue
			}
			frame.Runes[y][x] = tcell.RuneVLine
			frame.Styles[y][x] = style
		}
	}
	setRune := func(y, x int, r rune, style tcell.Style) {
		if y < 0 || y >= h || x < 0 || x >= w {
			return
		}
		frame.Runes[y][x] = r
		frame.Styles[y][x] = style
	}

	x1 := x0 + boxW - 1
	y1 := y0 + boxH - 1
	drawHLine(y0, x0+1, x1-1, boxStyle)
	drawHLine(y1, x0+1, x1-1, boxStyle)
	drawVLine(x0, y0+1, y1-1, boxStyle)
	drawVLine(x1, y0+1, y1-1, boxStyle)
	setRune(y0, x0, tcell.RuneULCorner, boxStyle)
	setRune(y0, x1, tcell.RuneURCorner, boxStyle)
	setRune(y1, x0, tcell.RuneLLCorner, boxStyle)
	setRune(y1, x1, tcell.RuneLRCorner, boxStyle)

	contentLeft := x0 + 2
	contentRight := x1 - 2

	drawCenteredText := func(y int, text string, style tcell.Style) {
		if y < 0 || y >= h {
			return
		}
		runes := []rune(text)
		room := contentRight - contentLeft + 1
		if room <= 0 {
			return
		}
		if len(runes) > room {
			runes = runes[:room]
		}
		start := contentLeft + (room-len(runes))/2
		for i, r := range runes {
			setRune(y, start+i, r, style)
		}
	}

	// Inner row layout (relative to y0+1):
	//   0: title
	//   1: blank
	//   2: stage
	//   3: detail
	//   4: elapsed
	drawCenteredText(y0+1, title, titleStyle)
	drawCenteredText(y0+3, stage, stageStyle)
	drawCenteredText(y0+4, detail, detailStyle)

	elapsedText := formatElapsed(elapsed)
	drawCenteredText(y0+5, elapsedText, elapsedStyle)

	return frame
}

// stagePulse resolves the texelui Pulse DynamicColor at elapsed time,
// returning a concrete tcell.Color the splash can paint. Base color
// is a soft cyan; the brightness scales between 0.55 and 1.10 at
// 3 Hz (~333ms cycle) — fast enough to read clearly as motion
// without crossing the line into nervous flicker.
func stagePulse(elapsed time.Duration) tcell.Color {
	dc := uicolor.Pulse(tcell.NewRGBColor(140, 220, 250), 0.55, 1.10, 3.0)
	return dc.Resolve(uicolor.ColorContext{T: float32(elapsed.Seconds())})
}

// formatElapsed renders a duration as "(0.4s)" / "(12s)" / "(1m 04s)"
// — short enough to fit a 36-column box, precise enough to convey
// "is the bootstrap actually progressing?" during a cold start.
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Second:
		tenths := int(d / (100 * time.Millisecond))
		return fmt.Sprintf("(0.%ds)", tenths)
	case d < time.Minute:
		return fmt.Sprintf("(%ds)", int(d/time.Second))
	default:
		mins := int(d / time.Minute)
		secs := int((d % time.Minute) / time.Second)
		return fmt.Sprintf("(%dm %02ds)", mins, secs)
	}
}
