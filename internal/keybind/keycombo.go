package keybind

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/gdamore/tcell/v2"
)

// KeyCombo represents a single key combination including modifiers.
type KeyCombo struct {
	Key       tcell.Key
	Rune      rune
	Modifiers tcell.ModMask
}

// MatchesEvent returns true if this combo matches the given key event.
func (kc KeyCombo) MatchesEvent(ev *tcell.EventKey) bool {
	// For Ctrl+letter keys, modifiers are implicit in the key constant itself.
	if kc.Key >= tcell.KeyCtrlA && kc.Key <= tcell.KeyCtrlZ {
		return ev.Key() == kc.Key
	}
	if kc.Key == tcell.KeyRune {
		return ev.Key() == tcell.KeyRune && ev.Rune() == kc.Rune && ev.Modifiers() == kc.Modifiers
	}
	return ev.Key() == kc.Key && ev.Modifiers() == kc.Modifiers
}

// keyNames maps lowercase key names to tcell key constants.
var keyNames = map[string]tcell.Key{
	"up":        tcell.KeyUp,
	"down":      tcell.KeyDown,
	"left":      tcell.KeyLeft,
	"right":     tcell.KeyRight,
	"enter":     tcell.KeyEnter,
	"esc":       tcell.KeyEscape,
	"escape":    tcell.KeyEscape,
	"tab":       tcell.KeyTab,
	"backspace": tcell.KeyBackspace2,
	"delete":    tcell.KeyDelete,
	"insert":    tcell.KeyInsert,
	"home":      tcell.KeyHome,
	"end":       tcell.KeyEnd,
	"pgup":      tcell.KeyPgUp,
	"pgdn":      tcell.KeyPgDn,
	"f1":        tcell.KeyF1,
	"f2":        tcell.KeyF2,
	"f3":        tcell.KeyF3,
	"f4":        tcell.KeyF4,
	"f5":        tcell.KeyF5,
	"f6":        tcell.KeyF6,
	"f7":        tcell.KeyF7,
	"f8":        tcell.KeyF8,
	"f9":        tcell.KeyF9,
	"f10":       tcell.KeyF10,
	"f11":       tcell.KeyF11,
	"f12":       tcell.KeyF12,
}

// ctrlKeys maps single letters to their tcell Ctrl key constant.
var ctrlKeys = map[string]tcell.Key{
	"a": tcell.KeyCtrlA,
	"b": tcell.KeyCtrlB,
	"c": tcell.KeyCtrlC,
	"d": tcell.KeyCtrlD,
	"e": tcell.KeyCtrlE,
	"f": tcell.KeyCtrlF,
	"g": tcell.KeyCtrlG,
	"h": tcell.KeyCtrlH,
	"i": tcell.KeyCtrlI,
	"j": tcell.KeyCtrlJ,
	"k": tcell.KeyCtrlK,
	"l": tcell.KeyCtrlL,
	"m": tcell.KeyCtrlM,
	"n": tcell.KeyCtrlN,
	"o": tcell.KeyCtrlO,
	"p": tcell.KeyCtrlP,
	"q": tcell.KeyCtrlQ,
	"r": tcell.KeyCtrlR,
	"s": tcell.KeyCtrlS,
	"t": tcell.KeyCtrlT,
	"u": tcell.KeyCtrlU,
	"v": tcell.KeyCtrlV,
	"w": tcell.KeyCtrlW,
	"x": tcell.KeyCtrlX,
	"y": tcell.KeyCtrlY,
	"z": tcell.KeyCtrlZ,
}

// reverse maps built in init for formatting.
var (
	reverseKeyNames map[tcell.Key]string
	reverseCtrlKeys map[tcell.Key]string
)

