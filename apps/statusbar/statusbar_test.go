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
