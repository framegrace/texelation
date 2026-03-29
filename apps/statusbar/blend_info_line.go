// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/statusbar/blend_info_line.go
// Summary: Custom widget for the status bar's second row — gradient blend line
// with overlaid text (mode icon, title, fps, clock) and toast notifications.

package statusbar

import (
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/framegrace/texelation/internal/theming"
	"github.com/framegrace/texelation/texel"
	"github.com/framegrace/texelui/color"
	"github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"
)

// BlendInfoLine is a single-row widget that renders a gradient background
// (accentColor → accentColor → contentBG) and overlays left/right text.
// In toast mode the gradient uses a severity color and the message replaces
// the normal content.
type BlendInfoLine struct {
	core.BaseWidget
	mu  sync.RWMutex
	inv func(core.Rect)

	// Colors
	accentColor tcell.Color
	contentBG   tcell.Color

	// Normal-mode content
	inControlMode bool
	subMode       rune
	title         string
	fpsActual     float64
	fpsTheo       float64
	clock         string

	// Toast state
	toastMessage  string
	toastSeverity texel.ToastSeverity
	toastExpiry   time.Time
	toastActive   bool
}

// NewBlendInfoLine creates a BlendInfoLine with sensible defaults from the theme.
func NewBlendInfoLine() *BlendInfoLine {
	tm := theming.ForApp("statusbar")
	bil := &BlendInfoLine{
		accentColor: tm.GetSemanticColor("accent.primary"),
		contentBG:   tm.GetSemanticColor("bg.base"),
	}
	bil.Resize(1, 1)
	return bil
}

// SetInvalidator satisfies core.InvalidationAware so the UI manager can wire
// dirty-region callbacks.
func (bil *BlendInfoLine) SetInvalidator(fn func(core.Rect)) {
	bil.mu.Lock()
	bil.inv = fn
	bil.mu.Unlock()
}

// invalidate fires the invalidation callback when state changes.
func (bil *BlendInfoLine) invalidate() {
	if bil.inv != nil {
		bil.inv(bil.Rect)
	}
}

// SetAccentColor sets the gradient's left-side accent color.
func (bil *BlendInfoLine) SetAccentColor(c tcell.Color) {
	bil.mu.Lock()
	bil.accentColor = c
	bil.mu.Unlock()
	bil.invalidate()
}

// SetMode sets the keyboard/control mode indicator.
func (bil *BlendInfoLine) SetMode(controlMode bool, subMode rune) {
	bil.mu.Lock()
	bil.inControlMode = controlMode
	bil.subMode = subMode
	bil.mu.Unlock()
	bil.invalidate()
}

// SetTitle sets the active pane title shown on the left side.
func (bil *BlendInfoLine) SetTitle(title string) {
	bil.mu.Lock()
	bil.title = title
	bil.mu.Unlock()
	bil.invalidate()
}

// SetFPS sets the actual and theoretical fps values for the right-side display.
func (bil *BlendInfoLine) SetFPS(actual, theoretical float64) {
	bil.mu.Lock()
	bil.fpsActual = actual
	bil.fpsTheo = theoretical
	bil.mu.Unlock()
	bil.invalidate()
}

// SetClock sets the clock string for the right-side display.
func (bil *BlendInfoLine) SetClock(t string) {
	bil.mu.Lock()
	bil.clock = t
	bil.mu.Unlock()
	bil.invalidate()
}

// ShowToast activates toast mode, replacing normal content with the given
// message for the specified duration.
func (bil *BlendInfoLine) ShowToast(message string, severity texel.ToastSeverity, duration time.Duration) {
	bil.mu.Lock()
	bil.toastMessage = message
	bil.toastSeverity = severity
	bil.toastExpiry = time.Now().Add(duration)
	bil.toastActive = true
	bil.mu.Unlock()
	bil.invalidate()
}

// DismissToast deactivates toast mode immediately.
func (bil *BlendInfoLine) DismissToast() {
	bil.mu.Lock()
	bil.toastActive = false
	bil.mu.Unlock()
	bil.invalidate()
}

// isToastActive returns true when a toast is active and has not expired.
// Must be called with mu held (at least RLock).
func (bil *BlendInfoLine) isToastActive() bool {
	if !bil.toastActive {
		return false
	}
	if time.Now().After(bil.toastExpiry) {
		// Expire in-place (we hold RLock, caller should promote to write if they
		// want to persist the change; here we just report expiry without mutation).
		return false
	}
	return true
}