func init() {
	reverseKeyNames = make(map[tcell.Key]string, len(keyNames))
	for name, key := range keyNames {
		// Prefer shorter canonical names (esc over escape, pgup over pgdn duplicates, etc.)
		if existing, ok := reverseKeyNames[key]; ok {
			if len(name) < len(existing) {
				reverseKeyNames[key] = name
			}
		} else {
			reverseKeyNames[key] = name
		}
	}

	reverseCtrlKeys = make(map[tcell.Key]string, len(ctrlKeys))
	for letter, key := range ctrlKeys {
		reverseCtrlKeys[key] = letter
	}
}

// ParseKeyCombo parses a string like "ctrl+a", "shift+up", "f1", "a", "alt+b", "space".
func ParseKeyCombo(s string) (KeyCombo, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return KeyCombo{}, fmt.Errorf("keybind: empty key combo string")
	}

	parts := strings.Split(s, "+")

	// Collect modifier parts (all but last) and the key part (last).
	keyPart := parts[len(parts)-1]
	modParts := parts[:len(parts)-1]

	if keyPart == "" {
		return KeyCombo{}, fmt.Errorf("keybind: missing key in %q", s)
	}

	var mods tcell.ModMask
	hasCtrl := false
	for _, mod := range modParts {
		switch strings.ToLower(mod) {
		case "ctrl":
			mods |= tcell.ModCtrl
			hasCtrl = true
		case "shift":
			mods |= tcell.ModShift
		case "alt":
			mods |= tcell.ModAlt
		default:
			return KeyCombo{}, fmt.Errorf("keybind: unknown modifier %q in %q", mod, s)
		}
	}

	lowerKey := strings.ToLower(keyPart)

	// Special: "space" → rune ' '
	if lowerKey == "space" {
		return KeyCombo{Key: tcell.KeyRune, Rune: ' ', Modifiers: mods}, nil
	}

	// ctrl+<single-letter> with no other special modifiers → use KeyCtrlX
	if hasCtrl && mods == tcell.ModCtrl && len(keyPart) == 1 {
		if ck, ok := ctrlKeys[lowerKey]; ok {
			return KeyCombo{Key: ck, Modifiers: mods}, nil
		}
	}

	// Named keys (up, down, f1, enter, etc.) — case-insensitive lookup
	if key, ok := keyNames[lowerKey]; ok {
		return KeyCombo{Key: key, Modifiers: mods}, nil
	}

	// Single character → KeyRune (preserves case: 't' ≠ 'T')
	runes := []rune(keyPart)
	if len(runes) == 1 && unicode.IsPrint(runes[0]) {
		return KeyCombo{Key: tcell.KeyRune, Rune: runes[0], Modifiers: mods}, nil
	}

	return KeyCombo{}, fmt.Errorf("keybind: unknown key %q in %q", keyPart, s)
}

// FormatKeyCombo produces a human-readable representation like "Ctrl+A", "Shift+Up", "F1".
func FormatKeyCombo(kc KeyCombo) string {
	var parts []string

	if kc.Modifiers&tcell.ModCtrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if kc.Modifiers&tcell.ModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if kc.Modifiers&tcell.ModShift != 0 {
		parts = append(parts, "Shift")
	}

	var keyStr string

	// Ctrl+letter keys: use the letter name.
	if letter, ok := reverseCtrlKeys[kc.Key]; ok {
		keyStr = strings.ToUpper(letter)
		return strings.Join(append(parts, keyStr), "+")
	}

	// Rune keys.
	if kc.Key == tcell.KeyRune {
		if kc.Rune == ' ' {
			keyStr = "Space"
		} else {
			keyStr = strings.ToUpper(string(kc.Rune))
		}
		return strings.Join(append(parts, keyStr), "+")
	}

	// Named keys.
	if name, ok := reverseKeyNames[kc.Key]; ok {
		// Title-case: "up" → "Up", "f1" → "F1", "pgup" → "Pgup"
		keyStr = strings.ToUpper(name[:1]) + name[1:]
		return strings.Join(append(parts, keyStr), "+")
	}

	// Fallback: use numeric key value.
	keyStr = fmt.Sprintf("Key(%d)", kc.Key)
	return strings.Join(append(parts, keyStr), "+")
}
