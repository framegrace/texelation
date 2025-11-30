// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/launcher/launcher.go
// Summary: Implements the app launcher for discovering and launching apps.
// Usage: Shows a list of available apps from the registry; Enter to launch.

package launcher

import (
	"fmt"
	"log"
	"sync"

	"github.com/gdamore/tcell/v2"
	"texelation/registry"
	"texelation/texel"
	"texelation/texel/cards"
	"texelation/texel/theme"
	"texelation/texelui/adapter"
	"texelation/texelui/core"
	"texelation/texelui/widgets"
)

// Launcher displays available apps from the registry and allows launching them.
type Launcher struct {
	*adapter.UIApp

	registry   *registry.Registry
	replacer   texel.AppReplacer

	mu           sync.RWMutex
	apps         []*registry.AppEntry
	selectedIdx  int
	labels       []*widgets.Label
	pane         *widgets.Pane

	width, height int
}

// New creates a new launcher app that displays apps from the given registry.
func New(reg *registry.Registry) texel.App {
	l := &Launcher{
		registry:    reg,
		selectedIdx: 0,
	}

	// Create TexelUI manager
	ui := core.NewUIManager()
	l.UIApp = adapter.NewUIApp("Launcher", ui)

	// Load apps from registry
	l.loadApps()

	// Note: UI will be built on first Resize() call

	// Wrap in pipeline for effects support
	return cards.NewPipeline(nil, cards.WrapApp(l))
}

// SetReplacer implements ReplacerReceiver to receive the replacer from the pane.
func (l *Launcher) SetReplacer(replacer texel.AppReplacer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.replacer = replacer
	log.Printf("Launcher: Replacer set")
}

// loadApps fetches the list of apps from the registry.
func (l *Launcher) loadApps() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.registry == nil {
		log.Printf("Launcher: No registry available")
		l.apps = []*registry.AppEntry{}
		return
	}

	l.apps = l.registry.List()
	log.Printf("Launcher: Loaded %d apps", len(l.apps))
}

// buildUI constructs the TexelUI interface.
// Assumes l.mu is already locked by caller.
func (l *Launcher) buildUI() {
	ui := l.UI()

	// Add background pane
	tm := theme.Get()
	bgColor := tm.GetSemanticColor("bg.surface")
	style := tcell.StyleDefault.Background(bgColor)

	l.pane = widgets.NewPane(0, 0, 1, 1, style)
	ui.AddWidget(l.pane)

	// Create labels for each app
	l.labels = make([]*widgets.Label, 0, len(l.apps))

	for i, app := range l.apps {
		// Format: icon + name + description
		text := fmt.Sprintf("%s  %s", app.Manifest.Icon, app.Manifest.DisplayName)
		if app.Manifest.Description != "" {
			text += fmt.Sprintf(" - %s", app.Manifest.Description)
		}

		label := widgets.NewLabel(2, 2+i, 0, 1, text)
		label.SetFocusable(true)
		l.labels = append(l.labels, label)
		ui.AddWidget(label)
	}

	// Focus first app if available
	if len(l.labels) > 0 {
		ui.Focus(l.labels[0])
	}

	l.updateSelection()
}

// updateSelection updates the visual style of the selected app.
func (l *Launcher) updateSelection() {
	tm := theme.Get()
	normalFg := tm.GetSemanticColor("text.primary")
	normalBg := tm.GetSemanticColor("bg.surface")
	selectedFg := tm.GetSemanticColor("text.inverse")
	selectedBg := tm.GetSemanticColor("accent.primary")

	for i, label := range l.labels {
		if i == l.selectedIdx {
			label.Style = tcell.StyleDefault.Foreground(selectedFg).Background(selectedBg)
		} else {
			label.Style = tcell.StyleDefault.Foreground(normalFg).Background(normalBg)
		}
	}
}

// Resize handles pane resizing.
func (l *Launcher) Resize(cols, rows int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.width, l.height = cols, rows

	// Build UI on first resize if not already built
	if l.pane == nil && cols > 0 && rows > 0 {
		l.buildUI()
	}

	// Call parent resize
	l.UIApp.Resize(cols, rows)

	// Resize background pane
	if l.pane != nil {
		l.pane.SetPosition(0, 0)
		l.pane.Resize(cols, rows)
	}

	// Update label positions and widths
	for i, label := range l.labels {
		label.SetPosition(2, 2+i)
		label.Resize(cols-4, 1)
	}
}

// HandleKey handles keyboard input for navigation and launching.
func (l *Launcher) HandleKey(ev *tcell.EventKey) {
	l.mu.Lock()

	switch ev.Key() {
	case tcell.KeyUp:
		if l.selectedIdx > 0 {
			l.selectedIdx--
			if l.UIApp != nil && l.selectedIdx < len(l.labels) {
				l.UI().Focus(l.labels[l.selectedIdx])
			}
			l.updateSelection()
		}
		l.mu.Unlock()
		return

	case tcell.KeyDown:
		if l.selectedIdx < len(l.apps)-1 {
			l.selectedIdx++
			if l.UIApp != nil && l.selectedIdx < len(l.labels) {
				l.UI().Focus(l.labels[l.selectedIdx])
			}
			l.updateSelection()
		}
		l.mu.Unlock()
		return

	case tcell.KeyEnter:
		if l.selectedIdx >= 0 && l.selectedIdx < len(l.apps) {
			selectedApp := l.apps[l.selectedIdx]
			replacer := l.replacer
			l.mu.Unlock()

			if replacer != nil {
				log.Printf("Launcher: Launching app '%s'", selectedApp.Manifest.Name)
				replacer.ReplaceWithApp(selectedApp.Manifest.Name, nil)
			} else {
				log.Printf("Launcher: Cannot launch - no replacer set")
			}
			return
		}
		l.mu.Unlock()
		return
	}

	l.mu.Unlock()

	// Pass to UI manager for other keys (if initialized)
	if l.UIApp != nil {
		l.UIApp.HandleKey(ev)
	}
}

// GetTitle returns the launcher title.
func (l *Launcher) GetTitle() string {
	return "Launcher"
}
