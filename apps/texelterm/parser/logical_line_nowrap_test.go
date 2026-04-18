package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogicalLine_NoWrap_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lhist")

	in := []*LogicalLine{
		{Cells: []Cell{{Rune: 'a'}}, NoWrap: true},
		{Cells: []Cell{{Rune: 'b'}}, NoWrap: false},
	}
	if err := WriteLogicalLines(path, in); err != nil {
		t.Fatal(err)
	}

	out, err := LoadLogicalLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(out))
	}
	if !out[0].NoWrap {
		t.Errorf("line 0: NoWrap flag did not round-trip")
	}
	if out[1].NoWrap {
		t.Errorf("line 1: NoWrap should be false, got true")
	}
}

func TestLogicalLine_NoWrap_BackwardCompatibility(t *testing.T) {
	// Old files (without NoWrap flag set) should load with NoWrap=false.
	dir := t.TempDir()
	path := filepath.Join(dir, "old.lhist")

	// Write an LL without NoWrap (flag byte won't have 0x08 set).
	in := []*LogicalLine{{Cells: []Cell{{Rune: 'x'}}}}
	if err := WriteLogicalLines(path, in); err != nil {
		t.Fatal(err)
	}

	// Simulate reading — flag 0x08 is absent.
	out, err := LoadLogicalLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].NoWrap {
		t.Errorf("default NoWrap should be false")
	}

	_ = os.Remove(path)
}

func TestLogicalLine_Clone_PreservesNoWrap(t *testing.T) {
	ll := &LogicalLine{Cells: []Cell{{Rune: 'a'}}, NoWrap: true}
	clone := ll.Clone()
	if !clone.NoWrap {
		t.Errorf("Clone should preserve NoWrap")
	}
}
