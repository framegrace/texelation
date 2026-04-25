// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/persistence.go
// Summary: Client-side session and viewport persistence (issue #199 Plan D).
// Usage: Load on startup before simple.Connect; Save on debounced viewport
//   changes; Wipe on stale-session rejection. Runs in $XDG_STATE_HOME.

package clientruntime

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
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

	// Hex shadow fields — populated for marshaling, consumed during
	// unmarshaling. See MarshalJSON / UnmarshalJSON.
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
