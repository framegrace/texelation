package keybind

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestParseKeyCombo(t *testing.T) {
	cases := []struct {
		input    string
		wantKey  tcell.Key
		wantRune rune
		wantMods tcell.ModMask
	}{
		// ctrl+letter → tcell KeyCtrlX constants
		{"ctrl+a", tcell.KeyCtrlA, 0, tcell.ModCtrl},
		{"ctrl+z", tcell.KeyCtrlZ, 0, tcell.ModCtrl},
		{"ctrl+c", tcell.KeyCtrlC, 0, tcell.ModCtrl},
		// shift+arrow
		{"shift+up", tcell.KeyUp, 0, tcell.ModShift},
		{"shift+down", tcell.KeyDown, 0, tcell.ModShift},
		{"shift+left", tcell.KeyLeft, 0, tcell.ModShift},
		{"shift+right", tcell.KeyRight, 0, tcell.ModShift},
		// alt+arrow
		{"alt+left", tcell.KeyLeft, 0, tcell.ModAlt},
		{"alt+right", tcell.KeyRight, 0, tcell.ModAlt},
		// function keys
		{"f1", tcell.KeyF1, 0, tcell.ModNone},
		{"f12", tcell.KeyF12, 0, tcell.ModNone},
		// plain letter → KeyRune
		{"a", tcell.KeyRune, 'a', tcell.ModNone},
		// alt+letter → KeyRune with ModAlt
		{"alt+b", tcell.KeyRune, 'b', tcell.ModAlt},
		// space
		{"space", tcell.KeyRune, ' ', tcell.ModNone},
		// pgup/pgdn
		{"pgup", tcell.KeyPgUp, 0, tcell.ModNone},
		{"pgdn", tcell.KeyPgDn, 0, tcell.ModNone},
		// other named keys
		{"backspace", tcell.KeyBackspace2, 0, tcell.ModNone},
		{"delete", tcell.KeyDelete, 0, tcell.ModNone},
		{"enter", tcell.KeyEnter, 0, tcell.ModNone},
		{"esc", tcell.KeyEscape, 0, tcell.ModNone},
		{"tab", tcell.KeyTab, 0, tcell.ModNone},
		// ctrl+shift+letter → KeyRune with ModCtrl|ModShift
		{"ctrl+shift+f", tcell.KeyRune, 'f', tcell.ModCtrl | tcell.ModShift},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			kc, err := ParseKeyCombo(tc.input)
			if err != nil {
				t.Fatalf("ParseKeyCombo(%q) unexpected error: %v", tc.input, err)
			}
			if kc.Key != tc.wantKey {
				t.Errorf("Key: got %v, want %v", kc.Key, tc.wantKey)
			}
			if kc.Rune != tc.wantRune {
				t.Errorf("Rune: got %q, want %q", kc.Rune, tc.wantRune)
			}
			if kc.Modifiers != tc.wantMods {
				t.Errorf("Modifiers: got %v, want %v", kc.Modifiers, tc.wantMods)
			}
		})
	}
}

func TestParseKeyCombo_Invalid(t *testing.T) {
	cases := []string{
		"",
		"ctrl+",
		"foo+bar",
		"ctrl+alt+",
	}

	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			_, err := ParseKeyCombo(tc)
			if err == nil {
				t.Errorf("ParseKeyCombo(%q) expected error, got nil", tc)
			}
		})
	}
}

func TestFormatKeyCombo(t *testing.T) {
	cases := []struct {
		kc   KeyCombo
		want string
	}{
		{
			KeyCombo{Key: tcell.KeyCtrlA, Modifiers: tcell.ModCtrl},
			"Ctrl+A",
		},
		{
			KeyCombo{Key: tcell.KeyUp, Modifiers: tcell.ModShift},
			"Shift+Up",
		},
		{
			KeyCombo{Key: tcell.KeyF1, Modifiers: tcell.ModNone},
			"F1",
		},
		{
			KeyCombo{Key: tcell.KeyRune, Rune: 'b', Modifiers: tcell.ModAlt},
			"Alt+B",
		},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			got := FormatKeyCombo(tc.kc)
			if got != tc.want {
				t.Errorf("FormatKeyCombo: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMatchesEvent(t *testing.T) {
	cases := []struct {
		name  string
		combo KeyCombo
		event *tcell.EventKey
		want  bool
	}{
		{
			name:  "ctrl+a matches KeyCtrlA event",
			combo: KeyCombo{Key: tcell.KeyCtrlA, Modifiers: tcell.ModCtrl},
			event: tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl),
			want:  true,
		},
		{
			name:  "ctrl+a does not match KeyCtrlB",
			combo: KeyCombo{Key: tcell.KeyCtrlA, Modifiers: tcell.ModCtrl},
			event: tcell.NewEventKey(tcell.KeyCtrlB, 0, tcell.ModCtrl),
			want:  false,
		},
		{
			name:  "shift+up matches",
			combo: KeyCombo{Key: tcell.KeyUp, Modifiers: tcell.ModShift},
			event: tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModShift),
			want:  true,
		},
		{
			name:  "shift+up does not match plain up",
			combo: KeyCombo{Key: tcell.KeyUp, Modifiers: tcell.ModShift},
			event: tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone),
			want:  false,
		},
		{
			name:  "plain rune 'a' matches",
			combo: KeyCombo{Key: tcell.KeyRune, Rune: 'a', Modifiers: tcell.ModNone},
			event: tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone),
			want:  true,
		},
		{
			name:  "plain rune 'a' does not match 'b'",
			combo: KeyCombo{Key: tcell.KeyRune, Rune: 'a', Modifiers: tcell.ModNone},
			event: tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone),
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.combo.MatchesEvent(tc.event)
			if got != tc.want {
				t.Errorf("MatchesEvent: got %v, want %v", got, tc.want)
			}
		})
	}
}
