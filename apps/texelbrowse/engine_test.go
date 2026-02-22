// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/engine_test.go
// Summary: Integration tests for the browser engine layer.
//          Requires Chromium and TEXELBROWSE_INTEGRATION=1 to run.

package texelbrowse

import (
	"os"
	"sync"
	"testing"
	"time"
)

func skipUnlessIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("TEXELBROWSE_INTEGRATION") == "" {
		t.Skip("set TEXELBROWSE_INTEGRATION=1 to run browser tests (requires chromium)")
	}
}

// chromeProfileDir creates a temporary directory for Chrome's user data.
// Chrome may hold file locks briefly after shutdown, so we use os.MkdirTemp
// instead of t.TempDir() to avoid test failures from cleanup races.
// Cleanup is best-effort with a short delay.
func chromeProfileDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "texelbrowse-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		// Chrome may still be releasing file locks; give it a moment.
		time.Sleep(100 * time.Millisecond)
		os.RemoveAll(dir)
	})
	return dir
}

func TestEngine_LaunchAndNavigate(t *testing.T) {
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

	if err := tab.Navigate("https://example.com"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	url, title := tab.Location()
	if url == "" {
		t.Error("expected non-empty URL after navigation")
	}
	if title == "" {
		t.Error("expected non-empty title after navigation")
	}
	t.Logf("url=%q title=%q", url, title)
}

func TestEngine_BackForwardReload(t *testing.T) {
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

	// Navigate to first page.
	if err := tab.Navigate("https://example.com"); err != nil {
		t.Fatalf("Navigate(example.com): %v", err)
	}
	url1, _ := tab.Location()

	// Navigate to second page.
	if err := tab.Navigate("https://example.org"); err != nil {
		t.Fatalf("Navigate(example.org): %v", err)
	}
	url2, _ := tab.Location()

	if url1 == url2 {
		t.Fatalf("expected different URLs, got %q for both", url1)
	}

	// Go back.
	if err := tab.Back(); err != nil {
		t.Fatalf("Back: %v", err)
	}
	urlBack, _ := tab.Location()
	if urlBack != url1 {
		t.Errorf("after Back: got %q, want %q", urlBack, url1)
	}

	// Go forward.
	if err := tab.Forward(); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	urlFwd, _ := tab.Location()
	if urlFwd != url2 {
		t.Errorf("after Forward: got %q, want %q", urlFwd, url2)
	}

	// Reload.
	if err := tab.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	urlReload, _ := tab.Location()
	if urlReload != url2 {
		t.Errorf("after Reload: got %q, want %q", urlReload, url2)
	}
}

func TestEngine_MultipleTabs(t *testing.T) {
	skipUnlessIntegration(t)

	profileDir := chromeProfileDir(t)
	engine, err := NewEngine(profileDir)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	tab1, err := engine.NewTab()
	if err != nil {
		t.Fatalf("NewTab 1: %v", err)
	}

	tab2, err := engine.NewTab()
	if err != nil {
		t.Fatalf("NewTab 2: %v", err)
	}

	if err := tab1.Navigate("https://example.com"); err != nil {
		t.Fatalf("tab1.Navigate: %v", err)
	}
	if err := tab2.Navigate("https://example.org"); err != nil {
		t.Fatalf("tab2.Navigate: %v", err)
	}

	url1, _ := tab1.Location()
	url2, _ := tab2.Location()

	if url1 == "" || url2 == "" {
		t.Errorf("expected non-empty URLs: tab1=%q tab2=%q", url1, url2)
	}
	if url1 == url2 {
		t.Errorf("expected different URLs for different tabs, both got %q", url1)
	}

	// Close tab1, tab2 should still work.
	tab1.Close()

	if err := tab2.Reload(); err != nil {
		t.Fatalf("tab2.Reload after tab1.Close: %v", err)
	}
	tab2.Close()
}

func TestEngine_Callbacks(t *testing.T) {
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

	var mu sync.Mutex
	var navigateURLs []string
	var loadingStates []bool

	tab.OnNavigate = func(url, title string) {
		mu.Lock()
		navigateURLs = append(navigateURLs, url)
		mu.Unlock()
	}
	tab.OnLoading = func(loading bool) {
		mu.Lock()
		loadingStates = append(loadingStates, loading)
		mu.Unlock()
	}

	if err := tab.Navigate("https://example.com"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	// Give lifecycle events a moment to arrive.
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	navCount := len(navigateURLs)
	loadCount := len(loadingStates)
	mu.Unlock()

	// We expect at least one navigate callback from captureLocation
	// and possibly more from the lifecycle listener.
	if navCount == 0 {
		t.Error("expected at least one OnNavigate callback")
	}

	// We expect at least one loading state change (true then false).
	if loadCount == 0 {
		t.Error("expected at least one OnLoading callback")
	}

	t.Logf("navigate callbacks: %d, loading callbacks: %d", navCount, loadCount)
}

func TestEngine_FetchAXTree(t *testing.T) {
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

	if err := tab.Navigate("https://example.com"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	doc, err := tab.FetchDocument()
	if err != nil {
		t.Fatalf("FetchDocument: %v", err)
	}

	if len(doc.Nodes) == 0 {
		t.Fatal("expected non-empty document")
	}
	if doc.URL == "" {
		t.Error("expected document to have URL set")
	}
	if doc.Title == "" {
		t.Error("expected document to have title set")
	}

	// example.com should have at least a heading and a link.
	var hasHeading, hasLink bool
	for _, n := range doc.Nodes {
		switch n.Role {
		case "heading":
			hasHeading = true
			t.Logf("heading: %q (level %d)", n.Name, n.Level)
		case "link":
			hasLink = true
			t.Logf("link: %q", n.Name)
		}
	}
	if !hasHeading {
		t.Error("expected at least one heading node in example.com AX tree")
	}
	if !hasLink {
		t.Error("expected at least one link node in example.com AX tree")
	}

	t.Logf("document: %d nodes, url=%q, title=%q", len(doc.Nodes), doc.URL, doc.Title)
}
