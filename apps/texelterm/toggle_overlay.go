package texelterm

import (
	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
	"github.com/framegrace/texelui/widgets"
	"github.com/gdamore/tcell/v2"
)

// Powerline rounded cap characters for pill shape.
const (
	pillLeftCap  = '\uE0B6' //
	pillRightCap = '\uE0B4' //
	pillSep      = '│'      // separator between interactive and indicator groups
)

// overlayToggles returns the ordered list of toggle buttons for the overlay.
// Interactive (toggleable) buttons come first, then read-only indicators.
func (a *TexelTerm) overlayToggles() []*widgets.ToggleButton {
	return []*widgets.ToggleButton{a.cfgToggle, a.tfmToggle, a.wrpToggle, a.searchToggle, a.tuiToggle, a.altToggle}
}

// overlaySepIndex returns the index in overlayToggles() where the separator
// should be drawn (after the interactive group, before the indicator group).
func (a *TexelTerm) overlaySepIndex() int {
	return 4 // after cfg, tfm, wrp, search; before tui, alt
}

// updateModeIndicatorsLocked updates toggle button states from current terminal state.
// Must be called with a.mu held.
func (a *TexelTerm) updateModeIndicatorsLocked() {
	if a.vterm == nil {
		return
	}

	// TUI/NRM - TUI detection (active = TUI detected)
	a.tuiToggle.Active = a.vterm.IsInTUIMode()

	// ALT - alt screen
	a.altToggle.Active = a.vterm.InAltScreen()

	// TFM - transformer pipeline (disabled + forced off during TUI or alt screen)
	tfmOverride := a.vterm.IsInTUIMode() || a.vterm.InAltScreen()
	if tfmOverride {
		a.tfmToggle.Disabled = true
		a.tfmToggle.Active = false
		if a.pipeline != nil && a.pipeline.Enabled() {
			a.pipeline.SetEnabled(false)
			a.vterm.SetShowOverlay(false)
			a.vterm.MarkAllDirty()
		}
	} else {
		a.tfmToggle.Disabled = false
		a.tfmToggle.Active = a.tfmUserPref
		if a.pipeline != nil && a.pipeline.Enabled() != a.tfmUserPref {
			a.pipeline.SetEnabled(a.tfmUserPref)
			a.vterm.SetShowOverlay(a.tfmUserPref)
			a.vterm.MarkAllDirty()
		}
	}

	// WRP - wrap (disabled + forced off during TUI or alt screen)
	if tfmOverride {
		a.wrpToggle.Disabled = true
		a.wrpToggle.Active = false
		if a.vterm.WrapEnabled() {
			a.vterm.SetWrapEnabled(false)
		}
	} else {
		a.wrpToggle.Disabled = false
		a.wrpToggle.Active = a.wrpUserPref
		if a.vterm.WrapEnabled() != a.wrpUserPref {
			a.vterm.SetWrapEnabled(a.wrpUserPref)
		}
	}

	// Search - active when history navigator is visible
	a.searchToggle.Active = a.historyNavigator != nil && a.historyNavigator.IsVisible()

	// Config - active when config panel is visible
	a.cfgToggle.Active = a.configPanel != nil && a.configPanel.IsVisible()
}

// toggleOverlayRect returns the bounding rect for the toggle button overlay
// at the top-right of the terminal, including pill caps. Must be called with a.mu held.
func (a *TexelTerm) toggleOverlayRect(totalCols int) texelcore.Rect {
	totalW := 0
	for _, tb := range a.overlayToggles() {
		w, _ := tb.Size()
		totalW += w
	}
	totalW += 2 // pill caps
	totalW += 1 // separator between interactive and indicator groups
	x := totalCols - totalW
	return texelcore.Rect{X: x, Y: 0, W: totalW, H: 1}
}

// drawToggleOverlay renders the toggle buttons as a pill-shaped overlay at the top-right.
// Skips drawing if the terminal is too narrow to fit the overlay.
// Must be called with a.mu held.
func (a *TexelTerm) drawToggleOverlay(buf [][]texelcore.Cell, totalCols int) {
	if len(buf) == 0 {
		return
	}
	rect := a.toggleOverlayRect(totalCols)
	// Don't draw if terminal is too narrow (overlay needs margin + width + 1 char gap)
	if rect.X < 0 {
		return
	}
	totalRows := len(buf)

	// Get colors for pill caps: fg = overlay BG (bg.surface), bg = terminal BG (bg.base)
	tm := theme.Get()
	overlayBG := tm.GetSemanticColor("bg.surface")
	terminalBG := tm.GetSemanticColor("bg.base")
	if overlayBG == tcell.ColorDefault {
		overlayBG = tcell.ColorBlack
	}
	if terminalBG == tcell.ColorDefault {
		terminalBG = tcell.ColorBlack
	}
	capStyle := tcell.StyleDefault.Foreground(overlayBG).Background(terminalBG)

	p := texelcore.NewPainter(buf, texelcore.Rect{X: 0, Y: 0, W: totalCols, H: totalRows})

	// Draw left pill cap
	p.SetCell(rect.X, 0, pillLeftCap, capStyle)

	// Position and draw each toggle button (after left cap), with separator between groups
	toggles := a.overlayToggles()
	sepIdx := a.overlaySepIndex()
	sepStyle := tcell.StyleDefault.Foreground(tm.GetSemanticColor("text.muted")).Background(overlayBG)
	xx := rect.X + 1
	for i, tb := range toggles {
		if i == sepIdx {
			p.SetCell(xx, 0, pillSep, sepStyle)
			xx++
		}
		w, _ := tb.Size()
		tb.SetPosition(xx, 0)
		xx += w
	}
	for _, tb := range toggles {
		tb.Draw(p)
	}

	// Draw right pill cap
	p.SetCell(xx, 0, pillRightCap, capStyle)
}

// handleToggleOverlayMouse checks if a mouse event hits the toggle overlay.
// Returns true if the event was consumed by the overlay.
// Must be called with a.mu held.
func (a *TexelTerm) handleToggleOverlayMouse(ev *tcell.EventMouse) bool {
	overlayRect := a.toggleOverlayRect(a.width)
	if overlayRect.X < 0 {
		return false
	}

	x, y := ev.Position()
	if !overlayRect.Contains(x, y) {
		if a.statusBar != nil {
			a.statusBar.ClearHoverHelp()
		}
		return false
	}

	toggles := a.overlayToggles()

	// Show hover help for the hovered button
	helpFound := false
	for _, tb := range toggles {
		if tb.HitTest(x, y) {
			if ht := tb.HelpText(); ht != "" && a.statusBar != nil {
				a.statusBar.SetHoverHelp(ht)
				helpFound = true
			}
			break
		}
	}
	if !helpFound && a.statusBar != nil {
		a.statusBar.ClearHoverHelp()
	}

	// Forward click events to buttons
	for _, tb := range toggles {
		if tb.HandleMouse(ev) {
			a.requestRefresh()
			return true
		}
	}

	a.requestRefresh()
	return true
}
