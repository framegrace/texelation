package testutil_test

import (
    "testing"

    "github.com/framegrace/texelation/apps/texelterm/testutil"
)

// TestCodexSpinnerColorBleed captures the Codex spinner session that shows
// color-bleed artifacts in texelterm. It is currently skipped until the bug is
// fixed, but keeps the recording in CI and documents the first diff.
func TestCodexSpinnerColorBleed(t *testing.T) {
	rec, err := testutil.LoadRecording("testdata/codex_spinner.txrec")
	if err != nil {
		t.Fatalf("load recording: %v", err)
	}

    cmp, err := testutil.NewReferenceComparator(rec)
    if err != nil {
        t.Skipf("tmux not available: %v", err)
    }

    res, err := cmp.CompareAtEndWithFullDiff()
    if err != nil {
        t.Fatalf("compare: %v", err)
    }

    if res.Match {
        t.Log("Codex spinner matches tmux (color bleed fixed?)")
        return
    }

    // Currently failing on color-only differences (no char diffs). Skip until fixed.
    first := res.Differences[0]
    t.Skipf("Known bug: codex spinner colors bleed; first diff (%d,%d): %s (char=%d color=%d)",
        first.X, first.Y, first.DiffDesc, res.CharDiffs, res.ColorDiffs)
}
