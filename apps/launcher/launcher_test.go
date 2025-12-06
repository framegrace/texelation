// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package launcher

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"texelation/registry"
	"texelation/texel"
)

// mockControlBus implements texel.ControlBus for testing.
type mockControlBus struct {
	handlers     map[string]texel.ControlHandler
	triggerCount map[string]int
	lastPayload  map[string]interface{}
}

func newMockControlBus() *mockControlBus {
	return &mockControlBus{
		handlers:     make(map[string]texel.ControlHandler),
		triggerCount: make(map[string]int),
		lastPayload:  make(map[string]interface{}),
	}
}

func (m *mockControlBus) Trigger(id string, payload interface{}) error {
	m.triggerCount[id]++
	m.lastPayload[id] = payload
	if handler, ok := m.handlers[id]; ok {
		return handler(payload)
	}
	return nil
}

func (m *mockControlBus) Capabilities() []texel.ControlCapability {
	return nil
}

func (m *mockControlBus) Register(id, description string, handler texel.ControlHandler) error {
	m.handlers[id] = handler
	return nil
}

func (m *mockControlBus) Unregister(id string) {
	delete(m.handlers, id)
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

func TestLauncher_AttachControlBus(t *testing.T) {
	reg := createTestRegistry()
	l := &Launcher{
		registry: reg,
	}
	l.loadApps()

	bus := newMockControlBus()
	l.AttachControlBus(bus)

	if l.controlBus == nil {
		t.Error("AttachControlBus() did not set the control bus")
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

	bus := newMockControlBus()
	l.AttachControlBus(bus)

	// Should have 3 apps
	if len(l.apps) != 3 {
		t.Fatalf("Expected 3 apps, got %d", len(l.apps))
	}

	// Launch first app (app1)
	l.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if bus.triggerCount["launcher.select-app"] != 1 {
		t.Errorf("Expected 1 select-app trigger, got %d", bus.triggerCount["launcher.select-app"])
	}

	if bus.lastPayload["launcher.select-app"] != "app1" {
		t.Errorf("Expected to launch 'app1', got '%v'", bus.lastPayload["launcher.select-app"])
	}

	// Move down and launch second app
	l.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	l.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if bus.triggerCount["launcher.select-app"] != 2 {
		t.Errorf("Expected 2 select-app triggers, got %d", bus.triggerCount["launcher.select-app"])
	}

	if bus.lastPayload["launcher.select-app"] != "app2" {
		t.Errorf("Expected to launch 'app2', got '%v'", bus.lastPayload["launcher.select-app"])
	}
}

func TestLauncher_LaunchWithoutControlBus(t *testing.T) {
	reg := createTestRegistry()
	l := &Launcher{
		registry:    reg,
		selectedIdx: 0,
	}
	l.loadApps()

	// Don't attach control bus - should not panic
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
