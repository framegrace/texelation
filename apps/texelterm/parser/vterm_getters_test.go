// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import "testing"

func TestVTermGetters(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		v := NewVTerm(80, 24)

		if v.InsertMode() {
			t.Error("InsertMode should be false by default")
		}
	})
}

func TestVTermIsInTUIMode(t *testing.T) {
	v := NewVTerm(80, 24)

	// Without a memory buffer, fixedWidthDetector returns nil,
	// so IsInTUIMode should be false.
	if v.IsInTUIMode() {
		t.Error("IsInTUIMode should be false by default (no fixed width detector)")
	}
}
