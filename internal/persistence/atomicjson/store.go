// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Package atomicjson provides a small set of helpers for persisting
// "latest-wins" snapshot state to disk: atomic temp+rename writes,
// crash-safe loads with corrupt-file recovery, and a debounced writer
// (see Store) shared between the client (Plan D) and server (Plan D2)
// session-state persistence layers.
//
// Crash-safety scope: Save fsyncs the temp file before rename so a
// kernel/power crash leaves either the previous contents or the new
// file, never partial data. The directory is fsynced after rename so
// the rename itself survives a crash. Other processes never observe a
// torn write because the rename is atomic at the VFS layer.
//
// This is NOT an event journal. State is overwritten in place; there
// is no replay. Use the existing terminal write_ahead_log.go for
// append-only data.

package atomicjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// renameFn is the rename primitive used by Save. Defaults to os.Rename
// in production; tests override it to simulate rename-failure paths
// (e.g. cross-device link, EACCES) without needing a real filesystem
// trigger.
//
// PARALLELISM: tests that override renameFn MUST NOT call t.Parallel
// (and must NOT run in a sibling t.Parallel test) — this is global
// mutable state. Use t.Cleanup to restore the default (see
// TestSaveFailingRenameLeavesPriorFile in Task 14b for the pattern).
// Future refactor may move this onto a per-Store option to lift the
// constraint; for now the trade-off is acceptable because rename-
// failure tests are rare and serial.
var renameFn = os.Rename

// Save writes v to path atomically. Sequence:
//  1. Encode JSON into a sibling tmp file in the same directory (so
//     rename stays cheap and atomic at the filesystem level).
//  2. fsync the tmp file so its contents survive a crash.
//  3. Rename tmp over the canonical path (atomic; either old or new).
//  4. fsync the parent directory so the rename itself survives a
//     crash. Failure here is best-effort logged, not returned —
//     the rename succeeded so the data is durable on most ext4/xfs
//     defaults; a missing dir-fsync only matters for very strict
//     ordering guarantees we don't currently need.
//
// The deferred cleanup removes the tmp file on any failure path. On
// success, rename has already consumed the tmp name and Remove returns
// ErrNotExist (silently swallowed).
func Save[T any](path string, v *T) error {
	if v == nil {
		return errors.New("atomicjson: nil payload")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("atomicjson: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".atomicjson.tmp-*")
	if err != nil {
		return fmt.Errorf("atomicjson: tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("atomicjson: temp cleanup failed: %v", err)
		}
	}()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("atomicjson: encode: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("atomicjson: sync tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("atomicjson: close tmp: %w", err)
	}
	if err := renameFn(tmpPath, path); err != nil {
		return fmt.Errorf("atomicjson: rename: %w", err)
	}
	// Best-effort directory fsync so the rename itself survives a
	// crash. Errors are logged but not propagated — the data is
	// already on stable storage from the tmp.Sync above.
	if d, derr := os.Open(dir); derr == nil {
		if serr := d.Sync(); serr != nil {
			log.Printf("atomicjson: dir sync %s: %v", dir, serr)
		}
		_ = d.Close()
	}
	return nil
}

// Load reads a JSON-encoded T from path. Returns:
//   - (nil, nil) if path is missing.
//   - (nil, nil) if path exists but parse fails — the corrupt file is
//     deleted (logged) so the next save replaces it cleanly. Project has
//     no back-compat constraint; auto-migration is explicitly out of scope.
//   - (v, nil) on success.
//   - (nil, err) only on disk-level errors that prevent recovery.
func Load[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("atomicjson: read: %w", err)
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		if werr := Wipe(path); werr != nil {
			log.Printf("atomicjson: parse failed (%v); wipe also failed (%v)", err, werr)
		} else {
			log.Printf("atomicjson: parse failed (%v); file wiped", err)
		}
		return nil, nil
	}
	return &v, nil
}

// Wipe removes path. Idempotent — missing-file is not an error.
func Wipe(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("atomicjson: wipe: %w", err)
	}
	return nil
}

