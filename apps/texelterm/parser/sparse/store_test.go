package sparse

import (
	"sync"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestStore_NewStore(t *testing.T) {
	s := NewStore(80)
	if s == nil {
		t.Fatal("NewStore returned nil")
	}
	if got := s.Width(); got != 80 {
		t.Errorf("Width() = %d, want 80", got)
	}
	if got := s.Max(); got != -1 {
		t.Errorf("Max() of empty store = %d, want -1", got)
	}
	_ = parser.Cell{} // Keep the import; used in later tests
}

func TestStore_SetGetSingleCell(t *testing.T) {
	s := NewStore(10)
	cell := parser.Cell{Rune: 'A'}
	s.Set(5, 3, cell)

	got := s.Get(5, 3)
	if got.Rune != 'A' {
		t.Errorf("Get(5,3).Rune = %q, want %q", got.Rune, 'A')
	}
	if got := s.Max(); got != 5 {
		t.Errorf("Max() after Set(5,*) = %d, want 5", got)
	}
}

func TestStore_GetMissingReturnsBlank(t *testing.T) {
	s := NewStore(10)
	got := s.Get(0, 0)
	if got.Rune != 0 {
		t.Errorf("Get on empty Store returned rune %q, want 0", got.Rune)
	}
	got = s.Get(999, 7)
	if got.Rune != 0 {
		t.Errorf("Get(999,7) on empty Store returned rune %q, want 0", got.Rune)
	}
}

func TestStore_SetExtendsBeyondExistingLine(t *testing.T) {
	s := NewStore(80)
	s.Set(0, 0, parser.Cell{Rune: 'X'})
	s.Set(0, 40, parser.Cell{Rune: 'Y'})
	if got := s.Get(0, 0).Rune; got != 'X' {
		t.Errorf("Get(0,0) = %q, want X", got)
	}
	if got := s.Get(0, 40).Rune; got != 'Y' {
		t.Errorf("Get(0,40) = %q, want Y", got)
	}
	if got := s.Get(0, 20).Rune; got != 0 {
		t.Errorf("Get(0,20) = %q, want blank", got)
	}
}

func TestStore_MaxNeverDecreases(t *testing.T) {
	s := NewStore(10)
	s.Set(10, 0, parser.Cell{Rune: 'A'})
	s.Set(5, 0, parser.Cell{Rune: 'B'})
	if got := s.Max(); got != 10 {
		t.Errorf("Max() after writing higher then lower = %d, want 10", got)
	}
}

func TestStore_SetLineGetLine(t *testing.T) {
	s := NewStore(10)
	line := []parser.Cell{
		{Rune: 'h'}, {Rune: 'i'}, {Rune: '!'},
	}
	s.SetLine(3, line)

	got := s.GetLine(3)
	if len(got) != 3 {
		t.Fatalf("GetLine(3) len = %d, want 3", len(got))
	}
	if got[0].Rune != 'h' || got[1].Rune != 'i' || got[2].Rune != '!' {
		t.Errorf("GetLine(3) runes = %q,%q,%q; want h,i,!",
			got[0].Rune, got[1].Rune, got[2].Rune)
	}
}

func TestStore_SetLineOverwritesExistingCells(t *testing.T) {
	s := NewStore(10)
	s.Set(0, 5, parser.Cell{Rune: 'X'}) // existing cell at col 5
	s.SetLine(0, []parser.Cell{{Rune: 'A'}, {Rune: 'B'}})

	line := s.GetLine(0)
	if len(line) != 2 {
		t.Fatalf("GetLine(0) len = %d, want 2 (SetLine replaces, not merges)", len(line))
	}
}

func TestStore_GetLineDoesNotAffectAdjacent(t *testing.T) {
	s := NewStore(10)
	s.SetLine(5, []parser.Cell{{Rune: 'X'}})
	if got := s.GetLine(4); got != nil && len(got) != 0 {
		t.Errorf("GetLine(4) = %v, want empty/nil", got)
	}
	if got := s.GetLine(6); got != nil && len(got) != 0 {
		t.Errorf("GetLine(6) = %v, want empty/nil", got)
	}
}

func TestStore_GetLineReturnsCopy(t *testing.T) {
	s := NewStore(10)
	s.SetLine(0, []parser.Cell{{Rune: 'A'}})
	line := s.GetLine(0)
	line[0].Rune = 'Z' // mutate returned slice
	if got := s.Get(0, 0).Rune; got != 'A' {
		t.Errorf("Store was mutated by caller: Get(0,0) = %q, want A", got)
	}
}

func TestStore_ClearRangeRemovesOnlyTargets(t *testing.T) {
	s := NewStore(10)
	s.SetLine(0, []parser.Cell{{Rune: 'A'}})
	s.SetLine(5, []parser.Cell{{Rune: 'B'}})
	s.SetLine(10, []parser.Cell{{Rune: 'C'}})

	s.ClearRange(3, 7) // inclusive range

	if got := s.GetLine(0); got == nil || got[0].Rune != 'A' {
		t.Errorf("line 0 should be preserved, got %v", got)
	}
	if got := s.GetLine(5); got != nil && len(got) > 0 && got[0].Rune != 0 {
		t.Errorf("line 5 should be cleared, got %v", got)
	}
	if got := s.GetLine(10); got == nil || got[0].Rune != 'C' {
		t.Errorf("line 10 should be preserved, got %v", got)
	}
}

func TestStore_ClearRangeKeepsContentEnd(t *testing.T) {
	s := NewStore(10)
	s.SetLine(20, []parser.Cell{{Rune: 'X'}})
	s.ClearRange(20, 20)
	if got := s.Max(); got != 20 {
		t.Errorf("Max() after ClearRange = %d, want 20 (contentEnd never decreases)", got)
	}
}

func TestStore_ConcurrentReadersWriter(t *testing.T) {
	s := NewStore(80)
	const N = 200
	var wg sync.WaitGroup

	// One writer filling in lines.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := int64(0); i < N; i++ {
			s.SetLine(i, []parser.Cell{{Rune: 'x'}})
		}
	}()

	// Many readers hammering.
	for r := 0; r < 8; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := int64(0); i < N; i++ {
				_ = s.Get(i, 0)
				_ = s.GetLine(i)
				_ = s.Max()
				_ = s.Width()
			}
		}()
	}

	wg.Wait()
}
