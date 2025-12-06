// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/buffer_store_test.go
// Summary: Exercises buffer store behaviour to ensure the core desktop engine remains reliable.
// Usage: Executed during `go test` to guard against regressions.

package texel

import "testing"

func TestInMemoryBufferStore(t *testing.T) {
	store := NewInMemoryBufferStore()
	if store.Snapshot() != nil {
		t.Fatalf("expected empty snapshot")
	}

	buf := [][]Cell{{{Ch: 'a'}}}
	store.Save(buf)

	if store.Snapshot() == nil {
		t.Fatalf("expected snapshot after save")
	}

	store.Clear()
	if store.Snapshot() != nil {
		t.Fatalf("expected snapshot to clear")
	}
}
