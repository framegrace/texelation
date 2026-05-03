// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/framegrace/texelation/internal/persistence/atomicjson"
)

func pickerHexID(id [16]byte) string {
	return hex.EncodeToString(id[:])
}

func TestStoredSummaries_EmptyOnFreshManager(t *testing.T) {
	m := NewManager()
	got := m.StoredSummaries()
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d entries", len(got))
	}
}

func TestStoredSummaries_OrderedPinnedThenLastActive(t *testing.T) {
	m := NewManager()
	m.SetPersistedSessions(map[[16]byte]*StoredSession{
		{0x01}: {SessionID: [16]byte{0x01}, Label: "old-pinned", LastActive: time.Unix(100, 0), Pinned: true, PaneCount: 1},
		{0x02}: {SessionID: [16]byte{0x02}, Label: "newest", LastActive: time.Unix(300, 0), PaneCount: 2},
		{0x03}: {SessionID: [16]byte{0x03}, Label: "middle", LastActive: time.Unix(200, 0), PaneCount: 3},
	})
	got := m.StoredSummaries()
	if len(got) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(got))
	}
	if got[0].Label != "old-pinned" {
		t.Fatalf("[0] expected pinned first, got %q", got[0].Label)
	}
	if got[1].Label != "newest" {
		t.Fatalf("[1] expected newest second, got %q", got[1].Label)
	}
	if got[2].Label != "middle" {
		t.Fatalf("[2] expected middle third, got %q", got[2].Label)
	}
}

func TestStoredSummaries_HasThumbnailFlag(t *testing.T) {
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, SessionsDirName)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	withPNG := [16]byte{0x10}
	withoutPNG := [16]byte{0x20}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// Write the PNG AFTER EnablePersistence so the boot scan's
	// orphan cleanup doesn't reap it (no matching JSON exists).
	if err := os.WriteFile(filepath.Join(sessionsDir, pickerHexID(withPNG)+".png"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	m.SetPersistedSessions(map[[16]byte]*StoredSession{
		withPNG:    {SessionID: withPNG, LastActive: time.Unix(200, 0), PaneCount: 1},
		withoutPNG: {SessionID: withoutPNG, LastActive: time.Unix(100, 0), PaneCount: 1},
	})
	got := m.StoredSummaries()
	by := make(map[[16]byte]bool)
	for _, s := range got {
		by[s.SessionID] = s.HasThumbnail
	}
	if !by[withPNG] {
		t.Errorf("expected HasThumbnail=true for %x", withPNG)
	}
	if by[withoutPNG] {
		t.Errorf("expected HasThumbnail=false for %x", withoutPNG)
	}
}

func TestLiveSummaries_EmptyInF1(t *testing.T) {
	m := NewManager()
	if got := m.LiveSummaries(); got != nil {
		t.Fatalf("expected nil (F.2 will populate), got %#v", got)
	}
}

func TestRenameStored_UpdatesInMemoryAndDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := [16]byte{0xAA}
	original := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		Label:         "before",
		PaneCount:     1,
	}
	data, _ := json.Marshal(original)
	if err := os.WriteFile(SessionFilePath(dir, id), data, 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable persistence: %v", err)
	}
	if err := m.RenameStored(id, "after"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got := m.StoredSummaries()
	if len(got) != 1 || got[0].Label != "after" {
		t.Fatalf("in-memory rename failed; got %#v", got)
	}
	reloaded, err := atomicjson.Load[StoredSession](SessionFilePath(dir, id))
	if err != nil || reloaded == nil {
		t.Fatalf("reload: err=%v reloaded=%v", err, reloaded)
	}
	if reloaded.Label != "after" {
		t.Fatalf("on-disk Label=%q, want %q", reloaded.Label, "after")
	}
}

func TestRenameStored_UnknownID(t *testing.T) {
	m := NewManager()
	if err := m.EnablePersistence(t.TempDir(), 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := m.RenameStored([16]byte{0xFF}, "nope"); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestDeleteStored_RemovesBothFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := [16]byte{0xBB}
	jsonPath := SessionFilePath(dir, id)
	pngPath := filepath.Join(dir, SessionsDirName, pickerHexID(id)+".png")
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		PaneCount:     1,
	}
	data, _ := json.Marshal(stored)
	os.WriteFile(jsonPath, data, 0o644)
	os.WriteFile(pngPath, []byte("fake-png"), 0o644)
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := m.DeleteStored(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Errorf("expected JSON gone, stat err=%v", err)
	}
	if _, err := os.Stat(pngPath); !os.IsNotExist(err) {
		t.Errorf("expected PNG gone, stat err=%v", err)
	}
	if got := m.StoredSummaries(); len(got) != 0 {
		t.Errorf("expected empty after delete, got %d entries", len(got))
	}
}

func TestDeleteStored_PNGMissingStillSucceeds(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := [16]byte{0xCC}
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		PaneCount:     1,
	}
	data, _ := json.Marshal(stored)
	os.WriteFile(SessionFilePath(dir, id), data, 0o644)
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := m.DeleteStored(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestDeleteStored_RefusesLive(t *testing.T) {
	id := [16]byte{0xDD}
	m := NewManager()
	if _, err := m.NewSessionWithID(id); err != nil {
		t.Fatalf("create live session: %v", err)
	}
	err := m.DeleteStored(id)
	if err == nil {
		t.Fatalf("expected error refusing live session, got nil")
	}
}
