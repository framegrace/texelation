package server

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"texelation/texel"
)

// mockSnapshotStore wraps SnapshotStore to count saves
type mockSnapshotStore struct {
	*SnapshotStore
	saveCount int
	mu        sync.Mutex
	savedCh   chan struct{}
}

func newMockSnapshotStore(path string) *mockSnapshotStore {
	return &mockSnapshotStore{
		SnapshotStore: NewSnapshotStore(path),
		savedCh:       make(chan struct{}, 1),
	}
}

func (m *mockSnapshotStore) Save(capture *texel.TreeCapture) error {
	m.mu.Lock()
	m.saveCount++
	m.mu.Unlock()

	// Notify valid save
	select {
	case m.savedCh <- struct{}{}:
	default:
	}

	return m.SnapshotStore.Save(capture)
}

func (m *mockSnapshotStore) SaveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveCount
}

func TestSnapshotSavedOnLayoutChange(t *testing.T) {
	// Setup
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")

	// Use mock store to track calls
	mockStore := newMockSnapshotStore(path)

	// Create Desktop and add an initial app so snapshot isn't empty
	driver := sinkScreenDriver{}
	lifecycle := texel.NoopAppLifecycle{}
	shellFactory := func() texel.App { return &recordingApp{title: "shell"} }
	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}
	desktop.SwitchToWorkspace(1)
	desktop.ActiveWorkspace().AddApp(&recordingApp{title: "initial"})

	// Create Server
	// Use a random socket path to avoid conflicts
	sockPath := filepath.Join(dir, "test.sock")
	srv := NewServer(sockPath, NewManager())
	sink := NewDesktopSink(desktop)
	srv.SetEventSink(sink)

	// Set snapshot store with LONG interval
	srv.SetSnapshotStore(mockStore.SnapshotStore, 1*time.Hour)

	// Start Server
	go func() {
		if err := srv.Start(); err != nil && err != os.ErrClosed {
			// t.Logf("Server start error: %v", err)
			// Logging here might be racey with test end
		}
	}()
	defer srv.Stop(context.Background())

	// Wait for initial save (Server.Start calls startSnapshotLoop which calls persistSnapshot)
	// We need to wait enough for the goroutine to spin up and save.
	time.Sleep(500 * time.Millisecond)
	initialInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("initial snapshot not created: %v", err)
	}
	initialTime := initialInfo.ModTime()

	// Trigger Layout Change (simulate adding a pane/split)
	// We add another app to the current workspace, which triggers EventTreeChanged AND ensures we have panes to save.
	t.Log("Adding second app to trigger EventTreeChanged...")
	desktop.ActiveWorkspace().AddApp(&recordingApp{title: "second"})

	// Wait a bit for the event to be processed
	time.Sleep(200 * time.Millisecond)

	// Check if file updated
	newInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}

	if !newInfo.ModTime().After(initialTime) {
		t.Errorf("Snapshot NOT saved after EventTreeChanged (ModTime unchanged)")
	} else {
		t.Log("Snapshot saved successfully after EventTreeChanged")
	}

	// Update reference time
	initialTime = newInfo.ModTime()

	// Now stop the server
	t.Log("Stopping server...")
	// We need to call Stop() to trigger the shutdown save
	// But srv.Stop() closes channels that startSnapshotLoop uses.
	// We need to verify that srv.Stop() triggers one last persistSnapshot.

	// To verify this without race conditions on file system (might be fast),
	// we'd ideally mock the store. But since we can't easily, let's just see if ModTime changes.
	// We'll modify the desktop state first to ensure there's something different to save?
	// Actually, persistSnapshot saves regardless of diff, as long as there are panes.

	// Let's forcefully change the desktop state slightly (title change?)
	// Not easy to reach into panes.
	// But Save() overwrites the file. ModTime should update.

	// Force a sleep to ensure fs timestamp resolution is met
	time.Sleep(1 * time.Second)

	go func() {
		srv.Stop(context.Background())
	}()

	// Wait for server to stop (SnapshotLoop exits)
	srv.wg.Wait()

	finalInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}

	if !finalInfo.ModTime().After(initialTime) {
		t.Errorf("Snapshot NOT saved on Server.Stop() (ModTime unchanged)")
	} else {
		t.Log("Snapshot saved successfully on Stop()")
	}
}

// Add Broadcast method to DesktopEngine for testing if not public
// It is NOT public on DesktopEngine (it's on dispatcher), but DesktopEngine doesn't expose Dispatcher directly?
// DesktopEngine.Subscribe is public.
// DesktopEngine does not expose Broadcast.
// But we can trigger it by causing a layout change.
// desktop.SwitchToWorkspace(2) should trigger it.
