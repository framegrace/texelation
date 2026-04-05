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
		// wrapEnabled defaults to true in NewVTerm
		if !v.WrapEnabled() {
			t.Error("WrapEnabled should be true by default")
		}
	})

	t.Run("WithWrap false", func(t *testing.T) {
		v := NewVTerm(80, 24, WithWrap(false))
		if v.WrapEnabled() {
			t.Error("WrapEnabled should be false when created with WithWrap(false)")
		}
	})

	t.Run("WithWrap true", func(t *testing.T) {
		v := NewVTerm(80, 24, WithWrap(true))
		if !v.WrapEnabled() {
			t.Error("WrapEnabled should be true when created with WithWrap(true)")
		}
	})

	t.Run("WithReflow sets wrapEnabled", func(t *testing.T) {
		v := NewVTerm(80, 24, WithReflow(false))
		if v.WrapEnabled() {
			t.Error("WithReflow(false) should disable wrapEnabled")
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
