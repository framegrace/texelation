// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/manager_session_picker.go
// Summary: Session-picker helpers on Manager — list / rename / delete
// stored sessions for the F.1 recovery flow.

package server

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"

	"github.com/framegrace/texelation/internal/persistence/atomicjson"
	"github.com/framegrace/texelation/internal/thumbnail"
	"github.com/framegrace/texelation/protocol"
)

// StoredSummaries returns a slice of SessionSummary records for every
// known persisted session, sorted pinned-first then LastActive desc.
// HasThumbnail is set per record by stat-ing the on-disk PNG sidecar
// when persistence is enabled; without persistence the flag is always
// false.
//
// Snapshot pattern: copies persistedSessions + persistBasedir under
// the RLock (O(N) memory, O(N) wall-clock), then runs the per-record
// PNG stat OUTSIDE the lock. Holding the RLock during disk stat would
// block every Manager write op (DeleteStored, RenameStored,
// LookupOrRehydrate, Close) for the duration of N stat calls — a real
// stall on a slow filesystem with a populous sessions dir.
func (m *Manager) StoredSummaries() []protocol.SessionSummary {
	m.mu.RLock()
	if len(m.persistedSessions) == 0 {
		m.mu.RUnlock()
		return nil
	}
	persistDir := m.persistBasedir
	type entry struct {
		id  [16]byte
		ref *StoredSession
	}
	snap := make([]entry, 0, len(m.persistedSessions))
	for id, s := range m.persistedSessions {
		snap = append(snap, entry{id: id, ref: s})
	}
	m.mu.RUnlock()

	summaries := make([]protocol.SessionSummary, 0, len(snap))
	for _, e := range snap {
		// Defensively skip empty/ephemeral sessions that may have
		// landed on disk via past versions or odd flush timing —
		// the picker shouldn't show them.
		if e.ref.PaneCount <= 0 && len(e.ref.PaneViewports) == 0 {
			continue
		}
		summary := protocol.SessionSummary{
			SessionID:      e.id,
			Label:          e.ref.Label,
			LastActive:     e.ref.LastActive.Unix(),
			PaneCount:      int32(e.ref.PaneCount),
			FirstPaneTitle: e.ref.FirstPaneTitle,
			Pinned:         e.ref.Pinned,
			Layout:         e.ref.Layout,
		}
		if persistDir != "" {
			pngPath := filepath.Join(persistDir, SessionsDirName, hex.EncodeToString(e.id[:])+".png")
			if info, err := os.Stat(pngPath); err == nil && !info.IsDir() {
				summary.HasThumbnail = true
			}
		}
		summaries = append(summaries, summary)
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].Pinned != summaries[j].Pinned {
			return summaries[i].Pinned
		}
		return summaries[i].LastActive > summaries[j].LastActive
	})
	return summaries
}

// HasSession reports whether id is known to the manager — either as
// a live session or a persisted one. Used by handleRecoverSession to
// validate the user's pick without claiming the session, so the
// subsequent client reconnect can take the normal LookupOrRehydrate
// path (which is what makes resume + handleClientReady fire properly).
func (m *Manager) HasSession(id [16]byte) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, live := m.sessions[id]; live {
		return true
	}
	_, stored := m.persistedSessions[id]
	return stored
}

// LiveSummaries returns the live (in-memory, attached or detached)
// session catalog. F.1 surfaces sessions currently held in m.sessions
// so a user running `--recover` while the daemon already has an
// attached session can still see + pick it. AttachedClients is
// populated from a stub heuristic for F.1 (1 if anyone is attached,
// 0 otherwise — the manager doesn't track per-session client counts
// today); F.2 will replace that with real counts.
//
// Empty/ephemeral sessions (PaneCount==0, no viewports) are skipped
// — same filter as StoredSummaries — so the picker's own dial doesn't
// show up in its own list.
func (m *Manager) LiveSummaries() []protocol.LiveSummary {
	m.mu.RLock()
	if len(m.sessions) == 0 {
		m.mu.RUnlock()
		return nil
	}
	type entry struct {
		id  [16]byte
		ref *Session
	}
	snap := make([]entry, 0, len(m.sessions))
	for id, s := range m.sessions {
		snap = append(snap, entry{id: id, ref: s})
	}
	m.mu.RUnlock()

	out := make([]protocol.LiveSummary, 0, len(snap))
	for _, e := range snap {
		stored := e.ref.CurrentStoredSnapshot()
		if stored.PaneCount <= 0 && len(stored.PaneViewports) == 0 {
			continue
		}
		out = append(out, protocol.LiveSummary{
			SessionID:       e.id,
			Label:           stored.Label,
			AttachedClients: 1, // F.1 stub; F.2 fills in real counts
			PaneCount:       int32(stored.PaneCount),
			LastInputAt:     stored.LastActive.Unix(),
		})
	}
	return out
}

