// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/persistence.go
// Summary: Client-side session and viewport persistence (issue #199 Plan D).
// Usage: Load on startup before simple.Connect; Save on debounced viewport
//   changes; Wipe on stale-session rejection. Runs in $XDG_STATE_HOME.

package clientruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// DefaultClientName is the slot used when --client-name and
// $TEXELATION_CLIENT_NAME are both unset. Single-client deployments
// touch nothing.
const DefaultClientName = "default"

// ClientNameEnvVar is the env var fallback for --client-name.
const ClientNameEnvVar = "TEXELATION_CLIENT_NAME"

// ClientState is the on-disk schema. Field semantics mirror
// protocol.PaneViewportState; JSON encoding uses lowercase hex for
// [16]byte values (jq-friendly, unlike base64).
type ClientState struct {
	SocketPath    string                       `json:"socketPath"`
	SessionID     [16]byte                     `json:"-"`
	LastSequence  uint64                       `json:"lastSequence"`
	WrittenAt     time.Time                    `json:"writtenAt"`
	PaneViewports []protocol.PaneViewportState `json:"-"`

	// Note: custom marshaler converts [16]byte fields to lowercase hex strings via jsonShape;
	// see MarshalJSON / UnmarshalJSON.
}

// jsonShape is the literal on-disk schema. ClientState wraps it so
// callers see [16]byte fields and the JSON encoding is hex strings.
type jsonShape struct {
	SocketPath    string                  `json:"socketPath"`
	SessionID     string                  `json:"sessionID"`
	LastSequence  uint64                  `json:"lastSequence"`
	WrittenAt     time.Time               `json:"writtenAt"`
	PaneViewports []jsonPaneViewportState `json:"paneViewports"`
}

type jsonPaneViewportState struct {
	PaneID         string `json:"paneID"`
	AltScreen      bool   `json:"altScreen"`
	AutoFollow     bool   `json:"autoFollow"`
	ViewBottomIdx  int64  `json:"viewBottomIdx"`
	WrapSegmentIdx uint16 `json:"wrapSegmentIdx"`
	Rows           uint16 `json:"rows"`
	Cols           uint16 `json:"cols"`
}

func (s ClientState) MarshalJSON() ([]byte, error) {
	out := jsonShape{
		SocketPath:   s.SocketPath,
		SessionID:    hex.EncodeToString(s.SessionID[:]),
		LastSequence: s.LastSequence,
		WrittenAt:    s.WrittenAt,
	}
	out.PaneViewports = make([]jsonPaneViewportState, len(s.PaneViewports))
	for i, p := range s.PaneViewports {
		out.PaneViewports[i] = jsonPaneViewportState{
			PaneID:         hex.EncodeToString(p.PaneID[:]),
			AltScreen:      p.AltScreen,
			AutoFollow:     p.AutoFollow,
			ViewBottomIdx:  p.ViewBottomIdx,
			WrapSegmentIdx: p.WrapSegmentIdx,
			Rows:           p.ViewportRows,
			Cols:           p.ViewportCols,
		}
	}
	return json.Marshal(&out)
}

func (s *ClientState) UnmarshalJSON(data []byte) error {
	var in jsonShape
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	s.SocketPath = in.SocketPath
	if err := decodeHex16(in.SessionID, &s.SessionID); err != nil {
		return fmt.Errorf("sessionID: %w", err)
	}
	s.LastSequence = in.LastSequence
	s.WrittenAt = in.WrittenAt
	s.PaneViewports = make([]protocol.PaneViewportState, len(in.PaneViewports))
	for i, p := range in.PaneViewports {
		var pid [16]byte
		if err := decodeHex16(p.PaneID, &pid); err != nil {
			return fmt.Errorf("paneViewports[%d].paneID: %w", i, err)
		}
		s.PaneViewports[i] = protocol.PaneViewportState{
			PaneID:         pid,
			AltScreen:      p.AltScreen,
			AutoFollow:     p.AutoFollow,
			ViewBottomIdx:  p.ViewBottomIdx,
			WrapSegmentIdx: p.WrapSegmentIdx,
			ViewportRows:   p.Rows,
			ViewportCols:   p.Cols,
		}
	}
	return nil
}

