package statusbar

import (
	"testing"
	"time"

	"github.com/framegrace/texelation/texel"
	"github.com/gdamore/tcell/v2"
)

func TestStatusBar_ReceivesWorkspacesChanged(t *testing.T) {
	sb := New()
	sb.Resize(80, 2)

	sb.OnEvent(texel.Event{
		Type: texel.EventWorkspacesChanged,
		Payload: texel.WorkspacesChangedPayload{
			Workspaces: []texel.WorkspaceInfo{
				{ID: 1, Name: "main", Color: tcell.ColorGreen},
				{ID: 2, Name: "dev", Color: tcell.ColorBlue},
			},
			ActiveID: 1,
		},
	})

	buf := sb.Render()
	if len(buf) < 2 {
		t.Fatalf("expected at least 2 rows, got %d", len(buf))
	}
	if len(buf[0]) != 80 {
		t.Fatalf("expected 80 cols in row 0, got %d", len(buf[0]))
	}
	if len(buf[1]) != 80 {
		t.Fatalf("expected 80 cols in row 1, got %d", len(buf[1]))
	}
}

func TestStatusBar_ReceivesModeChanged(t *testing.T) {
	sb := New()
	sb.Resize(80, 2)

	// Should not panic.
	sb.OnEvent(texel.Event{
		Type:    texel.EventModeChanged,
		Payload: texel.ModeChangedPayload{InControlMode: true, SubMode: 'w'},
	})

	buf := sb.Render()
	if len(buf) < 2 {
		t.Fatalf("expected at least 2 rows, got %d", len(buf))
	}
}

func TestStatusBar_ReceivesToast(t *testing.T) {
	sb := New()
	sb.Resize(80, 2)

	sb.OnEvent(texel.Event{
		Type: texel.EventToast,
		Payload: texel.ToastPayload{
			Message:  "Snapshot saved",
			Severity: texel.ToastSuccess,
			Duration: 3 * time.Second,
		},
	})

	buf := sb.Render()
	if len(buf) < 2 {
		t.Fatalf("expected at least 2 rows, got %d", len(buf))
	}
}

// TestStatusBar_FetchPendingAggregates — feeds two panes' +1/-1
// deltas through OnEvent and asserts the per-pane map sums correctly,
// clamps at zero on stray -1, and clears entirely when both panes
// resolve.
func TestStatusBar_FetchPendingAggregates(t *testing.T) {
	sb := New()
	sb.Resize(80, 2)

	paneA := [16]byte{0xa}
	paneB := [16]byte{0xb}

	send := func(paneID [16]byte, delta int) {
		sb.OnEvent(texel.Event{
			Type:    texel.EventFetchPending,
			Payload: texel.FetchPendingPayload{PaneID: paneID, Delta: delta},
		})
	}

	send(paneA, +1)
	send(paneB, +1)
	send(paneB, +1)
	if got := sb.fetchPendingTotal(); got != 3 {
		t.Errorf("after +1/+1/+1 across two panes, total = %d, want 3", got)
	}

	send(paneA, -1)
	if got := sb.fetchPendingTotal(); got != 2 {
		t.Errorf("after paneA resolved, total = %d, want 2", got)
	}

	// Stray -1 below zero on paneB should clamp; total decreases by 1.
	send(paneB, -1)
	send(paneB, -1)
	send(paneB, -1) // would drive paneB negative; clamp at 0.
	if got := sb.fetchPendingTotal(); got != 0 {
		t.Errorf("after over-decrementing, total = %d, want 0 (clamped)", got)
	}
}

// fetchPendingTotal exposes the aggregated count for tests.
func (sb *StatusBarApp) fetchPendingTotal() int {
	sb.fetchPendingMu.Lock()
	defer sb.fetchPendingMu.Unlock()
	total := 0
	for _, n := range sb.fetchPendingBy {
		total += n
	}
	return total
}

func TestStatusBar_Lifecycle(t *testing.T) {
	sb := New()
	sb.Resize(80, 2)

	done := make(chan struct{})
	go func() {
		_ = sb.Run()
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	sb.Stop()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after Stop")
	}
}

func TestStatusBar_Title(t *testing.T) {
	sb := New()
	if title := sb.GetTitle(); title != "Status Bar" {
		t.Errorf("expected title %q, got %q", "Status Bar", title)
	}
}
