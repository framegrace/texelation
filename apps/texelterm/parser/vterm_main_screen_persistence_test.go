// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"testing"
)

// TestEnableMemoryBuffer_WiresClearNotifier verifies that after
// EnableMemoryBufferWithDisk, ClearRangePersistent on mainScreen propagates
// a delete op to the AdaptivePersistence layer. Before the wiring (Task 9),
// mainScreen.SetClearNotifier is never called, so ClearRangePersistent has no
// notifier and the persistence layer sees no ops. After wiring, the delete op
// is enqueued (checked in BestEffort mode where ops are not flushed eagerly).
func TestEnableMemoryBuffer_WiresClearNotifier(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(80, 24)
	dir := t.TempDir()
	if err := v.EnableMemoryBufferWithDisk(dir, MemoryBufferOptions{
		TerminalID: "wiring-test",
	}); err != nil {
		t.Fatalf("EnableMemoryBufferWithDisk: %v", err)
	}
	defer v.CloseMemoryBuffer()

	if v.mainScreen == nil {
		t.Skip("MainScreenFactory not registered; sparse package not imported")
	}
	if v.mainScreenPersistence == nil {
		t.Fatal("mainScreenPersistence is nil after EnableMemoryBufferWithDisk")
	}

	// Force BestEffort mode so NotifyClearRange enqueues the op without an
	// immediate flush (WriteThrough would flush to zero before we can observe).
	v.mainScreenPersistence.mu.Lock()
	v.mainScreenPersistence.currentMode = PersistBestEffort
	v.mainScreenPersistence.mu.Unlock()

	// Trigger a clear via the public MainScreen API.
	v.mainScreen.ClearRangePersistent(0, 5)

	// If the notifier is wired, the AdaptivePersistence layer must hold at
	// least one pending op (the delete tombstone for [0, 5]).
	if n := v.mainScreenPersistence.PendingOpCount(); n == 0 {
		t.Error("no pending op after ClearRangePersistent; notifier not wired")
	}
}
