// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import "testing"

func TestComputeBottomWrapSegment_FlatRow(t *testing.T) {
	rowGIs := []int64{10, 11, 12, 13}
	got := computeBottomWrapSegment(rowGIs)
	if got != 0 {
		t.Fatalf("got %d want 0", got)
	}
}

func TestComputeBottomWrapSegment_WrappedTail(t *testing.T) {
	rowGIs := []int64{10, 11, 20, 20}
	got := computeBottomWrapSegment(rowGIs)
	if got != 1 {
		t.Fatalf("got %d want 1", got)
	}
}

func TestComputeBottomWrapSegment_AllSameGid(t *testing.T) {
	rowGIs := []int64{50, 50, 50, 50}
	got := computeBottomWrapSegment(rowGIs)
	if got != 3 {
		t.Fatalf("got %d want 3", got)
	}
}

func TestComputeBottomWrapSegment_Empty(t *testing.T) {
	if got := computeBottomWrapSegment(nil); got != 0 {
		t.Fatalf("nil: got %d want 0", got)
	}
	if got := computeBottomWrapSegment([]int64{}); got != 0 {
		t.Fatalf("empty: got %d want 0", got)
	}
}

func TestComputeBottomWrapSegment_InvalidGid(t *testing.T) {
	rowGIs := []int64{10, 11, 12, -1}
	if got := computeBottomWrapSegment(rowGIs); got != 0 {
		t.Fatalf("got %d want 0", got)
	}
}