// sessionPNGPath builds the PNG sidecar path for id. Returns empty
// string if persistence is disabled.
func (m *Manager) sessionPNGPath(id [16]byte) string {
	if m.persistBasedir == "" {
		return ""
	}
	return filepath.Join(m.persistBasedir, SessionsDirName, hex.EncodeToString(id[:])+".png")
}

// RenderLiveThumbnailBytes renders a fresh PNG for a live session
// every time it's called — live sessions change state continuously
// (typing, output, focus changes), so any cached PNG would be stale
// the moment it's written. The result is NOT persisted to disk; the
// lifecycle hooks (Close, ShutdownSessions) handle disk persistence
// when the session transitions out of live state.
//
// Returns ok=false when the session isn't live, the renderer is
// unwired, or render/encode fails.
func (m *Manager) RenderLiveThumbnailBytes(id [16]byte) ([]byte, bool) {
	m.mu.RLock()
	_, live := m.sessions[id]
	renderer := m.thumbRenderer
	m.mu.RUnlock()
	if !live || renderer == nil {
		return nil, false
	}
	img, ok := renderer.RenderSessionThumbnail(id)
	if !ok || img == nil {
		return nil, false
	}
	scaled := thumbnail.DownscaleAspectFit(img, 480, 270)
	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// RenameStored updates the persisted session's Label both in-memory
// and on disk. Acquires m.mu so concurrent StoredSummaries / Recover
// calls observe a consistent state. The disk write goes through
// atomicjson.Save to retain the existing crash-safety guarantees.
//
// Returns ErrSessionNotFound when the ID is unknown to the in-memory
// index. On disk-write failures, in-memory state is rolled back so a
// subsequent retry sees the original Label rather than a half-applied
// rename.
func (m *Manager) RenameStored(id [16]byte, newLabel string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.persistedSessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	prev := stored.Label
	stored.Label = newLabel
	if m.persistBasedir == "" {
		return nil
	}
	if err := atomicjson.Save(SessionFilePath(m.persistBasedir, id), stored); err != nil {
		stored.Label = prev
		return fmt.Errorf("manager: rename %x: %w", id[:4], err)
	}
	return nil
}

// DeleteStored removes the session's JSON sidecar, PNG sidecar (if
// present), and the in-memory persisted entry. Refuses with an error
// when the session is currently live — the user must detach all
// clients first.
//
// TOCTOU avoidance: the in-memory delete + the live-session refusal
// happen under m.mu held *contiguously* with markClosing(id), which
// blocks any concurrent rehydrate path until the unlink completes.
//
// PNG removal is best-effort (a missing PNG is not an error); JSON
// removal is authoritative — if the JSON unlink fails we surface the
// error so the caller can retry. The PNG-then-JSON order avoids
// leaving a JSON with stale HasThumbnail expectations across a
// partial delete.
func (m *Manager) DeleteStored(id [16]byte) error {
	m.mu.Lock()
	if _, live := m.sessions[id]; live {
		m.mu.Unlock()
		return fmt.Errorf("manager: session %x is live; detach all clients first", id[:4])
	}
	if _, ok := m.persistedSessions[id]; !ok {
		m.mu.Unlock()
		return ErrSessionNotFound
	}
	delete(m.persistedSessions, id)
	persistDir := m.persistBasedir
	m.markClosing(id)
	m.mu.Unlock()
	defer m.unmarkClosing(id)

	if persistDir == "" {
		return nil
	}
	pngPath := filepath.Join(persistDir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	if err := os.Remove(pngPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("manager: remove %s: %w", pngPath, err)
	}
	jsonPath := SessionFilePath(persistDir, id)
	if err := os.Remove(jsonPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("manager: remove %s: %w", jsonPath, err)
	}
	return nil
}
