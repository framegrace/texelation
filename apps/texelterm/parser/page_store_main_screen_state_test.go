package parser

import (
	"encoding/json"
	"strings"
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

func TestMainScreenState_Validate(t *testing.T) {
	// Baseline valid state used as a starting point for each case.
	base := MainScreenState{
		WriteTop:        100,
		ContentEnd:      150,
		CursorGlobalIdx: 112,
		CursorCol:       5,
		PromptStartLine: 108,
	}

	tests := []struct {
		name    string
		mutate  func(s *MainScreenState)
		wantErr string // substring match; empty means expect nil
	}{
		{
			name:   "valid baseline",
			mutate: func(*MainScreenState) {},
		},
		{
			name:   "empty store sentinel",
			mutate: func(s *MainScreenState) { s.ContentEnd = -1 },
		},
		{
			name:   "unknown prompt sentinel",
			mutate: func(s *MainScreenState) { s.PromptStartLine = -1 },
		},
		{
			name:   "cursor exactly at WriteTop",
			mutate: func(s *MainScreenState) { s.CursorGlobalIdx = s.WriteTop },
		},
		{
			name:    "negative WriteTop",
			mutate:  func(s *MainScreenState) { s.WriteTop = -1 },
			wantErr: "WriteTop -1",
		},
		{
			name:    "ContentEnd below -1 sentinel",
			mutate:  func(s *MainScreenState) { s.ContentEnd = -2 },
			wantErr: "ContentEnd -2",
		},
		{
			name:    "negative CursorCol",
			mutate:  func(s *MainScreenState) { s.CursorCol = -1 },
			wantErr: "CursorCol -1",
		},
		{
			name:    "PromptStartLine below -1 sentinel",
			mutate:  func(s *MainScreenState) { s.PromptStartLine = -2 },
			wantErr: "PromptStartLine -2",
		},
		{
			name:    "cursor above WriteTop",
			mutate:  func(s *MainScreenState) { s.CursorGlobalIdx = s.WriteTop - 1 },
			wantErr: "CursorGlobalIdx 99 must be >= WriteTop 100",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.mutate(&s)
			err := s.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: unexpected error %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate: expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate: error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
