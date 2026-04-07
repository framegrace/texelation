# List Editor Widget Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a generic `ListEditor` widget in texelui for editing arrays of objects, and wire it into texelation's config editor to replace the raw JSON textarea for the transformer pipeline.

**Architecture:** A `ListEditor` primitive in `texelui/primitives` renders a vertical list of collapsible items. Each item has a label, toggle checkbox, reorder buttons, remove button, and expandable detail fields. The config editor detects arrays of maps with an ID key and uses this widget instead of a textarea.

**Tech Stack:** Go, tcell, texelui widget system (BaseWidget, Form, Checkbox, Input, ScrollPane)

---

## File Structure

| File | Responsibility |
|------|---------------|
| Create: `texelui/primitives/listeditor.go` | ListEditor widget: data model, rendering, key/mouse handling |
| Create: `texelui/primitives/listeditor_test.go` | Unit tests |
| Modify: `texelation/apps/configeditor/field_builder.go` | Detect list-of-maps, create ListEditor instead of textarea |

---

### Task 1: ListEditor data model and core API

**Files:**
- Create: `texelui/primitives/listeditor.go` (in /home/marc/projects/texel/texelui/)

- [ ] **Step 1: Create the types and constructor**

```go
// texelui/primitives/listeditor.go
package primitives

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
	"github.com/framegrace/texelui/widgets"
)

// ListEditorConfig configures how the ListEditor displays items.
type ListEditorConfig struct {
	LabelKey  string // map key used as the row label (e.g., "id")
	ToggleKey string // map key for the checkbox (e.g., "enabled"); "" = no checkbox
}

// ListEditor is a widget for editing arrays of objects.
// Each item shows a label, optional toggle, expandable detail fields,
// reorder buttons (▲/▼), and a remove button (✕).
type ListEditor struct {
	core.BaseWidget
	config      ListEditorConfig
	items       []map[string]interface{}
	selectedIdx int  // focused item index
	expandedIdx int  // expanded item index (-1 = none)
	OnChange    func([]map[string]interface{})
	inv         func(core.Rect)
}

// NewListEditor creates a list editor with the given configuration.
func NewListEditor(cfg ListEditorConfig) *ListEditor {
	le := &ListEditor{
		config:      cfg,
		expandedIdx: -1,
	}
	le.SetFocusable(true)
	return le
}

// SetItems replaces the item list.
func (le *ListEditor) SetItems(items []map[string]interface{}) {
	le.items = items
	le.selectedIdx = 0
	le.expandedIdx = -1
	le.invalidate()
}

// Items returns the current item list.
func (le *ListEditor) Items() []map[string]interface{} {
	return le.items
}

func (le *ListEditor) invalidate() {
	if le.inv != nil {
		le.inv(le.Rect)
	}
}

func (le *ListEditor) fireChange() {
	if le.OnChange != nil {
		le.OnChange(le.items)
	}
	le.invalidate()
}

// itemLabel returns the display label for an item.
func (le *ListEditor) itemLabel(item map[string]interface{}) string {
	if le.config.LabelKey != "" {
		if v, ok := item[le.config.LabelKey]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return "(unnamed)"
}

// itemToggle returns the toggle state for an item.
func (le *ListEditor) itemToggle(item map[string]interface{}) bool {
	if le.config.ToggleKey == "" {
		return true
	}
	if v, ok := item[le.config.ToggleKey].(bool); ok {
		return v
	}
	return false
}

// detailKeys returns the map keys to show in the detail area (excludes label and toggle keys).
func (le *ListEditor) detailKeys(item map[string]interface{}) []string {
	var keys []string
	for k := range item {
		if k == le.config.LabelKey || k == le.config.ToggleKey {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// itemHeight returns the rendered height of an item.
func (le *ListEditor) itemHeight(idx int) int {
	if idx == le.expandedIdx {
		return 1 + len(le.detailKeys(le.items[idx]))
	}
	return 1
}

// totalHeight returns the total rendered height of all items plus the add button.
func (le *ListEditor) totalHeight() int {
	h := 0
	for i := range le.items {
		h += le.itemHeight(i)
	}
	h++ // add button row
	return h
}

// --- Mutations ---

func (le *ListEditor) toggleItem(idx int) {
	if le.config.ToggleKey == "" || idx < 0 || idx >= len(le.items) {
		return
	}
	le.items[idx][le.config.ToggleKey] = !le.itemToggle(le.items[idx])
	le.fireChange()
}

func (le *ListEditor) moveUp(idx int) {
	if idx <= 0 || idx >= len(le.items) {
		return
	}
	le.items[idx], le.items[idx-1] = le.items[idx-1], le.items[idx]
	le.selectedIdx = idx - 1
	if le.expandedIdx == idx {
		le.expandedIdx = idx - 1
	} else if le.expandedIdx == idx-1 {
		le.expandedIdx = idx
	}
	le.fireChange()
}

func (le *ListEditor) moveDown(idx int) {
	if idx < 0 || idx >= len(le.items)-1 {
		return
	}
	le.items[idx], le.items[idx+1] = le.items[idx+1], le.items[idx]
	le.selectedIdx = idx + 1
	if le.expandedIdx == idx {
		le.expandedIdx = idx + 1
	} else if le.expandedIdx == idx+1 {
		le.expandedIdx = idx
	}
	le.fireChange()
}

func (le *ListEditor) removeItem(idx int) {
	if idx < 0 || idx >= len(le.items) {
		return
	}
	le.items = append(le.items[:idx], le.items[idx+1:]...)
	if le.expandedIdx == idx {
		le.expandedIdx = -1
	} else if le.expandedIdx > idx {
		le.expandedIdx--
	}
	if le.selectedIdx >= len(le.items) {
		le.selectedIdx = len(le.items) - 1
	}
	if le.selectedIdx < 0 {
		le.selectedIdx = 0
	}
	le.fireChange()
}

func (le *ListEditor) addItem() {
	item := make(map[string]interface{})
	if le.config.LabelKey != "" {
		item[le.config.LabelKey] = ""
	}
	if le.config.ToggleKey != "" {
		item[le.config.ToggleKey] = true
	}
	le.items = append(le.items, item)
	le.selectedIdx = len(le.items) - 1
	le.expandedIdx = le.selectedIdx
	le.fireChange()
}

func (le *ListEditor) toggleExpand(idx int) {
	if le.expandedIdx == idx {
		le.expandedIdx = -1
	} else {
		le.expandedIdx = idx
	}
	le.invalidate()
}

func (le *ListEditor) setDetailValue(itemIdx int, key string, value interface{}) {
	if itemIdx < 0 || itemIdx >= len(le.items) {
		return
	}
	le.items[itemIdx][key] = value
	le.fireChange()
}
```

