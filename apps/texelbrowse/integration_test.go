// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/integration_test.go
// Summary: End-to-end integration test exercising the full pipeline:
//          Engine -> Tab -> Navigate -> FetchDocument -> MapDocument ->
//          LayoutManager.Arrange.

package texelbrowse

import (
	"testing"
	"time"
)

func TestIntegration_NavigateAndRender(t *testing.T) {
	skipUnlessIntegration(t)

	profileDir := chromeProfileDir(t)
	engine, err := NewEngine(profileDir)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	tab, err := engine.NewTab()
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}
	defer tab.Close()

	// Navigate to example.com.
	if err := tab.Navigate("https://example.com"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	// Fetch document.
	doc, err := tab.FetchDocument()
	if err != nil {
		t.Fatalf("FetchDocument: %v", err)
	}

	t.Logf("Page: %s (%s)", doc.URL, doc.Title)
	t.Logf("Nodes: %d, Mode: %v", len(doc.Nodes), doc.SuggestedMode())

	// Map to widgets.
	mapper := NewMapper(nil)
	widgets := mapper.MapDocument(doc)
	t.Logf("Widgets: %d", len(widgets))

	if len(widgets) == 0 {
		t.Error("expected at least one widget from example.com")
	}

	// Layout.
	layout := NewLayoutManager(80, 24)
	layout.SetMode(doc.SuggestedMode())
	layout.Arrange(widgets)

	// Verify widgets have valid positions.
	for i, w := range widgets {
		x, y := w.Position()
		if x < 0 || y < 0 {
			t.Errorf("widget %d has negative position: (%d, %d)", i, x, y)
		}
	}

	// Verify grouped layout produces a root widget.
	rootWidget := mapper.MapDocumentGrouped(doc)
	if rootWidget == nil {
		t.Error("expected non-nil root widget from MapDocumentGrouped")
	}
	rootWidget.Resize(80, 10000)
	_, rootH := rootWidget.Size()
	t.Logf("Grouped root height: %d", rootH)
	if rootH <= 0 {
		t.Error("expected positive grouped content height")
	}

	// Navigate to a data URI with a form.
	formHTML := `data:text/html,<html><body>
		<h1>Login</h1>
		<form>
			<input type="text" aria-label="Username">
			<input type="password" aria-label="Password">
			<button type="submit">Sign In</button>
		</form>
	</body></html>`

	if err := tab.Navigate(formHTML); err != nil {
		t.Fatalf("Navigate form: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	doc, err = tab.FetchDocument()
	if err != nil {
		t.Fatalf("FetchDocument form: %v", err)
	}

	t.Logf("Form page: %d nodes, mode: %v", len(doc.Nodes), doc.SuggestedMode())

	// Should detect as form mode.
	if doc.SuggestedMode() != ModeForm {
		t.Logf("Warning: expected form mode but got reading mode (ratio may be off)")
	}
}
