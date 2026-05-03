// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package boot

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/protocol"
	"github.com/framegrace/texelui/graphics"
)

// fakeClient stubs the picker's network transport for tests. Fields
// cover both Task 16 (basic ops) and Task 17 (thumbnail fetch).
type fakeClient struct {
	response      protocol.ListSessionsResponse
	listErr       error
	recoverCalled bool
	recoverID     [16]byte
	recoverErr    error
	newCalled     bool
	renameErr     error
	deleteErr     error
	fetchCalled   bool
	thumbBytes    []byte
	thumbErr      error
}

func (f *fakeClient) ListSessions() (protocol.ListSessionsResponse, error) {
	if f.listErr != nil {
		return protocol.ListSessionsResponse{}, f.listErr
	}
	return f.response, nil
}
func (f *fakeClient) RecoverSession(id [16]byte, newLabel string) error {
	f.recoverCalled = true
	f.recoverID = id
	return f.recoverErr
}
func (f *fakeClient) RenameSession(id [16]byte, newLabel string) error { return f.renameErr }
func (f *fakeClient) DeleteSession(id [16]byte) error                  { return f.deleteErr }
func (f *fakeClient) FetchThumbnail(id [16]byte) ([]byte, error) {
	f.fetchCalled = true
	if f.thumbErr != nil {
		return nil, f.thumbErr
	}
	return f.thumbBytes, nil
}
func (f *fakeClient) StartFreshSession() {
	f.newCalled = true
}

func newPickerScreen(t *testing.T) tcell.Screen {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	screen.SetSize(80, 24)
	return screen
}

func screenContents(s tcell.Screen) string {
	sim, ok := s.(tcell.SimulationScreen)
	if !ok {
		return ""
	}
	cells, w, h := sim.GetContents()
	var b []byte
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			if len(c.Runes) > 0 {
				b = append(b, []byte(string(c.Runes[0]))...)
			} else {
				b = append(b, ' ')
			}
		}
		b = append(b, '\n')
	}
	return string(b)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestPicker_RendersStoredCards(t *testing.T) {
	screen := newPickerScreen(t)
	defer screen.Fini()
	p := NewPicker(screen, &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{
				{SessionID: [16]byte{0x01}, Label: "alpha", LastActive: 100, PaneCount: 1},
				{SessionID: [16]byte{0x02}, Label: "beta", LastActive: 50, PaneCount: 2},
			},
		},
	})
	p.RefreshCatalog()
	p.Render()
	body := screenContents(screen)
	if !contains(body, "alpha") {
		t.Errorf("expected 'alpha' in render:\n%s", body)
	}
	if !contains(body, "beta") {
		t.Errorf("expected 'beta' in render:\n%s", body)
	}
}

func TestPicker_NavigationDown(t *testing.T) {
	screen := newPickerScreen(t)
	defer screen.Fini()
	p := NewPicker(screen, &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{
				{SessionID: [16]byte{0x01}, Label: "first"},
				{SessionID: [16]byte{0x02}, Label: "second"},
			},
		},
	})
	p.RefreshCatalog()
	if got := p.SelectedIdx(); got != 0 {
		t.Fatalf("initial selectedIdx = %d, want 0", got)
	}
	p.HandleKey(tcell.KeyDown, 0, 0)
	if got := p.SelectedIdx(); got != 1 {
		t.Fatalf("after down: selectedIdx = %d, want 1", got)
	}
	p.HandleKey(tcell.KeyDown, 0, 0)
	if got := p.SelectedIdx(); got != 1 {
		t.Errorf("clamp: selectedIdx = %d, want 1", got)
	}
}

func TestPicker_EnterDispatchesRecover(t *testing.T) {
	screen := newPickerScreen(t)
	defer screen.Fini()
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{
				{SessionID: [16]byte{0xCC}, Label: "pickme"},
			},
		},
	}
	p := NewPicker(screen, fc)
	p.RefreshCatalog()
	p.HandleKey(tcell.KeyEnter, 0, 0)
	if !fc.recoverCalled {
		t.Errorf("expected RecoverSession dispatch on Enter")
	}
	if fc.recoverID != ([16]byte{0xCC}) {
		t.Errorf("recoverID = %x, want CC", fc.recoverID)
	}
}

func TestPicker_NewKeyDispatchesNew(t *testing.T) {
	screen := newPickerScreen(t)
	defer screen.Fini()
	fc := &fakeClient{response: protocol.ListSessionsResponse{}}
	p := NewPicker(screen, fc)
	p.RefreshCatalog()
	p.HandleKey(0, 'n', 0)
	if !fc.newCalled {
		t.Errorf("expected fresh-session dispatch on 'n'")
	}
}

