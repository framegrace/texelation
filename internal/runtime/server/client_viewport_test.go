package server

import (
	"testing"

	"github.com/framegrace/texelation/protocol"
)

func TestClientViewports_ApplyUpdate(t *testing.T) {
	vs := NewClientViewports()
	pane := [16]byte{1, 2, 3}
	vs.Apply(protocol.ViewportUpdate{
		PaneID:        pane,
		ViewTopIdx:    100,
		ViewBottomIdx: 123,
		Rows:          24,
		Cols:          80,
		AutoFollow:    false,
	})
	got, ok := vs.Get(pane)
	if !ok {
		t.Fatal("viewport missing after Apply")
	}
	if got.ViewTopIdx != 100 || got.ViewBottomIdx != 123 || got.Rows != 24 {
		t.Fatalf("unexpected state: %#v", got)
	}
}

func TestSession_ApplyViewportUpdate(t *testing.T) {
	s := NewSession([16]byte{7}, 0)
	pane := [16]byte{7}
	s.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID:        pane,
		ViewTopIdx:    500,
		ViewBottomIdx: 523,
		Rows:          24,
		Cols:          80,
	})
	got, ok := s.viewports.Get(pane)
	if !ok || got.ViewTopIdx != 500 {
		t.Fatalf("unexpected: %#v ok=%v", got, ok)
	}
}
