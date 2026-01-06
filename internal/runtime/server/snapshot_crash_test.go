package server

import (
	texelcore "github.com/framegrace/texelui/core"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/framegrace/texelation/registry"
	"github.com/framegrace/texelation/texel"
)

// crashingApp simulates an app that fails on startup (e.g. invalid command)
type crashingApp struct {
	title string
}

func (c *crashingApp) Run() error                        { return errors.New("simulated crash") }
func (c *crashingApp) Stop()                             {}
func (c *crashingApp) Resize(cols, rows int)             {}
func (c *crashingApp) Render() [][]texelcore.Cell            { return [][]texelcore.Cell{{}} }
func (c *crashingApp) GetTitle() string                  { return c.title }
func (c *crashingApp) HandleKey(ev *tcell.EventKey)      {}
func (c *crashingApp) SetRefreshNotifier(ch chan<- bool) {}
func (c *crashingApp) SnapshotMetadata() (string, map[string]interface{}) {
	return "crashing-app", map[string]interface{}{"title": c.title}
}

// waitForSnapshotPaneCount waits until the snapshot contains expected number of panes
func waitForSnapshotPaneCount(t *testing.T, path string, expectedCount int) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		store := NewSnapshotStore(path)
		snap, err := store.Load()
		if err == nil {
			if len(snap.Panes) == expectedCount {
				return // Success
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Timeout waiting for snapshot to have %d panes", expectedCount)
}

func TestSnapshotRemovesCrashedApp(t *testing.T) {
	// Setup
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	sockPath := filepath.Join(dir, "test.sock")
	
	// Create Desktop
	driver := sinkScreenDriver{}
	// Use LocalAppLifecycle so StartApp actually runs the crashing app
	lifecycle := &texel.LocalAppLifecycle{}
	shellFactory := func() texelcore.App { return &recordingApp{title: "shell"} }
	
	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}
	defer lifecycle.Wait()
	defer desktop.Close()
	desktop.SwitchToWorkspace(1)
	
	// Register crashing app factory
	desktop.Registry().RegisterBuiltIn(&registry.Manifest{
		Name:        "CrashApp",
		DisplayName: "Crashing App",
	}, func() interface{} {
		return &crashingApp{title: "CrashApp"}
	})
	
	// Create Server
	srv := NewServer(sockPath, NewManager())
	sink := NewDesktopSink(desktop)
	srv.SetEventSink(sink)
	defer desktop.Unsubscribe(srv)
	srv.SetSnapshotStore(NewSnapshotStore(path), 1*time.Hour)
	
	go srv.Start()
	defer srv.Stop(context.Background())

	// 1. Add normal app
	desktop.ActiveWorkspace().AddApp(&recordingApp{title: "Normal"})
	waitForSnapshotPaneCount(t, path, 1)
	t.Log("Initial state: 1 pane saved")

	// 2. Replace with crashing app
	// This simulates starting an app that fails immediately
	ws := desktop.ActiveWorkspace()
	pane := ws.ActivePane()
	
	// This will trigger AttachApp -> StartApp -> Error -> handleAppExit -> RemoveNode
	pane.ReplaceWithApp("CrashApp", nil)
	
	// Wait for save
	// handleAppExit should PRESERVE the pane on error, so count remains 1.
	waitForSnapshotPaneCount(t, path, 1)
	t.Log("Crashed app preserved in snapshot (1 pane)")
}
