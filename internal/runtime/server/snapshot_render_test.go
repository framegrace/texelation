package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"texelation/texel"
)

func TestSnapshotBuffersOnlyReturnsActiveWorkspace(t *testing.T) {
	// Setup
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	sockPath := filepath.Join(dir, "test.sock")
	
	driver := sinkScreenDriver{}
	lifecycle := texel.NoopAppLifecycle{}
	shellFactory := func() texel.App { return &recordingApp{title: "shell"} }
	
	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}
	
	srv := NewServer(sockPath, NewManager())
	sink := NewDesktopSink(desktop)
	srv.SetEventSink(sink)
	srv.SetSnapshotStore(NewSnapshotStore(path), 1*time.Hour)
	
	go srv.Start()
	defer srv.Stop(context.Background())

	// 1. Create Workspace 1 with "App1"
	desktop.SwitchToWorkspace(1)
	desktop.ActiveWorkspace().AddApp(&recordingApp{title: "App1"})
	
	// 2. Create Workspace 2 with "App2"
	desktop.SwitchToWorkspace(2)
	desktop.ActiveWorkspace().AddApp(&recordingApp{title: "App2"})
	
	// 3. Verify SnapshotBuffers only contains "App2" (active workspace)
	buffers := desktop.SnapshotBuffers()
	
	foundApp1 := false
	foundApp2 := false
	for _, p := range buffers {
		if p.Title == "App1" { foundApp1 = true }
		if p.Title == "App2" { foundApp2 = true }
	}
	
	if foundApp1 {
		t.Errorf("SnapshotBuffers included App1 from inactive workspace!")
	}
	if !foundApp2 {
		t.Errorf("SnapshotBuffers missing App2 from active workspace!")
	}
	
	// 4. Verify CaptureTree (persistence) still contains BOTH
	capture := desktop.CaptureTree()
	foundApp1 = false
	foundApp2 = false
	for _, p := range capture.Panes {
		if p.Title == "App1" { foundApp1 = true }
		if p.Title == "App2" { foundApp2 = true }
	}
	
	if !foundApp1 || !foundApp2 {
		t.Errorf("CaptureTree failed to capture all workspaces (found1=%v, found2=%v)", foundApp1, foundApp2)
	}
}
