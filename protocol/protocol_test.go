// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: protocol/protocol_test.go
// Summary: Exercises protocol behaviour to ensure the protocol definitions remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: Keep changes backward-compatible; any additions require coordinated version bumps.

package protocol

import (
	"bytes"
	"errors"
	"testing"
)

func TestWriteReadRoundTrip(t *testing.T) {
	var session [16]byte
	copy(session[:], []byte("session-123456"))

	header := Header{
		Version:   Version,
		Type:      MsgBufferDelta,
		Flags:     FlagChecksum,
		Sequence:  42,
		SessionID: session,
	}
	payload := []byte("hello world")

	buf := &bytes.Buffer{}
	if err := WriteMessage(buf, header, payload); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	gotHeader, gotPayload, err := ReadMessage(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if gotHeader.Type != header.Type || gotHeader.Sequence != header.Sequence {
		t.Fatalf("header mismatch: %+v vs %+v", gotHeader, header)
	}
	if !bytes.Equal(gotPayload, payload) {
		t.Fatalf("payload mismatch: %q vs %q", gotPayload, payload)
	}
}

func TestReadMessageInvalidMagic(t *testing.T) {
	data := make([]byte, headerSize)
	buf := bytes.NewReader(data)
	if _, _, err := ReadMessage(buf); !errors.Is(err, ErrInvalidMagic) {
		t.Fatalf("expected ErrInvalidMagic, got %v", err)
	}
}

func TestChecksumMismatch(t *testing.T) {
	var session [16]byte
	header := Header{Version: Version, Type: MsgPing, Flags: FlagChecksum, SessionID: session}
	payload := []byte("ping")
	buf := &bytes.Buffer{}

	if err := WriteMessage(buf, header, payload); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	raw := buf.Bytes()
	raw[len(raw)-1] ^= 0xFF // flip a payload byte

	if _, _, err := ReadMessage(bytes.NewReader(raw)); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestUnsupportedVersion(t *testing.T) {
	var session [16]byte
	header := Header{Version: Version, Type: MsgHello, SessionID: session}
	buf := &bytes.Buffer{}
	if err := WriteMessage(buf, header, nil); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	data := buf.Bytes()
	data[4] = Version + 1

	if _, _, err := ReadMessage(bytes.NewReader(data)); !errors.Is(err, ErrUnsupportedVer) {
		t.Fatalf("expected unsupported version, got %v", err)
	}
}

func TestShortPayload(t *testing.T) {
	var session [16]byte
	header := Header{Version: Version, Type: MsgHello, Flags: FlagChecksum, SessionID: session}
	payload := []byte("payload")
	buf := &bytes.Buffer{}
	if err := WriteMessage(buf, header, payload); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	truncated := buf.Bytes()[:headerSize+2]
	if _, _, err := ReadMessage(bytes.NewReader(truncated)); !errors.Is(err, ErrShortPayload) {
		t.Fatalf("expected short payload error, got %v", err)
	}
}
