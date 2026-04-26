// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Server-side cross-daemon-restart session/viewport persistence.
// One file per session at <basedir>/sessions/<hex-sessionID>.json.
// See docs/superpowers/specs/2026-04-26-issue-199-plan-d2-server-viewport-persistence-design.md.

package server

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/framegrace/texelation/internal/persistence/atomicjson"
)

// StoredSessionSchemaVersion is the on-disk format version. Bump on
// incompatible changes; older files are deleted on boot scan with a log
// line (project has no back-compat constraint).
const StoredSessionSchemaVersion = 1

// StoredSession is the on-disk representation of cross-restart session
// state. Latest-wins snapshot — there is no replay log.
//
// JSON encoding routes through MarshalJSON / UnmarshalJSON via
// sessionJSONShape, so struct tags here would be ignored. They're
// omitted to make that fact obvious and avoid future confusion.
type StoredSession struct {
	SchemaVersion int
	SessionID     [16]byte
	LastActive    time.Time
	Pinned        bool
	PaneViewports []StoredPaneViewport
	// Plan F metadata (populated at write time; no consumers in D2):
	Label          string
	PaneCount      int
	FirstPaneTitle string
}

// StoredPaneViewport is the per-pane element. JSON encoding for this
// type also routes through paneViewportJSONShape (PaneID is
// hex-encoded), so struct tags would be ignored — omitted for clarity.
type StoredPaneViewport struct {
	PaneID         [16]byte
	AltScreen      bool
	AutoFollow     bool
	ViewBottomIdx  int64
	WrapSegmentIdx uint16
	Rows           uint16
	Cols           uint16
}

type sessionJSONShape struct {
	SchemaVersion  int                     `json:"schemaVersion"`
	SessionID      string                  `json:"sessionID"`
	LastActive     time.Time               `json:"lastActive"`
	Pinned         bool                    `json:"pinned"`
	PaneViewports  []paneViewportJSONShape `json:"paneViewports"`
	Label          string                  `json:"label"`
	PaneCount      int                     `json:"paneCount"`
	FirstPaneTitle string                  `json:"firstPaneTitle"`
}

type paneViewportJSONShape struct {
	PaneID         string `json:"paneID"`
	AltScreen      bool   `json:"altScreen"`
	AutoFollow     bool   `json:"autoFollow"`
	ViewBottomIdx  int64  `json:"viewBottomIdx"`
	WrapSegmentIdx uint16 `json:"wrapSegmentIdx"`
	Rows           uint16 `json:"rows"`
	Cols           uint16 `json:"cols"`
}

func (s StoredSession) MarshalJSON() ([]byte, error) {
	out := sessionJSONShape{
		SchemaVersion:  s.SchemaVersion,
		SessionID:      hex.EncodeToString(s.SessionID[:]),
		LastActive:     s.LastActive,
		Pinned:         s.Pinned,
		Label:          s.Label,
		PaneCount:      s.PaneCount,
		FirstPaneTitle: s.FirstPaneTitle,
	}
	out.PaneViewports = make([]paneViewportJSONShape, len(s.PaneViewports))
	for i, p := range s.PaneViewports {
		out.PaneViewports[i] = paneViewportJSONShape{
			PaneID:         hex.EncodeToString(p.PaneID[:]),
			AltScreen:      p.AltScreen,
			AutoFollow:     p.AutoFollow,
			ViewBottomIdx:  p.ViewBottomIdx,
			WrapSegmentIdx: p.WrapSegmentIdx,
			Rows:           p.Rows,
			Cols:           p.Cols,
		}
	}
	return json.Marshal(&out)
}

func (s *StoredSession) UnmarshalJSON(data []byte) error {
	var in sessionJSONShape
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	s.SchemaVersion = in.SchemaVersion
	if err := decodeHex16Session(in.SessionID, &s.SessionID); err != nil {
		return fmt.Errorf("sessionID: %w", err)
	}
	s.LastActive = in.LastActive
	s.Pinned = in.Pinned
	s.Label = in.Label
	s.PaneCount = in.PaneCount
	s.FirstPaneTitle = in.FirstPaneTitle
	s.PaneViewports = make([]StoredPaneViewport, len(in.PaneViewports))
	for i, p := range in.PaneViewports {
		var pid [16]byte
		if err := decodeHex16Session(p.PaneID, &pid); err != nil {
			return fmt.Errorf("paneViewports[%d].paneID: %w", i, err)
		}
		s.PaneViewports[i] = StoredPaneViewport{
			PaneID:         pid,
			AltScreen:      p.AltScreen,
			AutoFollow:     p.AutoFollow,
			ViewBottomIdx:  p.ViewBottomIdx,
			WrapSegmentIdx: p.WrapSegmentIdx,
			Rows:           p.Rows,
			Cols:           p.Cols,
		}
	}
	return nil
}

func decodeHex16Session(s string, out *[16]byte) error {
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

// SessionsDirName is the leaf directory under <basedir> that holds
// per-session files.
const SessionsDirName = "sessions"

// SessionFilePath returns the on-disk path for sessionID under basedir.
func SessionFilePath(basedir string, id [16]byte) string {
	return filepath.Join(basedir, SessionsDirName, hex.EncodeToString(id[:])+".json")
}

// ScanSessionsDir reads <basedir>/sessions/, parses each *.json file,
// and returns a map keyed by SessionID. Files that fail to parse or
// declare an unknown SchemaVersion are deleted (logged). Files whose
// filename hex does not match the decoded SessionID are skipped but
// NOT deleted — users may rename files when treating sessions as
// templates. Non-JSON files (anything not matching the *.json
// extension) are left untouched. Missing directory is not an error —
// returns an empty map.
func ScanSessionsDir(basedir string) (map[[16]byte]*StoredSession, error) {
	out := make(map[[16]byte]*StoredSession)
	dir := filepath.Join(basedir, SessionsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("server: scan sessions dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		s, lerr := atomicjson.Load[StoredSession](path)
		if lerr != nil {
			log.Printf("server: session scan: load %s: %v", path, lerr)
			continue
		}
		if s == nil {
			// atomicjson.Load already deleted a corrupt file; nothing more to do.
			continue
		}
		if s.SchemaVersion != StoredSessionSchemaVersion {
			log.Printf("server: session scan: %s schema=%d wanted=%d; deleting",
				path, s.SchemaVersion, StoredSessionSchemaVersion)
			if werr := atomicjson.Wipe(path); werr != nil {
				log.Printf("server: session scan: wipe failed: %v", werr)
			}
			continue
		}
		// Filename-vs-content sanity check. If the filename hex doesn't
		// match the decoded SessionID, the user likely renamed the file
		// (e.g. as a template — sessions can be reused per project policy,
		// see spec). Skip it without loading and WITHOUT deleting — D2
		// must not silently destroy files that look user-touched.
		expectedName := hex.EncodeToString(s.SessionID[:]) + ".json"
		if name != expectedName {
			log.Printf("server: session scan: %s filename does not match sessionID %s; skipping (file left in place)",
				name, expectedName)
			continue
		}
		out[s.SessionID] = s
	}
	return out, nil
}
