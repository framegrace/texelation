// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/progress_reporter_test.go
// Summary: Tests for DesktopEngine.SetProgressReporter and the
// per-pane progress emissions in SnapshotForClient. The boot splash
// surfaces these messages during cold-start WAL replay; without
// direct test coverage an off-by-one in the "N of M" denominator or
// a mis-quoted title would ship silently.

package texel

import (
	"sync"
	"testing"
)

// snapshotProgressTestPane builds a minimal leaf pane suitable for a
// snapshot walk. It only needs a non-nil app (so getTitle returns the
// title) and the absolute rect fields; everything else can stay zero.
func snapshotProgressTestPane(title string) *pane {
	p := newPane(nil)
	p.absX0, p.absY0 = 0, 0
	p.absX1, p.absY1 = 20, 6
	app := &snapshotTestApp{title: title, cols: 18, rows: 4}
	p.setApp(app)
	return p
}

// makeWorkspaceWithPanes builds a DesktopEngine whose active
// workspace's tree consists of a flat split with one leaf per title.
// Sufficient for SnapshotForClient's depth-first walk; we don't need
// a real lifecycle / shell factory because capturePaneSnapshot reads
// only the pane's own state.
func makeWorkspaceWithPanes(t *testing.T, titles []string) *DesktopEngine {
	t.Helper()
	d := &DesktopEngine{
		workspaces: make(map[int]*Workspace),
	}
	ws := &Workspace{id: 1, tree: &Tree{}}
	if len(titles) == 1 {
		// Single-leaf tree.
		ws.tree.Root = &Node{Pane: snapshotProgressTestPane(titles[0])}
	} else {
		// Horizontal split with one leaf per title — keeps the walk
		// order stable so we can assert message ordering.
		root := &Node{Split: Horizontal}
		for _, title := range titles {
			leaf := &Node{Parent: root, Pane: snapshotProgressTestPane(title)}
			root.Children = append(root.Children, leaf)
		}
		ws.tree.Root = root
	}
	d.workspaces[ws.id] = ws
	d.activeWorkspace = ws
	return d
}

func TestSetProgressReporter_StoresAndDetaches(t *testing.T) {
	d := &DesktopEngine{}
	if d.progressReporter != nil {
		t.Fatal("default progressReporter should be nil")
	}
	d.SetProgressReporter(func(string) {})
	if d.progressReporter == nil {
		t.Fatal("SetProgressReporter did not store the callback")
	}
	d.SetProgressReporter(nil)
	if d.progressReporter != nil {
		t.Fatal("SetProgressReporter(nil) did not detach the callback")
	}
}

func TestSnapshotForClient_PerPaneProgressMessages(t *testing.T) {
	titles := []string{"shell", "editor", "logs"}
	d := makeWorkspaceWithPanes(t, titles)

	var mu sync.Mutex
	var got []string
	d.SetProgressReporter(func(msg string) {
		mu.Lock()
		got = append(got, msg)
		mu.Unlock()
	})

	_ = d.SnapshotForClient()

	mu.Lock()
	defer mu.Unlock()
	want := []string{
		"Rendering pane 1 of 3 (shell)…",
		"Rendering pane 2 of 3 (editor)…",
		"Rendering pane 3 of 3 (logs)…",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d: %#v", len(got), len(want), got)
	}
	for i, msg := range want {
		if got[i] != msg {
			t.Errorf("message %d: got %q, want %q", i, got[i], msg)
		}
	}
}

func TestSnapshotForClient_NoReporterIsSilent(t *testing.T) {
	// SnapshotForClient with progressReporter unset must not panic
	// and must not invoke any callback (since none is registered).
	d := makeWorkspaceWithPanes(t, []string{"only"})
	// Don't call SetProgressReporter.

	// Just verify it doesn't panic; capture.Panes population is
	// already covered by other snapshot tests.
	_ = d.SnapshotForClient()
}

func TestSnapshotForClient_UntitledPaneFallsBackGracefully(t *testing.T) {
	// A pane with an empty title must not produce "Rendering pane 1
	// of 1 ()…"; the format substitutes "untitled".
	d := &DesktopEngine{
		workspaces: make(map[int]*Workspace),
	}
	ws := &Workspace{id: 1, tree: &Tree{}}
	p := newPane(nil)
	p.absX0, p.absY0 = 0, 0
	p.absX1, p.absY1 = 20, 6
	app := &snapshotTestApp{title: "", cols: 18, rows: 4}
	p.setApp(app)
	ws.tree.Root = &Node{Pane: p}
	d.workspaces[ws.id] = ws
	d.activeWorkspace = ws

	var got string
	d.SetProgressReporter(func(msg string) { got = msg })

	_ = d.SnapshotForClient()

	want := "Rendering pane 1 of 1 (untitled)…"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