func TestPicker_RecoverError_StaysOpen(t *testing.T) {
	screen := newPickerScreen(t)
	defer screen.Fini()
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{{SessionID: [16]byte{0xCC}, Label: "broken"}},
		},
		recoverErr: errors.New("session evicted"),
	}
	p := NewPicker(screen, fc)
	p.RefreshCatalog()
	p.HandleKey(tcell.KeyEnter, 0, 0)
	if p.Done() {
		t.Errorf("picker exited despite Recover error")
	}
	if p.errMsg == "" {
		t.Errorf("expected errMsg set after Recover failure")
	}
}

func TestPicker_RefreshCatalogError_PreservesPriorList(t *testing.T) {
	screen := newPickerScreen(t)
	defer screen.Fini()
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{{SessionID: [16]byte{0x01}, Label: "alpha"}},
		},
	}
	p := NewPicker(screen, fc)
	p.RefreshCatalog()
	if len(p.response.Stored) != 1 {
		t.Fatalf("setup: expected 1 stored, got %d", len(p.response.Stored))
	}
	fc.listErr = errors.New("socket dropped")
	p.RefreshCatalog()
	if len(p.response.Stored) != 1 {
		t.Errorf("expected prior list preserved on error, got %d", len(p.response.Stored))
	}
	if p.errMsg == "" {
		t.Errorf("expected errMsg set on RefreshCatalog error")
	}
}

// makeValidPNG produces minimal valid PNG bytes the picker's
// DecodeConfig + full Decode + widgets.Image will accept.
func makeValidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x40, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return buf.Bytes()
}

func TestPicker_FetchThumbnailDispatchedWhenGraphicsCapable(t *testing.T) {
	screen := newPickerScreen(t)
	defer screen.Fini()
	id := [16]byte{0xEE}
	pngBytes := makeValidPNG(t, 100, 60)
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{
				{SessionID: id, Label: "graphics", HasThumbnail: true},
			},
		},
		thumbBytes: pngBytes,
	}
	p := NewPicker(screen, fc)
	p.SetGraphicsProvider(graphics.NewHalfBlockProvider(), io.Discard)
	p.RefreshCatalog()
	p.Render()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if p.ThumbCached(id) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !p.ThumbCached(id) {
		t.Fatalf("expected thumbnail cached within 500ms")
	}
	if !fc.fetchCalled {
		t.Errorf("expected FetchThumbnail dispatch")
	}
}

func TestPicker_NoFetchWhenGraphicsAbsent(t *testing.T) {
	screen := newPickerScreen(t)
	defer screen.Fini()
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{{SessionID: [16]byte{0x10}, HasThumbnail: true}},
		},
		thumbBytes: []byte("nope"),
	}
	p := NewPicker(screen, fc)
	// gp left nil — picker stays in ASCII-only mode.
	p.RefreshCatalog()
	p.Render()
	time.Sleep(20 * time.Millisecond)
	if fc.fetchCalled {
		t.Errorf("did not expect FetchThumbnail dispatch on text-only terminal")
	}
}

func TestPicker_FetchThumbnailErrorClearsPending(t *testing.T) {
	// Regression test: a failed fetch must not leave pending[id]=true
	// forever (would prevent retry). After the goroutine returns,
	// pending must be cleared and failedFetches incremented so
	// successive renders don't re-spawn the goroutine.
	screen := newPickerScreen(t)
	defer screen.Fini()
	id := [16]byte{0xAB}
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{{SessionID: id, HasThumbnail: true}},
		},
		thumbErr: errors.New("transient"),
	}
	p := NewPicker(screen, fc)
	p.SetGraphicsProvider(graphics.NewHalfBlockProvider(), io.Discard)
	p.RefreshCatalog()
	p.Render()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !p.IsPending(id) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if p.IsPending(id) {
		t.Fatalf("expected pending cleared after fetch error within 500ms")
	}
	if p.ThumbCached(id) {
		t.Errorf("expected thumbCache empty on fetch error")
	}
}

func TestPicker_NavigationDismissesError(t *testing.T) {
	screen := newPickerScreen(t)
	defer screen.Fini()
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{
				{SessionID: [16]byte{0x01}, Label: "first"},
				{SessionID: [16]byte{0x02}, Label: "second"},
			},
		},
	}
	p := NewPicker(screen, fc)
	p.RefreshCatalog()
	p.errMsg = "stale error"
	p.HandleKey(tcell.KeyDown, 0, 0)
	if p.errMsg != "" {
		t.Errorf("expected errMsg cleared after navigation, got %q", p.errMsg)
	}
}
