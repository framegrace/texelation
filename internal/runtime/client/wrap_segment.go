// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/wrap_segment.go
// Summary: computeBottomWrapSegment library helper for issue #199 Plan B.
// Usage: Takes a per-display-row globalIdx slice from a pane render and
//
//	returns the bottom row's sub-row index within its chain (count of
//	consecutive trailing rows sharing the bottom gid, minus 1).
//
// Notes:
//   - Intended for a future renderer that surfaces per-row chain-head
//     gids. The current renderer maps display rows via ViewTopIdx+rowIdx
//     (flat 1:1), so this helper is NOT yet wired into the hot path; it
//     would always return 0 from that source. Plumbing viewportTrackers.
//     SetBottomWrapSegment is available whenever the renderer starts
//     producing repeating-gid sequences.
//   - Interpretation: if the chain extends above the viewport (only
//     partially visible), the returned value is the sub-row index within
//     the VISIBLE portion, not within the chain. The server reconciles
//     this on restore by walking backward `Rows` sub-rows starting from
//     (ViewBottomIdx, WrapSegmentIdx) — see sparse.WalkUpwardFromBottom.

package clientruntime

// computeBottomWrapSegment scans rowGIs upward from the bottom and counts
// consecutive entries sharing the same globalIdx as the bottom row. Returns
// that count minus 1 (the sub-row index of the bottom within the visible
// portion of the chain). Returns 0 for empty input or when the bottom row's
// globalIdx is -1 (blank / border / non-terminal pane row).
func computeBottomWrapSegment(rowGIs []int64) uint16 {
	if len(rowGIs) == 0 {
		return 0
	}
	bottom := rowGIs[len(rowGIs)-1]
	if bottom < 0 {
		return 0
	}
	count := 1
	for i := len(rowGIs) - 2; i >= 0; i-- {
		if rowGIs[i] != bottom {
			break
		}
		count++
	}
	return uint16(count - 1)
}
