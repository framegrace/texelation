// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/server_boot.go
// Summary: Boot-time capture loading and periodic snapshot persistence.
// Usage: Methods on *Server for loading/applying snapshots at startup and persisting them periodically.
// Notes: Extracted from server.go to isolate boot and snapshot lifecycle logic.

package server

import (
	"log"
	"os"
	"time"

	"github.com/framegrace/texelation/texel"
)

func (s *Server) loadBootSnapshot() {
	log.Printf("[BOOT] loadBootSnapshot called, snapshotStore=%v", s.snapshotStore != nil)
	if s.snapshotStore == nil {
		return
	}
	stored, err := s.snapshotStore.Load()
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[BOOT] snapshot file does not exist")
			return
		}
		log.Printf("snapshot load failed: %v", err)
		return
	}

	// Convert to TreeCapture which supports multiple workspaces
	capture := stored.ToTreeCapture()

	log.Printf("[BOOT] Loaded snapshot with %d panes, workspaces=%d", len(capture.Panes), len(capture.WorkspaceRoots))
	if len(capture.Panes) == 0 {
		log.Printf("[BOOT] No panes in snapshot, skipping")
		return
	}

	// Convert back to protocol format for initial client handshake (active workspace only)
	// We do this so setBootSnapshot still works for clients
	snapshot := stored.ToTreeSnapshot()
	s.setBootSnapshot(snapshot)

	// Apply the full capture
	s.applyBootCapture(capture)
}

// applyBootCapture applies the full multi-workspace capture
func (s *Server) applyBootCapture(capture texel.TreeCapture) {
	log.Printf("[BOOT] applyBootCapture called, desktopSink=%v", s.desktopSink != nil)
	if s.desktopSink == nil {
		return
	}
	desktop := s.desktopSink.Desktop()
	if desktop == nil {
		log.Printf("[BOOT] desktop is nil, cannot apply snapshot")
		return
	}
	log.Printf("[BOOT] Applying tree capture with %d panes", len(capture.Panes))
	if err := desktop.ApplyTreeCapture(capture); err != nil {
		log.Printf("apply boot snapshot failed: %v", err)
	} else {
		log.Printf("[BOOT] Successfully applied boot snapshot")
	}
}

// Deprecated: use applyBootCapture
func (s *Server) applyBootSnapshot() {
	// Re-load snapshot to get full capture if possible, or fallback to saved protocol snapshot
	if s.snapshotStore != nil {
		s.loadBootSnapshot()
	}
}

func (s *Server) startSnapshotLoop() {
	if s.snapshotStore == nil || s.desktopSink == nil {
		return
	}
	interval := s.snapshotInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	s.snapshotQuit = make(chan struct{})
	ticker := time.NewTicker(interval)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer ticker.Stop()
		// Ensure we save one last time when the loop exits
		defer s.persistSnapshot()

		for {
			select {
			case <-ticker.C:
				s.persistSnapshot()
			case <-s.snapshotQuit:
				return
			case <-s.quit:
				return
			}
		}
	}()
	// Do NOT save immediately on startup - we might be restoring!
	// s.persistSnapshot()
}

func (s *Server) persistSnapshot() {
	if s.snapshotStore == nil || s.desktopSink == nil {
		return
	}
	desktop := s.desktopSink.Desktop()
	if desktop == nil {
		return
	}
	capture := desktop.CaptureTree()
	if len(capture.Panes) == 0 {
		return
	}
	if err := s.snapshotStore.Save(&capture); err != nil {
		log.Printf("snapshot save failed: %v", err)
	} else {
		debugLog.Printf("Snapshot saved with %d panes", len(capture.Panes))
	}
	s.setBootSnapshot(treeCaptureToProtocol(capture))
}

// LoadPersistedSessions runs ScanSessionsDir against basedir and seeds
// the manager's rehydration index. Failure to scan (e.g., disk error)
// is non-fatal — the server boots without rehydration support and
// future MsgResumeRequest for unknown IDs falls through to
// ErrSessionNotFound, exactly as before Plan D2.
func LoadPersistedSessions(mgr *Manager, basedir string) error {
	if basedir == "" {
		return nil
	}
	loaded, err := ScanSessionsDir(basedir)
	if err != nil {
		log.Printf("server: persisted session scan failed: %v", err)
		return err
	}
	mgr.SetPersistedSessions(loaded)
	if len(loaded) > 0 {
		log.Printf("[BOOT] loaded %d persisted session(s) from %s", len(loaded), basedir)
	}
	return nil
}