- [ ] **Step 2: Build**

```bash
cd /home/marc/projects/texel/texelui
go build ./primitives/
```

- [ ] **Step 3: Commit**

```bash
git add primitives/listeditor.go
git commit -m "Add ListEditor data model and core API"
```

---

### Task 2: ListEditor rendering

**Files:**
- Modify: `texelui/primitives/listeditor.go`

- [ ] **Step 1: Add the Draw method**

Add to `listeditor.go`:

```go
// Draw renders the list editor.
func (le *ListEditor) Draw(p *core.Painter) {
	if le.Rect.W <= 0 || le.Rect.H <= 0 {
		return
	}

	tm := theme.Get()
	baseFG := tm.GetSemanticColor("text.primary")
	mutedFG := tm.GetSemanticColor("text.muted")
	accentFG := tm.GetSemanticColor("accent.primary")
	bgColor := tm.GetSemanticColor("bg.surface")
	selectedBG := tm.GetSemanticColor("bg.elevated")

	baseStyle := tcell.StyleDefault.Foreground(baseFG).Background(bgColor)
	mutedStyle := tcell.StyleDefault.Foreground(mutedFG).Background(bgColor)
	accentStyle := tcell.StyleDefault.Foreground(accentFG).Background(bgColor)
	selectedStyle := tcell.StyleDefault.Foreground(baseFG).Background(selectedBG)

	x, y := le.Rect.X, le.Rect.Y
	w := le.Rect.W
	maxY := le.Rect.Y + le.Rect.H

	for i, item := range le.items {
		if y >= maxY {
			break
		}

		// Choose style based on selection
		rowStyle := baseStyle
		if i == le.selectedIdx && le.IsFocused() {
			rowStyle = selectedStyle
		}

		// Clear row
		for col := x; col < x+w; col++ {
			p.SetCell(col, y, ' ', rowStyle)
		}

		col := x

		// Checkbox
		if le.config.ToggleKey != "" {
			checkChar := '☐'
			if le.itemToggle(item) {
				checkChar = '☑'
			}
			checkStyle := accentStyle
			if i == le.selectedIdx && le.IsFocused() {
				checkStyle = checkStyle.Background(selectedBG)
			}
			p.SetCell(col, y, checkChar, checkStyle)
			col += 2
		}

		// Label
		label := le.itemLabel(item)
		for _, ch := range label {
			if col >= x+w-10 { // leave room for buttons
				break
			}
			p.SetCell(col, y, ch, rowStyle)
			col++
		}

		// Right side: ▲ ▼ ✕ ▸/▾
		btnStyle := mutedStyle
		if i == le.selectedIdx && le.IsFocused() {
			btnStyle = btnStyle.Background(selectedBG)
		}
		rightX := x + w - 1

		// Expand/collapse arrow
		expandChar := '▸'
		if i == le.expandedIdx {
			expandChar = '▾'
		}
		p.SetCell(rightX, y, expandChar, btnStyle)
		rightX -= 2

		// Remove
		p.SetCell(rightX, y, '✕', btnStyle)
		rightX -= 2

		// Down
		if i < len(le.items)-1 {
			p.SetCell(rightX, y, '▼', btnStyle)
		} else {
			p.SetCell(rightX, y, ' ', btnStyle)
		}
		rightX -= 2

		// Up
		if i > 0 {
			p.SetCell(rightX, y, '▲', btnStyle)
		} else {
			p.SetCell(rightX, y, ' ', btnStyle)
		}

		y++

		// Expanded detail fields
		if i == le.expandedIdx && y < maxY {
			detailKeys := le.detailKeys(item)
			for _, key := range detailKeys {
				if y >= maxY {
					break
				}
				// Clear row
				for col := x; col < x+w; col++ {
					p.SetCell(col, y, ' ', baseStyle)
				}

				// Indent + key: value
				col := x + 2
				keyStr := key + ": "
				for _, ch := range keyStr {
					if col < x+w {
						p.SetCell(col, y, ch, mutedStyle)
						col++
					}
				}
				valStr := fmt.Sprintf("%v", item[key])
				for _, ch := range valStr {
					if col < x+w {
						p.SetCell(col, y, ch, baseStyle)
						col++
					}
				}
				y++
			}
		}
	}

	// Add button row
	if y < maxY {
		for col := x; col < x+w; col++ {
			p.SetCell(col, y, ' ', baseStyle)
		}
		addStr := "[+ Add]"
		addStyle := accentStyle
		col := x + 1
		for _, ch := range addStr {
			if col < x+w {
				p.SetCell(col, y, ch, addStyle)
				col++
			}
		}
	}
}
```

