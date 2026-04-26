// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration
// +build integration

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// TestD2_FullCrossRestartCycle exercises the daemon-restart resume path
// end-to-end at the unit level: write a session file as if a previous
// daemon had persisted state, simulate boot scan, and verify a fresh
// Manager rehydrates the session and a subsequent resume seeds viewports.
func TestD2_FullCrossRestartCycle(t *testing.T) {
	dir := t.TempDir()

	// === Phase A: previous daemon writes a session file ===
	id := [16]byte{0xca, 0xfe}
	prevMgr := NewManager()
	if err := prevMgr.EnablePersistence(dir, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	prevSess, err := prevMgr.NewSessionWithID(id)
	if err != nil {
		t.Fatal(err)
	}
	prevSess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID:        [16]byte{0xab, 0xcd},
		ViewBottomIdx: 7777,
		Rows:          30,
		Cols:          100,
		AutoFollow:    false,
	})
	prevSess.RecordPaneActivity(2, "bash") // Plan F metadata
	prevSess.Close()                       // flushes synchronously

	// === Phase B: new daemon boots, scans, indexes ===
	newMgr := NewManager()
	if err := newMgr.EnablePersistence(dir, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	// === Phase C: resume request lands and rehydrates ===
	sess, _, err := newMgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatalf("LookupOrRehydrate: %v", err)
	}
	defer sess.Close()
	if sess.ID() != id {
		t.Fatalf("rehydrated wrong session: %x", sess.ID())
	}
	vp, ok := sess.Viewport([16]byte{0xab, 0xcd})
	if !ok {
		t.Fatalf("expected pre-seeded viewport for pane")
	}
	if vp.ViewBottomIdx != 7777 || vp.Rows != 30 || vp.Cols != 100 {
		t.Fatalf("viewport fields wrong: %+v", vp)
	}

	// === Phase D: client's MsgResumeRequest carries fresher data ===
	sess.ApplyResume([]protocol.PaneViewportState{{
		PaneID:        [16]byte{0xab, 0xcd},
		ViewBottomIdx: 8000, // fresher than disk's 7777
		ViewportRows:  30,
		ViewportCols:  100,
		AutoFollow:    false,
	}}, nil)
	vp, _ = sess.Viewport([16]byte{0xab, 0xcd})
	if vp.ViewBottomIdx != 8000 {
		t.Fatalf("client-fresh value should win over pre-seed; got %d", vp.ViewBottomIdx)
	}

	// === Phase E: live update writes back to disk ===
	sess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID:        [16]byte{0xab, 0xcd},
		ViewBottomIdx: 8500,
		Rows:          30,
		Cols:          100,
	})
	sess.RecordPaneActivity(3, "vim") // refresh Plan F metadata after pane add
	sess.FlushPersistForTest()

	data, err := os.ReadFile(SessionFilePath(dir, id))
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	var stored StoredSession
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}

	// === Plan F metadata round-tripped through the full cycle ===
	if stored.PaneCount != 3 {
		t.Errorf("PaneCount: got %d want 3", stored.PaneCount)
	}
	if stored.FirstPaneTitle != "vim" {
		t.Errorf("FirstPaneTitle: got %q want vim", stored.FirstPaneTitle)
	}

	found := false
	for _, p := range stored.PaneViewports {
		if p.PaneID == [16]byte{0xab, 0xcd} && p.ViewBottomIdx == 8500 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected updated viewport persisted, got %+v", stored.PaneViewports)
	}
}

// TestD2_PinnedRoundTrip verifies that hand-edited Pinned=true on disk
// survives the boot scan and round-trips through the loaded index.
func TestD2_PinnedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x11, 0x22}
	stored := StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Now().UTC(),
		Pinned:        true,
	}
	path := SessionFilePath(dir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(&stored)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	sess, _, err := mgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	defer sess.Close()
	if sess.ID() != id {
		t.Fatalf("wrong session: %x", sess.ID())
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session file should remain after rehydrate: %v", err)
	}

	sess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID: [16]byte{0xff}, ViewBottomIdx: 1, Rows: 1, Cols: 1,
	})
	sess.FlushPersistForTest()

	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var afterWrite StoredSession
	if err := json.Unmarshal(again, &afterWrite); err != nil {
		t.Fatal(err)
	}
	if !afterWrite.Pinned {
		t.Fatalf("Pinned must round-trip across a write; got %+v", afterWrite)
	}
}

// TestD2_FileSurvivesSessionClose: closing a session does not delete
// the file. (Sessions can serve as templates per project policy.)
func TestD2_FileSurvivesSessionClose(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x33, 0x44}
	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	sess, err := mgr.NewSessionWithID(id)
	if err != nil {
		t.Fatal(err)
	}
	sess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID: [16]byte{0x01}, ViewBottomIdx: 1, Rows: 1, Cols: 1,
	})
	sess.Close()

	if _, err := os.Stat(SessionFilePath(dir, id)); err != nil {
		t.Fatalf("file must survive Close: %v", err)
	}

	mgr2 := NewManager()
	if err := mgr2.EnablePersistence(dir, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, _, err := mgr2.LookupOrRehydrate(id); err != nil {
		t.Fatalf("survived-Close session must rehydrate, got %v", err)
	}
}