// SaveFunc is the function Store invokes to persist payloads. Defaults
// to Save[T]; tests in the same package may inject alternatives via
// SetSaverForTest.
type SaveFunc[T any] func(path string, v *T) error

// Store is a debounced, latest-wins JSON writer. Concurrent calls to
// Update coalesce: the most recent payload at flush time wins, and the
// in-between values are discarded (intentional — this is snapshot state,
// not an event log).
//
// Concurrency model:
//   - mu protects state/timer/closed/lastSaveErr (lifecycle).
//   - saveMu serializes the actual disk write so tick and Flush can
//     never overlap.
//   - wg tracks tick/flush goroutines so Close blocks until all complete.
//
// Save errors are logged with per-error-string deduplication (one log
// per distinct error every 5 minutes, plus a recovery line on success
// after failure). Crash-loss is bounded by the debounce window.
type Store[T any] struct {
	path     string
	debounce time.Duration

	mu            sync.Mutex
	pending       *T
	timer         *time.Timer
	closed        bool
	lastSaveErr   string
	lastSaveErrAt time.Time

	saveMu sync.Mutex
	wg     sync.WaitGroup
	saver  SaveFunc[T]
}

// NewStore returns a Store that writes JSON-encoded T to path,
// debouncing successive Update calls by debounce.
func NewStore[T any](path string, debounce time.Duration) *Store[T] {
	return &Store[T]{
		path:     path,
		debounce: debounce,
		saver:    Save[T],
	}
}

// SetSaverForTest swaps the underlying save implementation. Only for
// in-package tests; production code uses the default Save[T].
func (s *Store[T]) SetSaverForTest(fn SaveFunc[T]) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saver = fn
}

// Update schedules a debounced save with v as the new pending value.
// Calls after Close are silently dropped.
func (s *Store[T]) Update(v T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	cp := v
	s.pending = &cp
	if s.timer == nil {
		s.timer = time.AfterFunc(s.debounce, s.tick)
	}
}

// Flush writes any pending value synchronously.
func (s *Store[T]) Flush() {
	s.wg.Add(1)
	defer s.wg.Done()

	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	v := s.pending
	s.pending = nil
	s.mu.Unlock()

	if v != nil {
		s.doSave(*v)
	}
}

// Close flushes any pending state, blocks for in-flight ticks, and
// rejects subsequent Update calls.
func (s *Store[T]) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()

	s.Flush()
	s.wg.Wait()
}

// tick is the AfterFunc callback. The wg counter is incremented inside
// tick rather than at scheduling time because timer.Stop in Close
// cannot reliably distinguish "stopped before fire" from "fire pending
// but not yet running" — tracking the lifecycle in the goroutine
// itself keeps the wg balance simple at the cost of a small benign
// race window: Close may return before a freshly-fired tick has called
// wg.Add. The mu-protected closed check at the top of tick ensures no
// I/O happens after Close in any case, so the race is observably a
// no-op even when it occurs.
func (s *Store[T]) tick() {
	s.wg.Add(1)
	defer s.wg.Done()

	s.mu.Lock()
	if s.closed || s.pending == nil {
		s.timer = nil
		s.mu.Unlock()
		return
	}
	v := *s.pending
	s.pending = nil
	s.timer = nil
	s.mu.Unlock()

	s.doSave(v)

	s.mu.Lock()
	if s.pending != nil && !s.closed && s.timer == nil {
		s.timer = time.AfterFunc(s.debounce, s.tick)
	}
	s.mu.Unlock()
}

const saveErrRelogInterval = 5 * time.Minute

func (s *Store[T]) doSave(v T) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	err := s.saver(s.path, &v)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		es := err.Error()
		now := time.Now()
		if es != s.lastSaveErr || now.Sub(s.lastSaveErrAt) >= saveErrRelogInterval {
			log.Printf("atomicjson: save failed (%v); will retry on next change", err)
			s.lastSaveErr = es
			s.lastSaveErrAt = now
		}
		return
	}
	if s.lastSaveErr != "" {
		log.Printf("atomicjson: save recovered after prior failure")
		s.lastSaveErr = ""
		s.lastSaveErrAt = time.Time{}
	}
}
