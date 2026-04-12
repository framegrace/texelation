package parser

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMainScreenState_JSONRoundtrip(t *testing.T) {
	s := MainScreenState{
		WriteTop:        100,
		ContentEnd:      150,
		CursorGlobalIdx: 145,
		CursorCol:       5,
		PromptStartLine: 140,
		WorkingDir:      "/home/user",
		SavedAt:         time.Unix(1700000000, 0).UTC(),
	}
	b, err := json.Marshal(&s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got MainScreenState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != s {
		t.Errorf("roundtrip mismatch:\n got %+v\nwant %+v", got, s)
	}
}