// TestD2_ConcurrentUpdates fires Apply* + RecordPaneActivity from many
// goroutines concurrently. Run under -race.
func TestD2_ConcurrentUpdates(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x55, 0x66}
	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 5*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	sess, err := mgr.NewSessionWithID(id)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	const N = 8
	const K = 200
	var wg sync.WaitGroup
	for g := 0; g < N; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < K; i++ {
				sess.ApplyViewportUpdate(protocol.ViewportUpdate{
					PaneID:        [16]byte{byte(g)},
					ViewBottomIdx: int64(i),
					Rows:          24,
					Cols:          80,
				})
				if i%10 == 0 {
					sess.RecordPaneActivity(g+1, "concurrent")
				}
			}
		}(g)
	}
	wg.Wait()
	sess.FlushPersistForTest()

	data, err := os.ReadFile(SessionFilePath(dir, id))
	if err != nil {
		t.Fatalf("file read: %v", err)
	}
	var stored StoredSession
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stored.SessionID != id {
		t.Fatalf("sessionID mismatch under concurrent load: %x", stored.SessionID)
	}
}

// TestD2_PhantomPaneFilterAfterPreSeed.
func TestD2_PhantomPaneFilterAfterPreSeed(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0xee, 0xee}
	realPane := [16]byte{0x01}
	deadPane := [16]byte{0x99}
	stored := StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Now().UTC(),
		PaneViewports: []StoredPaneViewport{
			{PaneID: realPane, ViewBottomIdx: 100, Rows: 24, Cols: 80},
			{PaneID: deadPane, ViewBottomIdx: 200, Rows: 24, Cols: 80},
		},
	}
	path := SessionFilePath(dir, id)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	data, _ := json.Marshal(&stored)
	_ = os.WriteFile(path, data, 0o600)

	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	sess, _, err := mgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	if _, ok := sess.Viewport(realPane); !ok {
		t.Fatalf("real pane viewport missing after pre-seed")
	}
	if _, ok := sess.Viewport(deadPane); !ok {
		t.Fatalf("phantom pane should be pre-seeded too — pruning runs next")
	}

	paneExists := func(p [16]byte) bool { return p == realPane }
	if dropped := sess.viewports.PrunePhantoms(paneExists); dropped != 1 {
		t.Fatalf("PrunePhantoms: got %d, want 1", dropped)
	}

	if _, ok := sess.Viewport(realPane); !ok {
		t.Fatalf("real pane should remain after prune")
	}
	if _, ok := sess.Viewport(deadPane); ok {
		t.Fatalf("dead pane MUST be pruned to prevent disk-side growth")
	}

	clientPayload := []protocol.PaneViewportState{
		{PaneID: realPane, ViewBottomIdx: 150, ViewportRows: 24, ViewportCols: 80},
		{PaneID: [16]byte{0xde, 0xad}, ViewBottomIdx: 999, ViewportRows: 24, ViewportCols: 80},
	}
	sess.ApplyResume(clientPayload, paneExists)

	if vp, _ := sess.Viewport(realPane); vp.ViewBottomIdx != 150 {
		t.Fatalf("realPane: got %d want 150", vp.ViewBottomIdx)
	}
	if _, ok := sess.Viewport([16]byte{0xde, 0xad}); ok {
		t.Fatalf("client-supplied phantom must be filtered by paneExists")
	}

	sess.FlushPersistForTest()
	loaded, _ := os.ReadFile(SessionFilePath(dir, id))
	var afterFlush StoredSession
	if err := json.Unmarshal(loaded, &afterFlush); err != nil {
		t.Fatal(err)
	}
	for _, p := range afterFlush.PaneViewports {
		if p.PaneID == deadPane || p.PaneID == [16]byte{0xde, 0xad} {
			t.Fatalf("phantom pane persisted to disk: %x", p.PaneID)
		}
	}
}

// TestD2_RehydrateRaceForSameID
func TestD2_RehydrateRaceForSameID(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x77, 0x88}
	stored := StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Now().UTC(),
	}
	path := SessionFilePath(dir, id)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	data, _ := json.Marshal(&stored)
	_ = os.WriteFile(path, data, 0o600)

	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	var got [2]*Session
	var errs [2]error
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			got[idx], _, errs[idx] = mgr.LookupOrRehydrate(id)
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("goroutine %d: %v", i, e)
		}
	}
	if got[0] != got[1] {
		t.Fatalf("expected same session pointer from both lookups, got %p vs %p", got[0], got[1])
	}
	if got[0] != nil {
		got[0].Close()
	}
}
