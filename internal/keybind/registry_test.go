package keybind

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// makeKeyEvent creates a tcell.EventKey for testing.
func makeKeyEvent(key tcell.Key, r rune, mods tcell.ModMask) *tcell.EventKey {
	return tcell.NewEventKey(key, r, mods)
}

func TestNewRegistry_LinuxPreset(t *testing.T) {
	r := NewRegistry("linux", "", nil)
	keys := r.KeysForAction(Help)
	if len(keys) == 0 {
		t.Fatal("expected Help to have at least one binding in linux preset")
	}
	// Help should be bound to F1
	found := false
	for _, kc := range keys {
		if kc.Key == tcell.KeyF1 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Help bound to F1, got %v", keys)
	}
}

func TestRegistry_Match(t *testing.T) {
	r := NewRegistry("linux", "", nil)
	ev := makeKeyEvent(tcell.KeyF1, 0, 0)
	action := r.Match(ev)
	if action != Help {
		t.Errorf("expected Help, got %q", action)
	}
}

func TestRegistry_MatchUnbound(t *testing.T) {
	r := NewRegistry("linux", "", nil)
	ev := makeKeyEvent(tcell.KeyF11, 0, 0)
	action := r.Match(ev)
	if action != "" {
		t.Errorf("expected empty action for unbound key, got %q", action)
	}
}

func TestRegistry_MultipleBindings(t *testing.T) {
	overrides := map[string][]string{
		string(Screenshot): {"f5", "ctrl+p"},
	}
	r := NewRegistry("linux", "", overrides)

	evF5 := makeKeyEvent(tcell.KeyF5, 0, 0)
	if got := r.Match(evF5); got != Screenshot {
		t.Errorf("F5: expected Screenshot, got %q", got)
	}

	evCtrlP := makeKeyEvent(tcell.KeyCtrlP, 0, tcell.ModCtrl)
	if got := r.Match(evCtrlP); got != Screenshot {
		t.Errorf("Ctrl+P: expected Screenshot, got %q", got)
	}
}

func TestRegistry_OverrideReplacesPreset(t *testing.T) {
	overrides := map[string][]string{
		string(Help): {"f9"},
	}
	r := NewRegistry("linux", "", overrides)

	// F1 should no longer match Help
	evF1 := makeKeyEvent(tcell.KeyF1, 0, 0)
	if got := r.Match(evF1); got == Help {
		t.Errorf("F1 should not match Help after override, but got Help")
	}

	// F9 should match Help
	evF9 := makeKeyEvent(tcell.KeyF9, 0, 0)
	if got := r.Match(evF9); got != Help {
		t.Errorf("F9 should match Help, got %q", got)
	}
}

func TestRegistry_ExtraPresetOverlay(t *testing.T) {
	// linux base + mac overlay: pane nav should use mac bindings (alt+up etc.)
	r := NewRegistry("linux", "mac", nil)

	// Mac uses alt+up for PaneNavUp (linux uses shift+up)
	evAltUp := makeKeyEvent(tcell.KeyUp, 0, tcell.ModAlt)
	if got := r.Match(evAltUp); got != PaneNavUp {
		t.Errorf("alt+up should match PaneNavUp in mac overlay, got %q", got)
	}

	// shift+up should no longer match PaneNavUp (replaced by mac binding)
	evShiftUp := makeKeyEvent(tcell.KeyUp, 0, tcell.ModShift)
	if got := r.Match(evShiftUp); got == PaneNavUp {
		t.Errorf("shift+up should NOT match PaneNavUp after mac overlay, but got PaneNavUp")
	}
}

func TestRegistry_AutoDetect(t *testing.T) {
	// Should not panic; must return a valid registry with at least Help bound.
	r := NewRegistry("auto", "", nil)
	if r == nil {
		t.Fatal("NewRegistry with auto returned nil")
	}
	keys := r.KeysForAction(Help)
	if len(keys) == 0 {
		t.Error("auto preset: Help has no bindings")
	}
}

func TestRegistry_AllActions(t *testing.T) {
	r := NewRegistry("linux", "", nil)
	entries := r.AllActions()

	// Build a set of returned actions.
	returned := make(map[Action]bool, len(entries))
	for _, e := range entries {
		returned[e.Action] = true
		if e.Description == "" {
			t.Errorf("action %q has empty description", e.Action)
		}
		if e.Category == "" {
			t.Errorf("action %q has empty category", e.Action)
		}
	}

	// Every action in ActionDescriptions must appear.
	for action := range ActionDescriptions {
		if !returned[action] {
			t.Errorf("AllActions missing action %q", action)
		}
	}

	// Verify ordering: entries must be sorted by Category then Action.
	for i := 1; i < len(entries); i++ {
		prev, cur := entries[i-1], entries[i]
		if prev.Category > cur.Category {
			t.Errorf("AllActions not sorted by category: %q before %q", prev.Category, cur.Category)
			break
		}
		if prev.Category == cur.Category && string(prev.Action) > string(cur.Action) {
			t.Errorf("AllActions not sorted by action within category %q: %q before %q",
				prev.Category, prev.Action, cur.Action)
			break
		}
	}
}

func TestPresets_AllActionsPresent(t *testing.T) {
	for _, presetName := range []string{"linux", "mac"} {
		preset := presetByName(presetName)
		for action := range ActionDescriptions {
			if _, ok := preset[action]; !ok {
				t.Errorf("preset %q is missing action %q", presetName, action)
			}
		}
	}
}
