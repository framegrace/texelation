package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"texelation/texel"
)

func TestSnapshotRestoresMultipleWorkspaces(t *testing.T) {
	// Setup
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	sockPath := filepath.Join(dir, "test.sock")
	
	// Create Desktop
	driver := sinkScreenDriver{}
	lifecycle := texel.NoopAppLifecycle{}
	shellFactory := func() texel.App { return &recordingApp{title: "shell"} }
	
	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}
	
	// Setup Server with persistence
	srv := NewServer(sockPath, NewManager())
	sink := NewDesktopSink(desktop)
	srv.SetEventSink(sink)
	srv.SetSnapshotStore(NewSnapshotStore(path), 1*time.Hour)
	
	go srv.Start()
	defer srv.Stop(context.Background())

	// 1. Create Workspace 1 with app
	desktop.SwitchToWorkspace(1)
	desktop.ActiveWorkspace().AddApp(&recordingApp{title: "WS1-App"})
	
	// 2. Create Workspace 2 with app
	desktop.SwitchToWorkspace(2)
	desktop.ActiveWorkspace().AddApp(&recordingApp{title: "WS2-App"})
	
	// Wait for save (AddApp triggers it)
	waitForSnapshotPaneCount(t, path, 2)
	t.Log("Snapshot saved with 2 panes")

	// 3. Restart Desktop (simulate restart by creating new engine and applying snapshot)
	// We simulate what Server.loadBootSnapshot does
	
	// Load snapshot manually
	store := NewSnapshotStore(path)
	stored, err := store.Load()
	if err != nil {
		t.Fatalf("Failed to load snapshot: %v", err)
	}
	
	// Convert to capture
	capture := stored.ToTreeCapture()
	
	if len(capture.WorkspaceRoots) != 2 {
		t.Fatalf("Expected 2 workspace roots, got %d", len(capture.WorkspaceRoots))
	}
	
	// Create NEW desktop
	newDesktop, _ := texel.NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	
	// Apply capture
	err = newDesktop.ApplyTreeCapture(capture)
	if err != nil {
		t.Fatalf("ApplyTreeCapture failed: %v", err)
	}
	
	// Verify state
	newDesktop.SwitchToWorkspace(1)
	if newDesktop.ActiveWorkspace().ActivePane() == nil {
		t.Error("Workspace 1 empty after restore")
	} else {
		// Can't access pane.app directly, but we know ActivePane() returns *pane
		// We need a way to check the title. 
		// ActivePane() returns *pane which is unexported in server package? 
		// No, ActivePane returns *texel.pane.
		// texel.Pane has no exported GetTitle.
		// BUT, texel.Pane IS exported (as pane? no, pane is lowercase in texel/pane.go).
		// Wait, Workspace.ActivePane() returns *pane (lowercase).
		// So we can't access it here.
		
		// Workaround: CaptureTree again from the new desktop and check results
		snap := newDesktop.CaptureTree()
		found1 := false
		found2 := false
		for _, p := range snap.Panes {
			if p.Title == "WS1-App" { found1 = true }
			if p.Title == "WS2-App" { found2 = true }
		}
		
		if !found1 {
			t.Error("WS1-App not found in restored desktop")
		}
		if !found2 {
			t.Error("WS2-App not found in restored desktop")
		}
	}
}
