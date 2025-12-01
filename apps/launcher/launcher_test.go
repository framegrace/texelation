// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package launcher

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"texelation/registry"
	"texelation/texel"
)

// mockReplacer implements AppReplacer for testing.
type mockReplacer struct {
	lastAppName   string
	lastConfig    map[string]interface{}
	replaceCount  int
}

func (m *mockReplacer) ReplaceWithApp(name string, config map[string]interface{}) {
	m.lastAppName = name
	m.lastConfig = config
	m.replaceCount++
}

// mockApp is a minimal app for testing.
type mockApp struct {
	title string
}

func (m *mockApp) Run() error                                  { return nil }
func (m *mockApp) Stop()                                       {}
func (m *mockApp) Resize(cols, rows int)                       {}
func (m *mockApp) Render() [][]texel.Cell                      { return nil }
func (m *mockApp) GetTitle() string                            { return m.title }
func (m *mockApp) HandleKey(ev *tcell.EventKey)                {}
func (m *mockApp) SetRefreshNotifier(refreshChan chan<- bool) {}

func createTestRegistry() *registry.Registry {
	reg := registry.New()

	// Register some built-in apps
	reg.RegisterBuiltIn("app1", func() interface{} {
		return &mockApp{title: "App 1"}
	})
	reg.RegisterBuiltIn("app2", func() interface{} {
		return &mockApp{title: "App 2"}
	})
	reg.RegisterBuiltIn("app3", func() interface{} {
		return &mockApp{title: "App 3"}
	})

	return reg
}

func TestLauncher_Creation(t *testing.T) {
	reg := createTestRegistry()
	app := New(reg)

	if app == nil {
		t.Fatal("New() returned nil")
	}

	if app.GetTitle() != "Launcher" {
		t.Errorf("Expected title 'Launcher', got '%s'", app.GetTitle())
	}
}

func TestLauncher_LoadsApps(t *testing.T) {
	reg := createTestRegistry()

	// Create launcher (wraps in pipeline, so we need to extract the underlying launcher)
	launcherApp := New(reg)

	// The returned app is wrapped in a pipeline, so we can't directly access it
	// But we can verify it doesn't panic and has the right title
	if launcherApp.GetTitle() != "Launcher" {
		t.Errorf("Expected title 'Launcher', got '%s'", launcherApp.GetTitle())
	}

	// Resize to trigger UI layout
	launcherApp.Resize(80, 24)

	// Render to ensure no panics
	buffer := launcherApp.Render()
	if buffer == nil {
		t.Error("Render() returned nil")
	}
}

func TestLauncher_SetReplacer(t *testing.T) {
	reg := createTestRegistry()
	l := &Launcher{
		registry: reg,
	}
	l.loadApps()

	replacer := &mockReplacer{}
	l.SetReplacer(replacer)

	if l.replacer == nil {
		t.Error("SetReplacer() did not set the replacer")
	}
}

func TestLauncher_NavigationUpDown(t *testing.T) {
	reg := createTestRegistry()
	l := &Launcher{
		registry:    reg,
		selectedIdx: 0,
	}
	l.loadApps()

	// Should have 3 apps
	if len(l.apps) != 3 {
		t.Fatalf("Expected 3 apps, got %d", len(l.apps))
	}

	// Initially at index 0
	if l.selectedIdx != 0 {
		t.Errorf("Expected selectedIdx 0, got %d", l.selectedIdx)
	}

	// Press Down - should move to index 1
	l.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if l.selectedIdx != 1 {
		t.Errorf("After Down, expected selectedIdx 1, got %d", l.selectedIdx)
	}

	// Press Down again - should move to index 2
	l.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if l.selectedIdx != 2 {
		t.Errorf("After Down, expected selectedIdx 2, got %d", l.selectedIdx)
	}

	// Press Down again - should stay at index 2 (boundary)
	l.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if l.selectedIdx != 2 {
		t.Errorf("After Down at boundary, expected selectedIdx 2, got %d", l.selectedIdx)
	}

	// Press Up - should move to index 1
	l.HandleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if l.selectedIdx != 1 {
		t.Errorf("After Up, expected selectedIdx 1, got %d", l.selectedIdx)
	}

	// Press Up twice - should move to index 0
	l.HandleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	l.HandleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if l.selectedIdx != 0 {
		t.Errorf("After Up at boundary, expected selectedIdx 0, got %d", l.selectedIdx)
	}
}

func TestLauncher_LaunchApp(t *testing.T) {
	reg := createTestRegistry()
	l := &Launcher{
		registry:    reg,
		selectedIdx: 0,
	}
	l.loadApps()

	replacer := &mockReplacer{}
	l.SetReplacer(replacer)

	// Should have 3 apps
	if len(l.apps) != 3 {
		t.Fatalf("Expected 3 apps, got %d", len(l.apps))
	}

	// Launch first app (app1)
	l.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if replacer.replaceCount != 1 {
		t.Errorf("Expected 1 replace call, got %d", replacer.replaceCount)
	}

	if replacer.lastAppName != "app1" {
		t.Errorf("Expected to launch 'app1', got '%s'", replacer.lastAppName)
	}

	// Move down and launch second app
	l.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	l.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if replacer.replaceCount != 2 {
		t.Errorf("Expected 2 replace calls, got %d", replacer.replaceCount)
	}

	if replacer.lastAppName != "app2" {
		t.Errorf("Expected to launch 'app2', got '%s'", replacer.lastAppName)
	}
}

func TestLauncher_LaunchWithoutReplacer(t *testing.T) {
	reg := createTestRegistry()
	l := &Launcher{
		registry:    reg,
		selectedIdx: 0,
	}
	l.loadApps()

	// Don't set replacer - should not panic
	l.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	// Should complete without error
}

func TestLauncher_EmptyRegistry(t *testing.T) {
	reg := registry.New()
	l := &Launcher{
		registry:    reg,
		selectedIdx: 0,
	}
	l.loadApps()

	if len(l.apps) != 0 {
		t.Errorf("Expected 0 apps with empty registry, got %d", len(l.apps))
	}

	// Navigation should not panic
	l.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	l.HandleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	l.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
}

func TestLauncher_Resize(t *testing.T) {
	reg := createTestRegistry()
	app := New(reg)

	// Should not panic on resize
	app.Resize(80, 24)
	app.Resize(120, 40)
	app.Resize(20, 10)

	// Should render after resize
	buffer := app.Render()
	if buffer == nil {
		t.Error("Render() returned nil after resize")
	}
}