// Draw renders the gradient background and overlaid text into the painter.
func (bil *BlendInfoLine) Draw(painter *core.Painter) {
	bil.mu.Lock()
	// Auto-expire toast while holding write lock.
	if bil.toastActive && time.Now().After(bil.toastExpiry) {
		bil.toastActive = false
	}

	x, y := bil.Rect.X, bil.Rect.Y
	w := bil.Rect.W

	toastActive := bil.toastActive
	accent := bil.accentColor
	contentBG := bil.contentBG
	inControl := bil.inControlMode
	subMode := bil.subMode
	title := bil.title
	fpsActual := bil.fpsActual
	fpsTheo := bil.fpsTheo
	clock := bil.clock
	toastMsg := bil.toastMessage
	toastSev := bil.toastSeverity
	bil.mu.Unlock()

	if w <= 0 {
		return
	}

	tm := theming.ForApp("statusbar")

	// Choose the accent color for the gradient.
	gradAccent := accent
	if toastActive {
		switch toastSev {
		case texel.ToastSuccess:
			gradAccent = tm.GetSemanticColor("action.success")
		case texel.ToastWarning:
			gradAccent = tm.GetSemanticColor("action.warning")
		case texel.ToastError:
			gradAccent = tm.GetSemanticColor("action.danger")
		default: // ToastInfo
			gradAccent = accent
		}
	}

	// Build gradient: accent → accent (30%) → contentBG.
	grad := color.Linear(
		0,
		color.Stop(0, gradAccent),
		color.Stop(0.3, gradAccent),
		color.Stop(1, contentBG),
	).WithLocal().Build()

	bgStyle := color.DynamicStyle{FG: color.Solid(tcell.ColorDefault), BG: grad}

	// Fill the row with the gradient background.
	painter.SetWidgetRect(bil.Rect)
	for col := x; col < x+w; col++ {
		painter.SetDynamicCell(col, y, ' ', bgStyle)
	}

	// Resolve text colors.
	darkFG := tm.GetSemanticColor("text.inverse")
	if darkFG == tcell.ColorDefault {
		darkFG = tcell.NewRGBColor(30, 30, 46)
	}
	mutedFG := tm.GetSemanticColor("text.muted")
	if mutedFG == tcell.ColorDefault {
		mutedFG = tm.GetSemanticColor("text.secondary")
	}

	darkDS := color.DynamicStyle{FG: color.Solid(darkFG), BG: color.Solid(tcell.ColorDefault)}
	mutedDS := color.DynamicStyle{FG: color.Solid(mutedFG), BG: color.Solid(tcell.ColorDefault)}

	if toastActive {
		// Toast mode: show message on the left, truncate to fit.
		msg := " " + toastMsg
		col := x
		for _, r := range msg {
			if col >= x+w {
				break
			}
			painter.SetDynamicCellKeepBG(col, y, r, darkDS)
			col++
		}
		return
	}

	// Normal mode: left = mode icon + title, right = fps + clock.

	// --- Left side ---
	var modeIcon string
	if inControl {
		if subMode != 0 {
			modeIcon = fmt.Sprintf(" [CTRL-A,%c,?] ", subMode)
		} else {
			modeIcon = ctrlIcon
		}
	} else {
		modeIcon = keyboardIcon
	}
	leftStr := " " + modeIcon + title + " "

	// --- Right side ---
	var fpsStr string
	if fpsTheo > 0 {
		fpsStr = fmt.Sprintf("%d/%d", int(fpsActual+0.5), int(fpsTheo+0.5))
	} else {
		fpsStr = fmt.Sprintf("%d", int(fpsActual+0.5))
	}
	rightStr := fmt.Sprintf(" %s fps  %s ", fpsStr, clock)
	rightWidth := utf8.RuneCountInString(rightStr)

	// Draw left text (stop before right-side zone).
	col := x
	limit := x + w
	if rightWidth < w {
		limit = x + w - rightWidth
	}
	for _, r := range leftStr {
		if col >= limit {
			break
		}
		painter.SetDynamicCellKeepBG(col, y, r, darkDS)
		col++
	}

	// Draw right text.
	if rightWidth <= w {
		col = x + w - rightWidth
		for _, r := range rightStr {
			if col >= x+w {
				break
			}
			painter.SetDynamicCellKeepBG(col, y, r, mutedDS)
			col++
		}
	}
}
