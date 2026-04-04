package keybind

import (
	"log"
	"sort"

	"github.com/gdamore/tcell/v2"
)

// Registry maps key combos to actions and provides reverse lookup.
type Registry struct {
	keyToAction  map[KeyCombo]Action
	actionToKeys map[Action][]KeyCombo
}

// NewRegistry builds a Registry by merging presets and overrides.
//
// Merge order:
//  1. Load base preset by name ("linux", "mac", "auto").
//  2. If extraPreset != "", overlay it (per-action replacement).
//  3. Apply overrides map (per-action replacement).
//  4. Parse all key strings, build lookup maps.
//
// When multiple layers assign the same key combo to different actions, the
// higher-priority layer wins (overrides > extra preset > base preset).
// Invalid key strings are logged and skipped.
func NewRegistry(preset string, extraPreset string, overrides map[string][]string) *Registry {
	// merged holds the final per-action key string lists.
	merged := make(map[Action][]string, len(ActionDescriptions))

	// claimedBy tracks which action currently "owns" each parsed KeyCombo.
	// We process layers from lowest to highest priority; each higher-priority
	// assignment displaces the previous owner.
	// We store ordered layers so we can re-process in priority order.
	type sourceLayer struct {
		name    string
		entries map[Action][]string
	}

	sources := []sourceLayer{
		{"base", presetByName(preset)},
	}
	if extraPreset != "" {
		sources = append(sources, sourceLayer{"extra", presetByName(extraPreset)})
	}
	if len(overrides) > 0 {
		overrideMap := make(map[Action][]string, len(overrides))
		for k, v := range overrides {
			overrideMap[Action(k)] = v
		}
		sources = append(sources, sourceLayer{"override", overrideMap})
	}

	// Build merged map: last write wins (highest priority layer last).
	for _, src := range sources {
		for action, keys := range src.entries {
			merged[action] = keys
		}
	}

	// Build keyToAction layer by layer, highest priority last.
	// For each action in a higher-priority layer:
	//   1. Remove all combos previously assigned to that action (its old layer's keys).
	//   2. Assign the new combos, displacing any other action that claimed them.
	keyToAction := make(map[KeyCombo]Action)
	// actionCombos tracks the live set of combos currently owned by each action.
	actionCombos := make(map[Action]map[KeyCombo]struct{}, len(merged))
	for action := range merged {
		actionCombos[action] = make(map[KeyCombo]struct{})
	}

	// prevLayerKeys tracks the key strings each action had after the previous layer.
	// We use this to detect which actions actually *changed* in the current layer.
	prevLayerKeys := make(map[Action][]string)

	for _, src := range sources {
		// Split this layer's actions into two groups:
		//   changed  — actions whose key list differs from the previous layer
		//   unchanged — actions whose key list is identical to the previous layer
		// We process unchanged first, then changed.  This ensures that when two
		// actions claim the same combo within a single layer (e.g. mac overlay
		// where PaneNavUp changed to "alt+up" but TermScrollUp still lists
		// "alt+up" unchanged), the changed action wins.
		type actionKeys struct {
			action  Action
			keyStrs []string
		}
		var changed, unchanged []actionKeys
		for action, keyStrs := range src.entries {
			prev := prevLayerKeys[action]
			if keySlicesEqual(prev, keyStrs) {
				unchanged = append(unchanged, actionKeys{action, keyStrs})
			} else {
				changed = append(changed, actionKeys{action, keyStrs})
			}
		}

		applyAction := func(action Action, keyStrs []string) {
			var newCombos []KeyCombo
			for _, s := range keyStrs {
				kc, err := ParseKeyCombo(s)
				if err != nil {
					log.Printf("keybind: skipping invalid key %q for action %q: %v", s, action, err)
					continue
				}
				newCombos = append(newCombos, kc)
			}
			// Revoke all combos this action previously owned.
			for kc := range actionCombos[action] {
				delete(keyToAction, kc)
			}
			actionCombos[action] = make(map[KeyCombo]struct{}, len(newCombos))
			// Assign new combos, displacing any other action that held them.
			for _, kc := range newCombos {
				if prev, exists := keyToAction[kc]; exists && prev != action {
					delete(actionCombos[prev], kc)
				}
				keyToAction[kc] = action
				actionCombos[action][kc] = struct{}{}
			}
		}

		for _, ak := range unchanged {
			applyAction(ak.action, ak.keyStrs)
		}
		for _, ak := range changed {
			applyAction(ak.action, ak.keyStrs)
		}

		// Record the current layer's key strings as the new "previous" state.
		for action, keyStrs := range src.entries {
			prevLayerKeys[action] = keyStrs
		}
	}

	// Build actionToKeys from the final actionCombos state.
	actionToKeys := make(map[Action][]KeyCombo, len(actionCombos))
	for action, comboSet := range actionCombos {
		combos := make([]KeyCombo, 0, len(comboSet))
		for kc := range comboSet {
			combos = append(combos, kc)
		}
		actionToKeys[action] = combos
	}

	return &Registry{
		keyToAction:  keyToAction,
		actionToKeys: actionToKeys,
	}
}

// keySlicesEqual returns true if two key string slices are identical.
func keySlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Match returns the action bound to the given key event, or "" if none.
func (r *Registry) Match(ev *tcell.EventKey) Action {
	for kc, action := range r.keyToAction {
		if kc.MatchesEvent(ev) {
			return action
		}
	}
	return ""
}

// KeysForAction returns the key combos bound to the given action.
func (r *Registry) KeysForAction(a Action) []KeyCombo {
	return r.actionToKeys[a]
}

// AllActions returns all known actions sorted by Category then Action,
// with descriptions from ActionDescriptions and current key bindings.
func (r *Registry) AllActions() []ActionEntry {
	entries := make([]ActionEntry, 0, len(ActionDescriptions))
	for action, info := range ActionDescriptions {
		entries = append(entries, ActionEntry{
			Action:      action,
			Description: info.Description,
			Category:    info.Category,
			Keys:        r.actionToKeys[action],
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Category != entries[j].Category {
			return entries[i].Category < entries[j].Category
		}
		return string(entries[i].Action) < string(entries[j].Action)
	})
	return entries
}