- [ ] **Step 2: Build**

```bash
go build ./primitives/
```

- [ ] **Step 3: Commit**

```bash
git add primitives/listeditor.go
git commit -m "Add ListEditor rendering with collapsed/expanded rows"
```

---

### Task 3: ListEditor keyboard handling

**Files:**
- Modify: `texelui/primitives/listeditor.go`

- [ ] **Step 1: Add HandleKey**

```go
// HandleKey handles keyboard input for the list editor.
func (le *ListEditor) HandleKey(ev *tcell.EventKey) bool {
	if len(le.items) == 0 {
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyRune && ev.Rune() == '+' {
			le.addItem()
			return true
		}
		return false
	}

	switch ev.Key() {
	case tcell.KeyUp:
		if le.selectedIdx > 0 {
			le.selectedIdx--
			le.invalidate()
		}
		return true
	case tcell.KeyDown:
		if le.selectedIdx < len(le.items)-1 {
			le.selectedIdx++
			le.invalidate()
		} else if le.selectedIdx == len(le.items)-1 {
			// Move to "Add" button conceptually (selectedIdx stays, but Enter will add)
		}
		return true
	case tcell.KeyEnter:
		if le.selectedIdx >= 0 && le.selectedIdx < len(le.items) {
			le.toggleExpand(le.selectedIdx)
		}
		return true
	case tcell.KeyRune:
		switch ev.Rune() {
		case ' ':
			le.toggleItem(le.selectedIdx)
			return true
		case '+':
			le.addItem()
			return true
		case '-':
			le.removeItem(le.selectedIdx)
			return true
		}
	case tcell.KeyDelete:
		le.removeItem(le.selectedIdx)
		return true
	}

	// Ctrl+Up/Down for reorder
	if ev.Modifiers()&tcell.ModCtrl != 0 {
		switch ev.Key() {
		case tcell.KeyUp:
			le.moveUp(le.selectedIdx)
			return true
		case tcell.KeyDown:
			le.moveDown(le.selectedIdx)
			return true
		}
	}

	return false
}
```

