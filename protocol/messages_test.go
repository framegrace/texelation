// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: protocol/messages_test.go
// Summary: Exercises messages behaviour to ensure the protocol definitions remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: Keep changes backward-compatible; any additions require coordinated version bumps.

package protocol

import (
	"strings"
	"testing"
)

func TestHelloRoundTrip(t *testing.T) {
	var id [16]byte
	copy(id[:], []byte("client-abcdefghi"))
	hello := Hello{ClientID: id, ClientName: "texel-client", Capabilities: 0xdeadbeef}
	payload, err := EncodeHello(hello)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeHello(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.ClientName != hello.ClientName || decoded.Capabilities != hello.Capabilities {
		t.Fatalf("mismatch: %#v vs %#v", decoded, hello)
	}
}

func TestDisconnectNoticeRoundTrip(t *testing.T) {
	notice := DisconnectNotice{ReasonCode: 3, Message: "server shutdown"}
	payload, err := EncodeDisconnectNotice(notice)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeDisconnectNotice(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.ReasonCode != notice.ReasonCode || decoded.Message != notice.Message {
		t.Fatalf("mismatch: %#v vs %#v", decoded, notice)
	}
}

func TestErrorFrameRoundTrip(t *testing.T) {
	frame := ErrorFrame{Code: 500, Message: "bad things"}
	payload, err := EncodeErrorFrame(frame)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeErrorFrame(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Code != frame.Code || decoded.Message != frame.Message {
		t.Fatalf("mismatch: %#v vs %#v", decoded, frame)
	}
}

func TestBufferAckRoundTrip(t *testing.T) {
	ack := BufferAck{Sequence: 1234}
	payload, err := EncodeBufferAck(ack)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeBufferAck(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Sequence != ack.Sequence {
		t.Fatalf("mismatch: got %d want %d", decoded.Sequence, ack.Sequence)
	}
}

func TestKeyEventRoundTrip(t *testing.T) {
	ev := KeyEvent{KeyCode: 42, RuneValue: 'a', Modifiers: 3}
	payload, err := EncodeKeyEvent(ev)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeKeyEvent(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.KeyCode != ev.KeyCode || decoded.RuneValue != ev.RuneValue || decoded.Modifiers != ev.Modifiers {
		t.Fatalf("mismatch: %#v vs %#v", decoded, ev)
	}
}

func TestMouseEventRoundTrip(t *testing.T) {
	ev := MouseEvent{X: 10, Y: 20, ButtonMask: 3, WheelX: -1, WheelY: 2, Modifiers: 5}
	payload, err := EncodeMouseEvent(ev)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeMouseEvent(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded != ev {
		t.Fatalf("mismatch: %#v vs %#v", decoded, ev)
	}
}

func TestClipboardSetRoundTrip(t *testing.T) {
	set := ClipboardSet{MimeType: "text/plain", Data: []byte("hello")}
	payload, err := EncodeClipboardSet(set)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeClipboardSet(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.MimeType != set.MimeType || string(decoded.Data) != string(set.Data) {
		t.Fatalf("mismatch: %#v vs %#v", decoded, set)
	}
}

func TestClipboardGetRoundTrip(t *testing.T) {
	req := ClipboardGet{MimeType: "text/plain"}
	payload, err := EncodeClipboardGet(req)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeClipboardGet(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.MimeType != req.MimeType {
		t.Fatalf("mismatch: %#v vs %#v", decoded, req)
	}
}

func TestClipboardDataRoundTrip(t *testing.T) {
	msg := ClipboardData{MimeType: "text/plain", Data: []byte("world")}
	payload, err := EncodeClipboardData(msg)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeClipboardData(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.MimeType != msg.MimeType || string(decoded.Data) != string(msg.Data) {
		t.Fatalf("mismatch: %#v vs %#v", decoded, msg)
	}
}

func TestPasteRoundTrip(t *testing.T) {
	msg := Paste{Data: []byte("hello world")}
	payload, err := EncodePaste(msg)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodePaste(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if string(decoded.Data) != string(msg.Data) {
		t.Fatalf("mismatch: %#v vs %#v", decoded, msg)
	}
}

func TestThemeUpdateRoundTrip(t *testing.T) {
	update := ThemeUpdate{Section: "pane", Key: "fg", Value: "#ffffff"}
	payload, err := EncodeThemeUpdate(update)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeThemeUpdate(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded != update {
		t.Fatalf("mismatch: %#v vs %#v", decoded, update)
	}
}

func TestThemeAckRoundTrip(t *testing.T) {
	ack := ThemeAck{Section: "pane", Key: "bg", Value: "#000000"}
	payload, err := EncodeThemeAck(ack)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeThemeAck(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded != ack {
		t.Fatalf("mismatch: %#v vs %#v", decoded, ack)
	}
}

func TestPaneFocusRoundTrip(t *testing.T) {
	var id [16]byte
	copy(id[:], []byte("pane-focus-demo"))
	focus := PaneFocus{PaneID: id}
	payload, err := EncodePaneFocus(focus)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodePaneFocus(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded != focus {
		t.Fatalf("mismatch: %#v vs %#v", decoded, focus)
	}
}

func TestTreeSnapshotRoundTrip(t *testing.T) {
	snapshot := TreeSnapshot{
		Panes: []PaneSnapshot{
			{PaneID: [16]byte{1}, Revision: 1, Title: "pane", Rows: []string{"hello", "world"}, X: 5, Y: 6, Width: 80, Height: 24, AppType: "test", AppConfig: `{"msg":"hi"}`},
		},
		Root: TreeNodeSnapshot{PaneIndex: 0, Split: SplitNone},
	}
	payload, err := EncodeTreeSnapshot(snapshot)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeTreeSnapshot(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(decoded.Panes) != 1 || decoded.Panes[0].Title != "pane" || decoded.Panes[0].Rows[1] != "world" || decoded.Panes[0].X != 5 {
		t.Fatalf("unexpected snapshot: %#v", decoded)
	}
	if decoded.Panes[0].AppType != "test" || decoded.Panes[0].AppConfig != `{"msg":"hi"}` {
		t.Fatalf("unexpected app metadata: %#v", decoded.Panes[0])
	}
	if decoded.Root.PaneIndex != 0 || decoded.Root.Split != SplitNone {
		t.Fatalf("unexpected root node: %#v", decoded.Root)
	}
}

func TestStateUpdateRoundTrip(t *testing.T) {
	update := StateUpdate{
		WorkspaceID:   7,
		AllWorkspaces: []int32{1, 3, 5},
		InControlMode: true,
		SubMode:       'w',
		ActiveTitle:   "shell",
		DesktopBgRGB:  0x112233,
		Zoomed:        true,
		ZoomedPaneID:  [16]byte{9, 9, 9, 9},
	}
	payload, err := EncodeStateUpdate(update)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeStateUpdate(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.WorkspaceID != update.WorkspaceID || decoded.SubMode != update.SubMode || decoded.ActiveTitle != update.ActiveTitle {
		t.Fatalf("unexpected decoded state %#v", decoded)
	}
	if decoded.DesktopBgRGB != update.DesktopBgRGB {
		t.Fatalf("desktop color mismatch: %x", decoded.DesktopBgRGB)
	}
	if !decoded.Zoomed || decoded.ZoomedPaneID != update.ZoomedPaneID {
		t.Fatalf("zoom info mismatch: %#v", decoded)
	}
	if len(decoded.AllWorkspaces) != len(update.AllWorkspaces) {
		t.Fatalf("workspace list mismatch: %v", decoded.AllWorkspaces)
	}
}

func TestPaneStateRoundTrip(t *testing.T) {
	state := PaneState{PaneID: [16]byte{1, 2, 3, 4}, Flags: PaneStateActive | PaneStateResizing, ZOrder: 42}
	payload, err := EncodePaneState(state)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodePaneState(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded != state {
		t.Fatalf("pane state mismatch: %#v", decoded)
	}
}

func TestResizeRoundTrip(t *testing.T) {
	resize := Resize{Cols: 120, Rows: 40}
	payload, err := EncodeResize(resize)
	if err != nil {
		t.Fatalf("encode resize failed: %v", err)
	}
	decoded, err := DecodeResize(payload)
	if err != nil {
		t.Fatalf("decode resize failed: %v", err)
	}
	if decoded != resize {
		t.Fatalf("resize mismatch: %#v", decoded)
	}
}

func BenchmarkEncodeBufferDelta(b *testing.B) {
	delta := BufferDelta{
		PaneID:   [16]byte{1, 2, 3, 4},
		Revision: 42,
		Styles: []StyleEntry{
			{AttrFlags: AttrBold, FgModel: ColorModelRGB, FgValue: 0xFFFFFF, BgModel: ColorModelRGB, BgValue: 0x000000},
		},
		Rows: make([]RowDelta, 24),
	}
	for i := range delta.Rows {
		delta.Rows[i] = RowDelta{
			Row:   uint16(i),
			Spans: []CellSpan{{StartCol: 0, Text: strings.Repeat("A", 80), StyleIndex: 0}},
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := EncodeBufferDelta(delta); err != nil {
			b.Fatal(err)
		}
	}
}
