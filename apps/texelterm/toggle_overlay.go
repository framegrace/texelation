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
	hamburger    = '\u2261'  // ≡
)

// overlayToggles returns the ordered list of interactive toggle buttons.
func (a *TexelTerm) overlayToggles() []*widgets.ToggleButton {
	return []*widgets.ToggleButton{a.cfgToggle, a.tfmToggle, a.wrpToggle, a.searchToggle}
}

// updateModeIndicatorsLocked updates toggle button states from current terminal state.
// Must be called with a.mu held.
func (a *TexelTerm) updateModeIndicatorsLocked() {
	if a.vterm == nil {
		return
	}

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

// toggleOverlayExpanded tracks whether the toggle overlay is expanded (mouse hovering).
// This field should be on the TexelTerm struct — added via the existing struct fields.

// collapsedOverlayWidth returns the width of the collapsed pill (caps + hamburger).
func collapsedOverlayWidth() int {
	return 3 // left cap + hamburger + right cap
}

// expandedOverlayWidth returns the width of the expanded pill with all toggle buttons.
func (a *TexelTerm) expandedOverlayWidth() int {
	w := 2 // pill caps
	for _, tb := range a.overlayToggles() {
		bw, _ := tb.Size()
		w += bw
	}
	return w
}

// toggleOverlayRect returns the bounding rect for the toggle overlay.
// Uses expanded or collapsed width based on hover state.
func (a *TexelTerm) toggleOverlayRect(totalCols int) texelcore.Rect {
	var totalW int
	if a.overlayExpanded {
		totalW = a.expandedOverlayWidth()
	} else {
		totalW = collapsedOverlayWidth()
	}
	x := totalCols - totalW
	return texelcore.Rect{X: x, Y: 0, W: totalW, H: 1}
}

// drawToggleOverlay renders the toggle buttons as a pill-shaped overlay at the top-right.
// In collapsed state, shows just a hamburger icon. Expands on hover to show all toggles.
// Must be called with a.mu held.
func (a *TexelTerm) drawToggleOverlay(buf [][]texelcore.Cell, totalCols int) {
	if len(buf) == 0 {
		return
	}
	rect := a.toggleOverlayRect(totalCols)
	if rect.X < 0 {
		return
	}
	totalRows := len(buf)

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

	if !a.overlayExpanded {
		// Collapsed: just pill caps + hamburger
		hamburgerStyle := tcell.StyleDefault.Foreground(tm.GetSemanticColor("text.muted")).Background(overlayBG)
		p.SetCell(rect.X, 0, pillLeftCap, capStyle)
		p.SetCell(rect.X+1, 0, hamburger, hamburgerStyle)
		p.SetCell(rect.X+2, 0, pillRightCap, capStyle)
		return
	}

	// Expanded: pill caps + all toggle buttons
	p.SetCell(rect.X, 0, pillLeftCap, capStyle)

	toggles := a.overlayToggles()
	xx := rect.X + 1
	for _, tb := range toggles {
		w, _ := tb.Size()
		tb.SetPosition(xx, 0)
		xx += w
	}
	for _, tb := range toggles {
		tb.Draw(p)
	}

	p.SetCell(xx, 0, pillRightCap, capStyle)
}

// handleToggleOverlayMouse checks if a mouse event hits the toggle overlay.
// Handles hover expand/collapse and click forwarding.
// Must be called with a.mu held.
func (a *TexelTerm) handleToggleOverlayMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()

	// Check against the EXPANDED rect to detect hover entry,
	// and the current rect for actual hit testing.
	expandedW := a.expandedOverlayWidth()
	expandedX := a.width - expandedW
	expandedRect := texelcore.Rect{X: expandedX, Y: 0, W: expandedW, H: 1}

	currentRect := a.toggleOverlayRect(a.width)

	// Check if mouse is in the expanded zone (for hover expansion)
	inExpandedZone := expandedRect.Contains(x, y)
	inCurrentRect := currentRect.Contains(x, y)

	if !inExpandedZone && !inCurrentRect {
		// Mouse left the overlay area
		if a.overlayExpanded {
			a.overlayExpanded = false
			a.requestRefresh()
		}
		if a.statusBar != nil {
			a.statusBar.ClearHoverHelp()
		}
		return false
	}

	// Mouse is in the overlay zone — expand if collapsed
	if !a.overlayExpanded {
		a.overlayExpanded = true
		a.requestRefresh()
		return true
	}

	// Expanded state: show hover help and handle clicks
	toggles := a.overlayToggles()

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

	for _, tb := range toggles {
		if tb.HandleMouse(ev) {
			a.requestRefresh()
			return true
		}
	}

	a.requestRefresh()
	return true
}
