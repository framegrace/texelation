package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
)

// recordingAppReplace is a mock app that supports snapshotting
type recordingAppReplace struct {
	title   string
	appType string
}

func (r *recordingAppReplace) Run() error                        { return nil }
func (r *recordingAppReplace) Stop()                             {}
func (r *recordingAppReplace) Resize(cols, rows int)             {}
func (r *recordingAppReplace) Render() [][]texel.Cell            { return [][]texel.Cell{{}} }
func (r *recordingAppReplace) GetTitle() string                  { return r.title }
func (r *recordingAppReplace) HandleKey(ev *tcell.EventKey)      {}
func (r *recordingAppReplace) SetRefreshNotifier(ch chan<- bool) {}
func (r *recordingAppReplace) SnapshotMetadata() (string, map[string]interface{}) {
	return r.appType, map[string]interface{}{"title": r.title}
}

func waitForSnapshotContent(t *testing.T, path, expectedTitle, expectedAppType string) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			var stored StoredSnapshot
			if err := json.Unmarshal(data, &stored); err == nil {
				if len(stored.Panes) > 0 {
					// Check if any pane has the expected title
					for _, p := range stored.Panes {
						if strings.Contains(p.Title, expectedTitle) {
							if expectedAppType != "" && p.AppType != expectedAppType {
								continue
							}
							return // Found it!
						}
					}
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Timeout waiting for snapshot to contain title '%s' with app_type '%s'", expectedTitle, expectedAppType)
}

func TestSnapshotSavedOnReplaceWithApp(t *testing.T) {
	// Setup
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	sockPath := filepath.Join(dir, "test.sock")

	// Create Desktop
	driver := sinkScreenDriver{}
	lifecycle := texel.NoopAppLifecycle{}

	// Factory creates "Launcher" initially
	shellFactory := func() texel.App { return &recordingAppReplace{title: "Launcher", appType: "launcher"} }

	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	// Register a "RealApp" in the registry that we will switch to
	desktop.Registry().RegisterBuiltIn("RealApp", func() interface{} {
		return &recordingAppReplace{title: "RealApp", appType: "realapp"}
	})

	// Create Server
	srv := NewServer(sockPath, NewManager())
	sink := NewDesktopSink(desktop)
	srv.SetEventSink(sink)

	store := NewSnapshotStore(path)
	srv.SetSnapshotStore(store, 1*time.Hour) // Disable timer-based save

	// Start Server
	go srv.Start()
	defer srv.Stop(context.Background())

	// 1. Initialize Workspace with Launcher
	// This should trigger EventAppAttached and save "Launcher"
	desktop.SwitchToWorkspace(1)
	desktop.ActiveWorkspace().AddApp(&recordingAppReplace{title: "Launcher", appType: "launcher"})

	// Verify "Launcher" is saved
	waitForSnapshotContent(t, path, "Launcher", "launcher")
	t.Log("Initial snapshot contains Launcher")

	// 2. Simulate ReplaceWithApp (User selects app in Launcher)
	t.Log("Replacing Launcher with RealApp...")
	pane := desktop.ActiveWorkspace().ActivePane()
	if pane == nil {
		t.Fatal("No active pane")
	}

	// This calls AttachApp, which should trigger EventAppAttached -> persistSnapshot
	pane.ReplaceWithApp("RealApp", nil)

	// Verify "RealApp" is saved
	waitForSnapshotContent(t, path, "RealApp", "realapp")
	t.Log("Snapshot correctly updated to contain RealApp")
}