- [ ] **Step 2: Build**

```bash
go build ./primitives/
```

- [ ] **Step 3: Commit**

```bash
git add primitives/listeditor.go
git commit -m "Add ListEditor keyboard handling"
```

---

### Task 4: ListEditor mouse handling

**Files:**
- Modify: `texelui/primitives/listeditor.go`

- [ ] **Step 1: Add HandleMouse**

```go
// HandleMouse handles mouse clicks on the list editor.
func (le *ListEditor) HandleMouse(ev *tcell.EventMouse) bool {
	mx, my := ev.Position()
	if !le.Rect.Contains(mx, my) {
		return false
	}

	buttons := ev.Buttons()
	if buttons&tcell.Button1 == 0 {
		return false
	}

	// Map y position to item index
	y := le.Rect.Y
	for i := range le.items {
		itemH := le.itemHeight(i)
		if my >= y && my < y+itemH {
			le.selectedIdx = i

			// Which column was clicked?
			relX := mx - le.Rect.X
			rightEdge := le.Rect.W - 1

			if my == y { // Header row
				// Expand/collapse arrow (rightmost)
				if relX >= rightEdge-1 {
					le.toggleExpand(i)
					return true
				}
				// Remove (2nd from right)
				if relX >= rightEdge-3 && relX <= rightEdge-2 {
					le.removeItem(i)
					return true
				}
				// Down arrow (3rd from right)
				if relX >= rightEdge-5 && relX <= rightEdge-4 {
					le.moveDown(i)
					return true
				}
				// Up arrow (4th from right)
				if relX >= rightEdge-7 && relX <= rightEdge-6 {
					le.moveUp(i)
					return true
				}
				// Checkbox (left)
				if relX < 2 && le.config.ToggleKey != "" {
					le.toggleItem(i)
					return true
				}
			}

			le.invalidate()
			return true
		}
		y += itemH
	}

	// Add button row
	if my == y {
		le.addItem()
		return true
	}

	return false
}
```

- [ ] **Step 2: Build**

```bash
go build ./primitives/
```

- [ ] **Step 3: Commit**

```bash
git add primitives/listeditor.go
git commit -m "Add ListEditor mouse handling"
```

---

### Task 5: ListEditor unit tests

