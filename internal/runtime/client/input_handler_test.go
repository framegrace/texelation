// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import (
	"io"
	"net"
	"os"
	"testing"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
)

func TestIsNetworkClosed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "os.ErrClosed returns true",
			err:  os.ErrClosed,
			want: true,
		},
		{
			name: "io.EOF returns false",
			err:  io.EOF,
			want: false,
		},
		{
			name: "nil error returns false",
			err:  nil,
			want: false,
		},
		{
			name: "generic error returns false",
			err:  io.ErrUnexpectedEOF,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNetworkClosed(tt.err)
			if got != tt.want {
				t.Errorf("isNetworkClosed(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsNetworkClosedWithNetError(t *testing.T) {
	// Test with a custom net.Error that's not a timeout
	nonTimeoutErr := &testNetError{isTimeout: false}
	if !isNetworkClosed(nonTimeoutErr) {
		t.Error("non-timeout net.Error should return true")
	}

	// Test with a timeout error
	timeoutErr := &testNetError{isTimeout: true}
	if isNetworkClosed(timeoutErr) {
		t.Error("timeout net.Error should return false")
	}
}

func TestConsumePasteKey(t *testing.T) {
	tests := []struct {
		name     string
		key      tcell.Key
		rune     rune
		wantByte byte
		wantRune rune
	}{
		{
			name:     "newline rune stored as-is",
			key:      tcell.KeyRune,
			rune:     '\n',
			wantRune: '\n', // Note: In actual usage, KeyEnter handles \r conversion
		},
		{
			name:     "enter key",
			key:      tcell.KeyEnter,
			wantByte: '\r',
		},
		{
			name:     "tab key",
			key:      tcell.KeyTab,
			wantByte: '\t',
		},
		{
			name:     "backspace",
			key:      tcell.KeyBackspace,
			wantByte: 0x7F,
		},
		{
			name:     "backspace2",
			key:      tcell.KeyBackspace2,
			wantByte: 0x7F,
		},
		{
			name:     "escape",
			key:      tcell.KeyEsc,
			wantByte: 0x1b,
		},
		{
			name:     "regular rune",
			key:      tcell.KeyRune,
			rune:     'a',
			wantRune: 'a',
		},
		{
			name:     "unicode character",
			key:      tcell.KeyRune,
			rune:     '世',
			wantRune: '世',
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &clientState{
				pasteBuf: make([]byte, 0, 10),
			}

			ev := tcell.NewEventKey(tt.key, tt.rune, tcell.ModNone)
			consumePasteKey(state, ev)

			if len(state.pasteBuf) == 0 {
				t.Fatal("pasteBuf should not be empty after consumePasteKey")
			}

			if tt.wantByte != 0 {
				// Expecting a single byte
				if len(state.pasteBuf) != 1 {
					t.Errorf("pasteBuf length = %d, want 1", len(state.pasteBuf))
				}
				if state.pasteBuf[0] != tt.wantByte {
					t.Errorf("pasteBuf[0] = 0x%02x, want 0x%02x", state.pasteBuf[0], tt.wantByte)
				}
			} else if tt.wantRune != 0 {
				// Expecting a UTF-8 encoded rune
				decoded, _ := utf8.DecodeRune(state.pasteBuf)
				if decoded != tt.wantRune {
					t.Errorf("decoded rune = %q, want %q", decoded, tt.wantRune)
				}
			}
		})
	}
}

func TestConsumePasteKeyAccumulates(t *testing.T) {
	state := &clientState{
		pasteBuf: make([]byte, 0, 100),
	}

	// Add multiple keys
	keys := []struct {
		key  tcell.Key
		rune rune
	}{
		{tcell.KeyRune, 'h'},
		{tcell.KeyRune, 'e'},
		{tcell.KeyRune, 'l'},
		{tcell.KeyRune, 'l'},
		{tcell.KeyRune, 'o'},
	}

	for _, k := range keys {
		ev := tcell.NewEventKey(k.key, k.rune, tcell.ModNone)
		consumePasteKey(state, ev)
	}

	result := string(state.pasteBuf)
	if result != "hello" {
		t.Errorf("accumulated pasteBuf = %q, want %q", result, "hello")
	}
}

// testNetError implements net.Error for testing
type testNetError struct {
	isTimeout bool
}

func (e *testNetError) Error() string   { return "test network error" }
func (e *testNetError) Timeout() bool   { return e.isTimeout }
func (e *testNetError) Temporary() bool { return false }

// Ensure testNetError implements net.Error
var _ net.Error = (*testNetError)(nil)