func decodeHex16(s string, out *[16]byte) error {
	b, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return err
	}
	if len(b) != 16 {
		return fmt.Errorf("expected 16 bytes, got %d", len(b))
	}
	copy(out[:], b)
	return nil
}

// ResolvePath returns the on-disk state file path for the given socket
// and client name. Precedence: explicit clientName arg → env
// $TEXELATION_CLIENT_NAME → DefaultClientName.
func ResolvePath(socketPath, clientName string) (string, error) {
	if socketPath == "" {
		return "", errors.New("persistence: empty socketPath")
	}
	abs, err := filepath.Abs(socketPath)
	if err != nil {
		return "", fmt.Errorf("persistence: abs socket path: %w", err)
	}
	name := strings.TrimSpace(clientName)
	if name == "" {
		name = strings.TrimSpace(os.Getenv(ClientNameEnvVar))
	}
	if name == "" {
		name = DefaultClientName
	}
	if !validClientName(name) {
		return "", fmt.Errorf("persistence: invalid clientName %q", name)
	}
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("persistence: home dir: %w", err)
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "texelation", "client", socketHash(abs), name+".json"), nil
}

func socketHash(absSocketPath string) string {
	h := sha256.Sum256([]byte(absSocketPath))
	return hex.EncodeToString(h[:8]) // 16 hex chars
}

// ErrInvalidClientName is returned by ValidateClientName for any name
// that fails the client-name rules. Callers can distinguish "user-input
// is bad" from "environment is bad" (e.g., $HOME unreadable in
// ResolvePath) and report the right thing.
var ErrInvalidClientName = errors.New("persistence: invalid client name")

// ValidateClientName runs the name rules in isolation (no path resolution,
// no $HOME lookup, no socket hashing). Returns ErrInvalidClientName on
// failure so callers in cmd/texelation and cmd/texel-client can wrap it
// with a "invalid --client-name %q" message without misattributing
// HOME-dir or socket errors to the user's flag value.
func ValidateClientName(name string) error {
	if !validClientName(name) {
		return fmt.Errorf("%w: %q", ErrInvalidClientName, name)
	}
	return nil
}

// validClientName rejects path-traversal, shell-meta characters, hidden
// files (leading-dot), and Windows reserved device names. The Makefile
// cross-compiles for Windows, so a client-name like "con" or "nul" must
// not be accepted — opening such a path blocks on win32.
func validClientName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if name[0] == '.' { // hidden / dotfiles
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	// Windows reserved device names (case-insensitive). The reserved list
	// also blocks names with a reserved stem followed by an extension
	// (e.g. "con.json"), so check the stem before the first dot.
	stem := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		stem = name[:i]
	}
	switch strings.ToUpper(stem) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	return true
}

// Save writes state to filePath atomically: write to a sibling .tmp
// file, then os.Rename. Crash mid-write leaves either the old file
// or the new file, never partial.
//
// MkdirAll on the parent dir is idempotent.
func Save(filePath string, state *ClientState) error {
	if state == nil {
		return errors.New("persistence: nil state")
	}
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("persistence: mkdir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".state.tmp-*")
	if err != nil {
		return fmt.Errorf("persistence: tempfile: %w", err)
	}
	tmpPath := tmp.Name()

	// Best-effort cleanup if anything below fails. Success path: rename
	// already consumed tmpPath, so Remove returns ErrNotExist (expected).
	// Failure path: tmpPath should still exist; log if Remove fails for
	// any reason other than ErrNotExist (would indicate filesystem trouble
	// and could otherwise accumulate orphan tmp files silently).
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("persistence: temp file cleanup failed: %v", err)
		}
	}()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(state); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("persistence: encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("persistence: close tmp: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("persistence: rename: %w", err)
	}
	return nil
}