**Files:**
- Create: `texelui/primitives/listeditor_test.go`

- [ ] **Step 1: Write tests**

```go
package primitives

import "testing"

func TestListEditor_AddRemove(t *testing.T) {
	le := NewListEditor(ListEditorConfig{LabelKey: "id", ToggleKey: "enabled"})
	le.SetItems([]map[string]interface{}{
		{"id": "first", "enabled": true},
		{"id": "second", "enabled": false},
	})

	if len(le.Items()) != 2 {
		t.Fatalf("expected 2 items, got %d", len(le.Items()))
	}

	le.addItem()
	if len(le.Items()) != 3 {
		t.Fatalf("expected 3 items after add, got %d", len(le.Items()))
	}

	le.removeItem(1)
	if len(le.Items()) != 2 {
		t.Fatalf("expected 2 items after remove, got %d", len(le.Items()))
	}
	if le.itemLabel(le.Items()[0]) != "first" {
		t.Errorf("expected first item 'first', got %q", le.itemLabel(le.Items()[0]))
	}
	if le.itemLabel(le.Items()[1]) != "" {
		t.Errorf("expected second item '' (added), got %q", le.itemLabel(le.Items()[1]))
	}
}

func TestListEditor_Reorder(t *testing.T) {
	le := NewListEditor(ListEditorConfig{LabelKey: "id"})
	le.SetItems([]map[string]interface{}{
		{"id": "A"},
		{"id": "B"},
		{"id": "C"},
	})

	le.moveDown(0)
	items := le.Items()
	if items[0]["id"] != "B" || items[1]["id"] != "A" || items[2]["id"] != "C" {
		t.Fatalf("after moveDown(0): got %v %v %v", items[0]["id"], items[1]["id"], items[2]["id"])
	}

	le.moveUp(2)
	items = le.Items()
	if items[0]["id"] != "B" || items[1]["id"] != "C" || items[2]["id"] != "A" {
		t.Fatalf("after moveUp(2): got %v %v %v", items[0]["id"], items[1]["id"], items[2]["id"])
	}
}

func TestListEditor_Toggle(t *testing.T) {
	le := NewListEditor(ListEditorConfig{LabelKey: "id", ToggleKey: "enabled"})
	le.SetItems([]map[string]interface{}{
		{"id": "test", "enabled": true},
	})

	if !le.itemToggle(le.Items()[0]) {
		t.Fatal("expected enabled=true initially")
	}
	le.toggleItem(0)
	if le.itemToggle(le.Items()[0]) {
		t.Fatal("expected enabled=false after toggle")
	}
}

func TestListEditor_ExpandCollapse(t *testing.T) {
	le := NewListEditor(ListEditorConfig{LabelKey: "id"})
	le.SetItems([]map[string]interface{}{
		{"id": "test", "style": "mocha"},
	})

	if le.expandedIdx != -1 {
		t.Fatal("expected no expansion initially")
	}
	le.toggleExpand(0)
	if le.expandedIdx != 0 {
		t.Fatal("expected item 0 expanded")
	}
	le.toggleExpand(0)
	if le.expandedIdx != -1 {
		t.Fatal("expected collapsed after second toggle")
	}
}

func TestListEditor_OnChange(t *testing.T) {
	changed := false
	le := NewListEditor(ListEditorConfig{LabelKey: "id"})
	le.OnChange = func(items []map[string]interface{}) {
		changed = true
	}
	le.SetItems([]map[string]interface{}{{"id": "x"}})
	changed = false

	le.addItem()
	if !changed {
		t.Fatal("expected OnChange to fire on add")
	}
	changed = false

	le.removeItem(0)
	if !changed {
		t.Fatal("expected OnChange to fire on remove")
	}
}

func TestListEditor_DetailKeys(t *testing.T) {
	le := NewListEditor(ListEditorConfig{LabelKey: "id", ToggleKey: "enabled"})
	item := map[string]interface{}{
		"id":      "txfmt",
		"enabled": true,
		"style":   "mocha",
		"extra":   42,
	}
	keys := le.detailKeys(item)
	if len(keys) != 2 {
		t.Fatalf("expected 2 detail keys, got %d: %v", len(keys), keys)
	}
	if keys[0] != "extra" || keys[1] != "style" {
		t.Fatalf("expected [extra, style], got %v", keys)
	}
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./primitives/ -v -run TestListEditor
```

Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add primitives/listeditor_test.go
git commit -m "Add ListEditor unit tests"
```

---

### Task 6: Wire ListEditor into config editor

**Files:**
- Modify: `texelation/apps/configeditor/field_builder.go` (in /home/marc/projects/texel/texelation/)

- [ ] **Step 1: Add list detection and builder**

In `field_builder.go`, change the `case []interface{}` branch:

```go
case []interface{}, []string:
    if items, ok := asListOfMaps(v); ok && hasKey(items, "id") {
        return fb.buildListEditor(fc, items, pane)
    }
    return fb.buildTextArea(fc, v, pane)
