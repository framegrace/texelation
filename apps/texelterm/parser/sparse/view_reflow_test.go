package sparse

import (
	"strings"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func fillRow(s *Store, gi int64, text string, wrapped bool) {
	cells := make([]parser.Cell, len(text))
	for i, r := range text {
		cells[i] = parser.Cell{Rune: r}
	}
	if wrapped && len(cells) > 0 {
		cells[len(cells)-1].Wrapped = true
	}
	s.SetLine(gi, cells)
}

func TestChainWalk_SingleRowNoWrap(t *testing.T) {
	s := NewStore(10)
	fillRow(s, 0, "hello", false)
	end, nowrap := walkChain(s, 0, 4*24)
	if end != 0 {
		t.Errorf("single non-wrapped row: end=%d want 0", end)
	}
	if nowrap {
		t.Errorf("row without NoWrap flag: expected nowrap=false")
	}
}

func TestChainWalk_TwoRowChain(t *testing.T) {
	s := NewStore(10)
	fillRow(s, 5, "0123456789", true)
	fillRow(s, 6, "abc", false)
	end, _ := walkChain(s, 5, 4*24)
	if end != 6 {
		t.Errorf("chain end=%d want 6", end)
	}
}

func TestChainWalk_NoWrapPropagation(t *testing.T) {
	s := NewStore(10)
	fillRow(s, 5, "0123456789", true)
	fillRow(s, 6, "abc", false)
	s.SetRowNoWrap(6, true) // any NoWrap in chain → whole chain NoWrap
	_, nowrap := walkChain(s, 5, 4*24)
	if !nowrap {
		t.Errorf("any NoWrap in chain should propagate")
	}
}

func TestChainWalk_MalformedChainStopsAtGap(t *testing.T) {
	s := NewStore(10)
	fillRow(s, 5, "0123456789", true) // claims wrapped but row 6 missing
	end, _ := walkChain(s, 5, 4*24)
	if end != 5 {
		t.Errorf("malformed chain should stop at gap; end=%d want 5", end)
	}
}

func TestChainWalk_CapOnUnboundedChain(t *testing.T) {
	s := NewStore(10)
	for gi := int64(0); gi < 100; gi++ {
		fillRow(s, gi, "0123456789", true)
	}
	end, _ := walkChain(s, 0, 20)
	if end > 19 {
		t.Errorf("chain walk exceeded cap: end=%d (cap=20)", end)
	}
}

func TestReflowChain_SingleLogical(t *testing.T) {
	s := NewStore(10)
	fillRow(s, 0, "0123456789", true)
	fillRow(s, 1, "abcde", false)
	// chain at width 5 → 3 rows: "01234", "56789", "abcde"
	rows := reflowChain(s, 0, 1, 5)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if cellsToStringSparse(rows[0]) != "01234" {
		t.Errorf("row 0 = %q want %q", cellsToStringSparse(rows[0]), "01234")
	}
	if cellsToStringSparse(rows[2]) != "abcde" {
		t.Errorf("row 2 = %q want %q", cellsToStringSparse(rows[2]), "abcde")
	}
}

func TestClipRow_TruncatesAndPads(t *testing.T) {
	cells := []parser.Cell{{Rune: 'a'}, {Rune: 'b'}, {Rune: 'c'}}
	got := clipRow(cells, 5)
	if len(got) != 5 {
		t.Fatalf("clipRow should return length=width")
	}
	if got[0].Rune != 'a' || got[2].Rune != 'c' {
		t.Errorf("clipRow dropped content")
	}
	// Truncate:
	got2 := clipRow(cells, 2)
	if len(got2) != 2 || got2[1].Rune != 'b' {
		t.Errorf("clipRow should truncate")
	}
}

// Locks in that a NoWrap chain reports one physical row per stored row
// regardless of width — matches Render's nowrap branch (clipRow per row).
// Regression guard: if the short-circuit is replaced by width-based math the
// count would diverge from Render's output and cause cursor/anchor drift.
func TestChainReflowedRowCount_NoWrap(t *testing.T) {
	s := NewStore(200)
	fillRow(s, 10, "0123456789012345", false) // 16 chars
	fillRow(s, 11, "abcdefghij", false)       // 10 chars
	s.SetRowNoWrap(10, true)
	s.SetRowNoWrap(11, true)

	for _, width := range []int{1, 5, 8, 16, 80} {
		got := chainReflowedRowCount(s, 10, 11, width, true)
		if got != 2 {
			t.Errorf("width=%d: got %d rows, want 2 (one per stored row)", width, got)
		}
	}
}

// Wrapping branch must still grow with narrower widths.
func TestChainReflowedRowCount_WrapGrowsWithWidth(t *testing.T) {
	s := NewStore(200)
	fillRow(s, 0, "0123456789", true)
	fillRow(s, 1, "abcde", false)
	// 15 total cells; widths sampled against ceil(15/width).
	for width, want := range map[int]int{5: 3, 8: 2, 15: 1, 20: 1} {
		got := chainReflowedRowCount(s, 0, 1, width, false)
		if got != want {
			t.Errorf("width=%d: got %d rows, want %d", width, got, want)
		}
	}
}

// Test helper
func cellsToStringSparse(cells []parser.Cell) string {
	b := strings.Builder{}
	for _, c := range cells {
		if c.Rune == 0 {
			b.WriteByte(' ')
		} else {
			b.WriteRune(c.Rune)
		}
	}
	return b.String()
}
