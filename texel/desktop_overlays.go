// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/desktop_overlays.go
// Summary: Overlay management and launcher/help/config dialogs for the desktop engine.

package texel

import "strings"

// ShowFloatingPanel opens a modal floating panel hosting the given app.
func (d *DesktopEngine) ShowFloatingPanel(app App, x, y, w, h int) {
	if app == nil {
		return
	}

	// Check if app is already floating? Maybe not needed for now.

	panel := &FloatingPanel{
		app:    app,
		x:      x,
		y:      y,
		width:  w,
		height: h,
		modal:  true,
		id:     newFloatingPanelID(app),
	}

	// Get pipeline for events and rendering
	if provider, ok := app.(PipelineProvider); ok {
		panel.pipeline = provider.Pipeline()
	}

	d.floatingPanels = append(d.floatingPanels, panel)

	if listener, ok := app.(Listener); ok {
		d.Subscribe(listener)
	}

	// Wire refresh notifier to pipeline (or app as fallback)
	notifier := d.makeRefreshNotifier()
	if panel.pipeline != nil {
		panel.pipeline.SetRefreshNotifier(notifier)
	} else {
		app.SetRefreshNotifier(notifier)
	}

	// Inject app-level storage for floating panels (they don't have pane IDs)
	if d.storage != nil {
		appType := "unknown"
		if provider, ok := app.(SnapshotProvider); ok {
			appType, _ = provider.SnapshotMetadata()
		}
		if setter, ok := app.(AppStorageSetter); ok {
			setter.SetAppStorage(d.storage.AppStorage(appType))
		}
	}

	d.appLifecycle.StartApp(app, nil)
	// Resize pipeline (or app as fallback)
	if panel.pipeline != nil {
		panel.pipeline.Resize(w, h)
	} else {
		app.Resize(w, h)
	}

	d.notifyPaneState(panel.id, true, false, ZOrderFloating, false)

	d.recalculateLayout()
	d.broadcastTreeChanged()
	// d.broadcastStateUpdate() // TODO: Notify focus change if we focus the panel?
}

// CloseFloatingPanel removes a floating panel.
func (d *DesktopEngine) CloseFloatingPanel(panel *FloatingPanel) {
	if panel == nil {
		return
	}

	found := false
	for i, p := range d.floatingPanels {
		if p == panel {
			d.floatingPanels = append(d.floatingPanels[:i], d.floatingPanels[i+1:]...)
			found = true
			break
		}
	}

	if found {
		d.appLifecycle.StopApp(panel.app)
		d.recalculateLayout()
		d.broadcastTreeChanged()
	}
}

// closeFloatingPanelByApp finds and closes the floating panel hosting the given app.
func (d *DesktopEngine) closeFloatingPanelByApp(app App) {
	var panel *FloatingPanel
	for _, fp := range d.floatingPanels {
		if fp.app == app {
			panel = fp
			break
		}
	}
	if panel != nil {
		d.CloseFloatingPanel(panel)
	}
}

func (d *DesktopEngine) launchLauncherOverlay() {
	// Check if already open
	for _, fp := range d.floatingPanels {
		if fp.app.GetTitle() == "Launcher" {
			d.CloseFloatingPanel(fp)
			return
		}
	}

	appInstance := d.registry.CreateApp("launcher", nil)
	app, ok := appInstance.(App)
	if !ok {
		return
	}

	// Register control bus handlers if the app provides a control bus
	if provider, ok := app.(ControlBusProvider); ok {
		// Register handler for app selection
		provider.RegisterControl("launcher.select-app", "Launch selected app in active pane", func(payload interface{}) error {
			appName, ok := payload.(string)
			if !ok {
				return nil
			}

			// Close the launcher floating panel
			d.closeFloatingPanelByApp(app)

			// Launch the selected app in the active pane
			if ws := d.ActiveWorkspace(); ws != nil {
				if pane := ws.ActivePane(); pane != nil {
					pane.ReplaceWithApp(appName, nil)
				}
			}
			return nil
		})

		// Register handler for launcher close
		provider.RegisterControl("launcher.close", "Close launcher overlay", func(payload interface{}) error {
			d.closeFloatingPanelByApp(app)
			return nil
		})
	}

	vw, vh := d.viewportSize()
	w := 60
	h := 20
	if w > vw {
		w = vw - 2
	}
	if h > vh {
		h = vh - 2
	}
	x := (vw - w) / 2
	y := (vh - h) / 2

	d.ShowFloatingPanel(app, x, y, w, h)
}

func (d *DesktopEngine) launchHelpOverlay() {
	// Check if already open
	for _, fp := range d.floatingPanels {
		if fp.app.GetTitle() == "Help" {
			d.CloseFloatingPanel(fp)
			return
		}
	}

	appInstance := d.registry.CreateApp("help", nil)
	app, ok := appInstance.(App)
	if !ok {
		return
	}

	// Register control bus handlers if the app provides a control bus
	if provider, ok := app.(ControlBusProvider); ok {
		// Register handler for help close
		provider.RegisterControl("help.close", "Close help overlay", func(payload interface{}) error {
			d.closeFloatingPanelByApp(app)
			return nil
		})
	}

	vw, vh := d.viewportSize()
	w := 60
	h := 30 // Help needs more height
	if w > vw {
		w = vw - 2
	}
	if h > vh {
		h = vh - 2
	}
	x := (vw - w) / 2
	y := (vh - h) / 2

	d.ShowFloatingPanel(app, x, y, w, h)
}

func (d *DesktopEngine) activeAppTarget() string {
	if d.activeWorkspace == nil {
		return ""
	}
	pane := d.activeWorkspace.ActivePane()
	if pane == nil || pane.app == nil {
		return ""
	}
	if provider, ok := pane.app.(SnapshotProvider); ok {
		appType, _ := provider.SnapshotMetadata()
		return appType
	}
	return ""
}

func (d *DesktopEngine) launchConfigEditorOverlay(target string) {
	for _, fp := range d.floatingPanels {
		if fp.app.GetTitle() == "Config Editor" {
			d.CloseFloatingPanel(fp)
			return
		}
	}

	appInstance := d.registry.CreateApp("config-editor", nil)
	app, ok := appInstance.(App)
	if !ok {
		return
	}

	if setter, ok := app.(interface{ SetDefaultTarget(string) }); ok && target != "" {
		setter.SetDefaultTarget(target)
	}

	if provider, ok := app.(ControlBusProvider); ok {
		_ = provider.RegisterControl("config-editor.apply", "Apply config changes", func(payload interface{}) error {
			d.handleConfigEditorApply(payload)
			return nil
		})
	}

	vw, vh := d.viewportSize()
	w := 80
	h := 30
	if w > vw {
		w = vw - 2
	}
	if h > vh {
		h = vh - 2
	}
	if w < 1 || h < 1 {
		return
	}
	x := (vw - w) / 2
	y := (vh - h) / 2

	d.ShowFloatingPanel(app, x, y, w, h)
}

func (d *DesktopEngine) handleConfigEditorApply(payload interface{}) {
	raw, ok := payload.(string)
	if !ok {
		return
	}
	switch {
	case raw == "system":
		d.reloadLayoutTransitions()
	case raw == "theme":
		d.applyThemeChange()
	case strings.HasPrefix(raw, "app-theme:"):
		d.applyThemeChange()
	}
}