case map[string]interface{}:
    return fb.buildTextArea(fc, v, pane)
```

Add the helper functions:

```go
// asListOfMaps checks if a value is a []interface{} where all elements are maps.
func asListOfMaps(v interface{}) ([]map[string]interface{}, bool) {
    arr, ok := v.([]interface{})
    if !ok {
        return nil, false
    }
    result := make([]map[string]interface{}, 0, len(arr))
    for _, elem := range arr {
        m, ok := elem.(map[string]interface{})
        if !ok {
            return nil, false
        }
        result = append(result, m)
    }
    return result, true
}

// hasKey checks if all maps in the list contain the given key.
func hasKey(items []map[string]interface{}, key string) bool {
    for _, item := range items {
        if _, ok := item[key]; !ok {
            return false
        }
    }
    return len(items) > 0
}

func (fb *FieldBuilder) buildListEditor(fc FieldConfig, items []map[string]interface{}, pane *widgets.Form) *fieldBinding {
    editor := primitives.NewListEditor(primitives.ListEditorConfig{
        LabelKey:  "id",
        ToggleKey: "enabled",
    })
    editor.SetItems(items)

    // Calculate height: collapsed items + add button, capped at 10
    height := len(items) + 1
    if height > 10 {
        height = 10
    }
    if height < 3 {
        height = 3
    }

    editor.OnChange = func(newItems []map[string]interface{}) {
        // Convert back to []interface{} for config storage
        arr := make([]interface{}, len(newItems))
        for i, m := range newItems {
            arr[i] = m
        }
        updateConfigValue(fb.cfg, fc.Section, fc.Key, arr)
        fb.onApply(fc.ApplyKind)
    }

    pane.AddFullWidthField(editor, height)
    return &fieldBinding{section: fc.Section, key: fc.Key, kind: fieldText, widget: editor}
}
```

Add import: `"github.com/framegrace/texelui/primitives"`

- [ ] **Step 2: Build**

```bash
cd /home/marc/projects/texel/texelation
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add apps/configeditor/field_builder.go
git commit -m "Wire ListEditor into config editor for array-of-maps fields"
```

---

### Task 7: Manual testing

- [ ] **Step 1: Build both projects**

```bash
cd /home/marc/projects/texel/texelui && go build ./...
cd /home/marc/projects/texel/texelation && make build
```

- [ ] **Step 2: Test**

Start texelation, open config editor (F4), navigate to Transformers tab. Verify:
- Pipeline shows as a list with `txfmt` and `tablefmt` rows
- Checkboxes toggle enable/disable
- ▸ expands to show per-transformer fields (style, max_buffer_rows)
- ▲/▼ reorders transformers
- ✕ removes a transformer
- [+ Add] adds a new entry
- Changes are saved to config

- [ ] **Step 3: Commit any fixes**

```bash
git add -A
git commit -m "Fix issues found during manual testing"
```
