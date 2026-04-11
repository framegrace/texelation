package sparse

import (
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
