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
