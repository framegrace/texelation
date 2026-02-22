// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/browse.go
// Summary: Semantic terminal browser that drives Chromium via CDP
//          and renders web content through the Accessibility Tree.

package texelbrowse

import (
	"sync"

	"github.com/framegrace/texelui/adapter"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
)

// BrowseApp is a semantic terminal browser that drives Chromium via CDP
// and renders web content through the Accessibility Tree.
type BrowseApp struct {
	*adapter.UIApp
	mu        sync.Mutex
	startURL  string
	statusBar *widgets.StatusBar
}

// New creates a new BrowseApp. If startURL is empty, no page is loaded initially.
func New(startURL string) core.App {
	ui := core.NewUIManager()
	app := &BrowseApp{
		startURL: startURL,
	}
	app.UIApp = adapter.NewUIApp("TexelBrowse", ui)
	app.statusBar = app.UIApp.StatusBar()
	if app.statusBar != nil {
		app.statusBar.SetHintText("No page loaded")
	}
	return app
}
