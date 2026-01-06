// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/launcher/launcher.go
// Summary: Implements the app launcher for discovering and launching apps.
// Usage: Shows a list of available apps from the registry; Enter to launch.

package launcher

import (
	"encoding/json"
	"fmt"
	texelcore "github.com/framegrace/texelui/core"
	"log"
	"sort"
	"sync"

	"github.com/framegrace/texelation/internal/theming"
	"github.com/framegrace/texelation/registry"
	"github.com/framegrace/texelation/texel"
	"github.com/framegrace/texelui/adapter"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
	"github.com/gdamore/tcell/v2"
)

// Compile-time interface checks
var _ texelcore.App = (*Launcher)(nil)
var _ texelcore.AppStorageSetter = (*Launcher)(nil)
var _ texelcore.SnapshotProvider = (*Launcher)(nil)
var _ texelcore.ControlBusProvider = (*Launcher)(nil)

// Launcher displays available apps from the registry and allows launching them.
type Launcher struct {
	*adapter.UIApp

	registry   *registry.Registry
	controlBus texelcore.ControlBus
	storage    texelcore.AppStorage

	mu          sync.RWMutex
	apps        []*registry.AppEntry
	usageCounts map[string]int
	selectedIdx int
	labels      []*widgets.Label
	pane        *widgets.Pane

	width, height int
}

// New creates a new launcher app that displays apps from the given registry.
func New(reg *registry.Registry) texelcore.App {
	l := &Launcher{
		registry:    reg,
		usageCounts: make(map[string]int),
		selectedIdx: 0,
		controlBus:  texelcore.NewControlBus(), // Own control bus, no pipeline needed
	}

	// Create TexelUI manager
	ui := core.NewUIManager()
	l.UIApp = adapter.NewUIApp("Launcher", ui)

	// Load apps from registry
	l.loadApps()

	// Note: UI will be built on first Resize() call

	return l
}

// ControlBus returns the launcher's control bus for external registration.
func (l *Launcher) ControlBus() texelcore.ControlBus {
	return l.controlBus
}

// RegisterControl implements texelcore.ControlBusProvider.
// This allows external code to register control handlers on the launcher's bus.
func (l *Launcher) RegisterControl(id, description string, handler func(payload interface{}) error) error {
	return l.controlBus.Register(id, description, texel.ControlHandler(handler))
}

// SetAppStorage implements texelcore.AppStorageSetter.
// This is called by the pane to inject app-level storage.
func (l *Launcher) SetAppStorage(storage texelcore.AppStorage) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.storage = storage

	// Load usage counts from storage
	l.loadUsageCounts()

	// Re-sort apps by usage if already loaded
	if len(l.apps) > 0 {
		l.sortAppsByUsage()
	}
}

// loadUsageCounts loads app usage counts from storage.
// Assumes l.mu is already locked.
func (l *Launcher) loadUsageCounts() {
	if l.storage == nil {
		return
	}

	data, err := l.storage.Get("usageCounts")
	if err != nil || data == nil {
		return
	}

	var counts map[string]int
	if err := json.Unmarshal(data, &counts); err != nil {
		return
	}

	l.usageCounts = counts
}

// saveUsageCounts persists app usage counts to storage.
// Assumes l.mu is already locked.
func (l *Launcher) saveUsageCounts() {
	if l.storage == nil {
		return
	}

	_ = l.storage.Set("usageCounts", l.usageCounts)
}

// sortAppsByUsage sorts apps by usage count (most used first).
// Assumes l.mu is already locked.
func (l *Launcher) sortAppsByUsage() {
	if l.usageCounts == nil {
		return
	}
	sort.SliceStable(l.apps, func(i, j int) bool {
		countI := l.usageCounts[l.apps[i].Manifest.Name]
		countJ := l.usageCounts[l.apps[j].Manifest.Name]
		return countI > countJ
	})
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

	// Sort by usage counts (most used first)
	l.sortAppsByUsage()

	log.Printf("Launcher: Loaded %d apps (sorted by usage)", len(l.apps))
}

// buildUI constructs the TexelUI interface.
// Assumes l.mu is already locked by caller.
func (l *Launcher) buildUI() {
	ui := l.UI()

	// Add background pane
	tm := theming.ForApp("launcher")
	bgColor := tm.GetSemanticColor("bg.surface")

	l.pane = widgets.NewPane()
	l.pane.Style = tcell.StyleDefault.Background(bgColor)
	ui.AddWidget(l.pane)

	// Create labels for each app
	l.labels = make([]*widgets.Label, 0, len(l.apps))

	for i, app := range l.apps {
		// Format: icon + name + description
		text := fmt.Sprintf("%s  %s", app.Manifest.Icon, app.Manifest.DisplayName)
		if app.Manifest.Description != "" {
			text += fmt.Sprintf(" - %s", app.Manifest.Description)
		}

		label := widgets.NewLabel(text)
		label.SetPosition(2, 2+i)
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
	tm := theming.ForApp("launcher")
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
			bus := l.controlBus

			// Track usage for this app (if storage is available)
			if l.usageCounts != nil {
				l.usageCounts[selectedApp.Manifest.Name]++
				l.saveUsageCounts()
			}

			l.mu.Unlock()

			if bus != nil {
				log.Printf("Launcher: Signaling app selection '%s'", selectedApp.Manifest.Name)
				// Trigger control bus event with app name as payload
				if err := bus.Trigger("launcher.select-app", selectedApp.Manifest.Name); err != nil {
					log.Printf("Launcher: Failed to trigger select-app: %v", err)
				}
			} else {
				log.Printf("Launcher: Cannot launch - no control bus attached")
			}
			return
		}
		l.mu.Unlock()
		return

	case tcell.KeyEsc:
		bus := l.controlBus
		l.mu.Unlock()
		if bus != nil {
			log.Printf("Launcher: Signaling close")
			// Trigger control bus event to close launcher
			if err := bus.Trigger("launcher.close", nil); err != nil {
				log.Printf("Launcher: Failed to trigger close: %v", err)
			}
		}
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

// SnapshotMetadata implements texelcore.SnapshotProvider.
// This allows the storage service to use the correct app type.
func (l *Launcher) SnapshotMetadata() (string, map[string]interface{}) {
	return "launcher", nil
}
