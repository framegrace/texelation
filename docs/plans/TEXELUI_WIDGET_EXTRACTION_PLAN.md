# TexelUI Widget Extraction Plan

This document details the plan to extract reusable widgets from the ColorPicker implementation and address code quality issues in the TexelUI library.

**Created:** 2025-12-27
**Status:** ✅ Completed (2025-12-27)

## Executive Summary

The ColorPicker widget works perfectly but contains patterns that should be reusable:
- **TabBar**: Horizontal tab navigation (used for mode switching)
- **ScrollableList**: Vertical scrollable list with selection (used in SemanticPicker)
- **Grid**: 2D grid layout with selection (used in PalettePicker)
- **ColorSwatch**: Color preview pattern `[██]` (used everywhere)

Additionally, we'll fix identified code quality issues:
- TextArea uses legacy theme API
- 150 lines of dead code in TextArea
- Unused `enabled`/`visible` fields in BaseWidget

## Approach: Pragmatic Balance (Modified)

**Decision**: Extract 3 full widgets + 1 helper function

| Component | Implementation | Rationale |
|-----------|---------------|-----------|
| **TabBar** | Full Widget | Requested, reusable for tabs |
| **ScrollableList** | Full Widget | High reuse (dropdowns, menus) |
| **Grid** | Full Widget | High reuse (icon grids, palettes) |
| **ColorSwatch** | Helper function | Only 4 lines, widget overhead not justified |

## Implementation Phases

### Phase 1: Code Quality Fixes (Low Risk)

**1.1 Fix TextArea legacy theme API** (`textarea.go:31-43`)

Before:
```go
bg := tm.GetColor("ui", "text_bg", tcell.ColorBlack)
fg := tm.GetColor("ui", "text_fg", tcell.ColorWhite)
```

After:
```go
bg := tm.GetSemanticColor("bg.surface")
fg := tm.GetSemanticColor("text.primary")
```

**1.2 Remove dead code** (`textarea.go:172-321`)

Delete the ~150 lines of commented-out `HandleKeyOld` function.

**1.3 Remove unused BaseWidget fields** (`core/widget.go`)

Remove `enabled` and `visible` fields from BaseWidget struct (lines 30-31). These are defined but never used by any widget.

### Phase 2: Create New Widgets

#### 2.1 TabBar Widget (`widgets/tabbar.go`)

**Purpose**: Horizontal tab navigation with keyboard and mouse support.

**API**:
```go
type TabItem struct {
    Label string
    ID    string  // Optional identifier
}

type TabBar struct {
    core.BaseWidget
    Tabs       []TabItem
    ActiveIdx  int
    OnChange   func(int)  // Called when tab selection changes
    inv        func(core.Rect)
}

// Constructor
func NewTabBar(x, y, w int, tabs []TabItem) *TabBar

// Widget interface
func (tb *TabBar) Draw(p *core.Painter)
func (tb *TabBar) HandleKey(ev *tcell.EventKey) bool  // Left/Right, 1-9
func (tb *TabBar) HandleMouse(ev *tcell.EventMouse) bool
func (tb *TabBar) SetInvalidator(fn func(core.Rect))

// Public API
func (tb *TabBar) SetActive(idx int)
func (tb *TabBar) ActiveTab() TabItem
```

**Key Handling**:
- Left/Right arrows: Navigate between tabs
- Number keys 1-9: Jump to tab by index
- Returns true if handled, false to let parent handle

**Visual Behavior**:
- Active tab: Reverse video (+ bold when TabBar is focused)
- Inactive tabs: Dim when focused, normal otherwise
- Optional focus marker '►' at left edge

**Estimated Lines**: ~150

---

#### 2.2 ScrollableList Widget (`widgets/scrollablelist.go`)

**Purpose**: Vertical scrollable list with item selection.

**API**:
```go
type ListItem struct {
    Text  string
    Value interface{}  // Optional data payload
}

type ScrollableList struct {
    core.BaseWidget
    Items       []ListItem
    SelectedIdx int
    OnChange    func(int)  // Called when selection changes

    // Custom rendering (optional)
    RenderItem func(p *core.Painter, rect core.Rect, item ListItem, selected bool)

    inv func(core.Rect)
}

// Constructor
func NewScrollableList(x, y, w, h int) *ScrollableList

// Widget interface
func (sl *ScrollableList) Draw(p *core.Painter)
func (sl *ScrollableList) HandleKey(ev *tcell.EventKey) bool
func (sl *ScrollableList) HandleMouse(ev *tcell.EventMouse) bool
func (sl *ScrollableList) SetInvalidator(fn func(core.Rect))

// Public API
func (sl *ScrollableList) SetItems(items []ListItem)
func (sl *ScrollableList) SetSelected(idx int)
func (sl *ScrollableList) SelectedItem() *ListItem
func (sl *ScrollableList) Clear()
```

**Key Handling**:
- Up/Down: Navigate items
- Home/End: Jump to first/last
- PgUp/PgDn: Page navigation
- Returns false when at boundary (let parent handle Tab)

**Visual Behavior**:
- Selected item: Reverse video
- Scroll indicators: ▲ (top), ▼ (bottom) when content overflows
- Centers selected item in viewport when navigating

**Estimated Lines**: ~200

---

#### 2.3 Grid Widget (`widgets/grid.go`)

**Purpose**: 2D grid layout with cell selection and navigation.

**API**:
```go
type GridItem struct {
    Text  string
    Value interface{}  // Optional data payload
}

type Grid struct {
    core.BaseWidget
    Items        []GridItem
    SelectedIdx  int
    MinCellWidth int  // Minimum cell width (for column calculation)
    MaxCols      int  // Maximum columns (0 = unlimited)
    OnChange     func(int)  // Called when selection changes

    // Custom rendering (optional)
    RenderCell func(p *core.Painter, rect core.Rect, item GridItem, selected bool)

    // Internal state
    cols int
    inv  func(core.Rect)
}

// Constructor
func NewGrid(x, y, w, h int) *Grid

// Widget interface
func (g *Grid) Draw(p *core.Painter)
func (g *Grid) HandleKey(ev *tcell.EventKey) bool
func (g *Grid) HandleMouse(ev *tcell.EventMouse) bool
func (g *Grid) SetInvalidator(fn func(core.Rect))

// Public API
func (g *Grid) SetItems(items []GridItem)
func (g *Grid) SetSelected(idx int)
func (g *Grid) SelectedItem() *GridItem
func (g *Grid) Columns() int  // Returns calculated column count
```

**Key Handling**:
- Arrow keys: 2D navigation
- Tab: Left-to-right, top-to-bottom sequential navigation
- Home/End: First/last item

**Layout Algorithm**:
1. Find longest item text
2. Calculate cell width: max(MinCellWidth, longestText + padding)
3. Calculate columns: min(availableWidth / cellWidth, MaxCols)
4. Distribute remaining width evenly

**Estimated Lines**: ~250

---

#### 2.4 ColorSwatch Helper (`widgets/colorpicker/helpers.go`)

**Purpose**: Render color preview in consistent format.

**API**:
```go
// DrawColorSwatch renders [██] with the given color
func DrawColorSwatch(p *core.Painter, x, y int, color tcell.Color, bracketStyle tcell.Style)

// DrawColorSwatchWithLabel renders [█A] where A is the label on background
func DrawColorSwatchWithLabel(p *core.Painter, x, y int, color, bgColor tcell.Color, label rune, bracketStyle tcell.Style)
```

**Usage**:
```go
// In SemanticPicker
DrawColorSwatch(p, x, y, selectedColor, baseStyle)
p.DrawText(x+5, y, colorName, style)

// In ColorPicker collapsed view
DrawColorSwatchWithLabel(p, x, y, result.Color, globalBg, 'A', style)
```

**Estimated Lines**: ~40

### Phase 3: Integrate Widgets into ColorPicker

#### 3.1 Refactor SemanticPicker (`colorpicker/semantic.go`)

**Before**: Manual scroll offset calculation, manual item rendering (~113 lines)

**After**:
```go
type SemanticPicker struct {
    list *ScrollableList
    semanticNames []string
}

func NewSemanticPicker() *SemanticPicker {
    sp := &SemanticPicker{
        semanticNames: []string{"accent", "bg.base", ...},
    }

    items := make([]ListItem, len(sp.semanticNames))
    for i, name := range sp.semanticNames {
        items[i] = ListItem{Text: name, Value: name}
    }

    sp.list = NewScrollableList(0, 0, 28, 10)
    sp.list.SetItems(items)
    sp.list.RenderItem = sp.renderColorItem

    return sp
}

func (sp *SemanticPicker) renderColorItem(p *core.Painter, rect core.Rect, item ListItem, selected bool) {
    name := item.Text
    color := theme.Get().GetSemanticColor(name)
    style := baseStyle
    if selected {
        style = style.Reverse(true)
    }
    DrawColorSwatch(p, rect.X, rect.Y, color, style)
    p.DrawText(rect.X+5, rect.Y, name, style)
}
```

**Estimated change**: -60 lines removed, +30 lines added

---

#### 3.2 Refactor PalettePicker (`colorpicker/palette.go`)

**Before**: Manual column calculation, manual grid rendering (~134 lines)

**After**:
```go
type PalettePicker struct {
    grid *Grid
    paletteNames []string
}

func NewPalettePicker() *PalettePicker {
    pp := &PalettePicker{
        paletteNames: []string{"rosewater", "flamingo", ...},
    }

    items := make([]GridItem, len(pp.paletteNames))
    for i, name := range pp.paletteNames {
        items[i] = GridItem{Text: name, Value: name}
    }

    pp.grid = NewGrid(0, 0, 40, 12)
    pp.grid.MinCellWidth = 15
    pp.grid.MaxCols = 3
    pp.grid.SetItems(items)
    pp.grid.RenderCell = pp.renderColorCell

    return pp
}

func (pp *PalettePicker) renderColorCell(p *core.Painter, rect core.Rect, item GridItem, selected bool) {
    name := item.Text
    color := theme.ResolveColorName(name)
    style := baseStyle
    if selected {
        style = style.Reverse(true)
    }
    DrawColorSwatch(p, rect.X, rect.Y, color, style)
    p.DrawText(rect.X+5, rect.Y, name, style)
}
```

**Estimated change**: -100 lines removed, +40 lines added

---

#### 3.3 Refactor ColorPicker (`colorpicker.go`)

**Before**: Manual tab bar rendering (lines 318-347), manual tab navigation

**After**:
```go
type ColorPicker struct {
    core.BaseWidget
    config      ColorPickerConfig
    expanded    bool
    currentMode ColorPickerMode
    result      ColorPickerResult
    focus       focusArea

    // NEW: Use TabBar widget
    tabBar     *TabBar

    modes      map[ColorPickerMode]colorpicker.ModePicker
    modeOrder  []ColorPickerMode
    activeMode colorpicker.ModePicker
    OnChange   func(ColorPickerResult)
    inv        func(core.Rect)
}

func NewColorPicker(x, y int, config ColorPickerConfig) *ColorPicker {
    cp := &ColorPicker{...}

    // Build tabs from enabled modes
    tabs := []TabItem{}
    for _, mode := range cp.modeOrder {
        tabs = append(tabs, TabItem{Label: mode.String(), ID: string(mode)})
    }

    cp.tabBar = NewTabBar(0, 0, 30, tabs)
    cp.tabBar.OnChange = func(idx int) {
        cp.selectMode(cp.modeOrder[idx])
    }

    return cp
}

func (cp *ColorPicker) drawExpanded(painter *core.Painter) {
    // ...border drawing...

    // Position and draw TabBar on top border
    cp.tabBar.SetPosition(cp.Rect.X + 2, cp.Rect.Y)
    cp.tabBar.Resize(cp.Rect.W - 4, 1)
    if cp.focus == focusTabBar {
        cp.tabBar.Focus()
    } else {
        cp.tabBar.Blur()
    }
    cp.tabBar.Draw(painter)

    // ...mode content drawing...
}
```

**Estimated change**: -80 lines removed, +40 lines added

### Phase 4: Testing & Validation

1. **Unit Tests** for new widgets:
   - `widgets/tabbar_test.go` (~100 lines)
   - `widgets/scrollablelist_test.go` (~150 lines)
   - `widgets/grid_test.go` (~150 lines)

2. **Integration Tests**:
   - Ensure ColorPicker still works identically
   - Test keyboard navigation through all modes
   - Test mouse interaction

3. **Manual Testing**:
   - Run ColorPicker demo
   - Verify tab switching
   - Verify list/grid selection
   - Verify modal behavior

## File Changes Summary

### New Files (7)
| File | Lines | Purpose |
|------|-------|---------|
| `widgets/tabbar.go` | ~150 | TabBar widget |
| `widgets/tabbar_test.go` | ~100 | TabBar tests |
| `widgets/scrollablelist.go` | ~200 | ScrollableList widget |
| `widgets/scrollablelist_test.go` | ~150 | ScrollableList tests |
| `widgets/grid.go` | ~250 | Grid widget |
| `widgets/grid_test.go` | ~150 | Grid tests |
| `widgets/colorpicker/helpers.go` | ~40 | ColorSwatch helpers |

### Modified Files (5)
| File | Changes |
|------|---------|
| `core/widget.go` | Remove `enabled`, `visible` fields |
| `widgets/textarea.go` | Fix theme API, remove dead code |
| `widgets/colorpicker.go` | Use TabBar widget |
| `widgets/colorpicker/semantic.go` | Use ScrollableList |
| `widgets/colorpicker/palette.go` | Use Grid |

### Totals
- **New code**: ~1040 lines (widgets + tests)
- **Removed code**: ~390 lines (dead code + refactored logic)
- **Net change**: +650 lines

## Success Criteria

1. All existing tests pass (`make test`)
2. ColorPicker functionality unchanged
3. New widgets work standalone
4. Code is more DRY (no duplicated scroll/grid logic)
5. Theme API is consistent across all widgets

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Breaking ColorPicker | Implement one phase at a time, test after each |
| Over-abstraction | Helper pattern for ColorSwatch instead of full widget |
| Merge conflicts | Work on feature branch, keep commits small |

## Timeline

Estimated effort: 6-8 hours

| Phase | Effort |
|-------|--------|
| Phase 1: Code Quality | 30 min |
| Phase 2: Create Widgets | 3-4 hours |
| Phase 3: Integration | 2-3 hours |
| Phase 4: Testing | 1 hour |

## Checklist

### Phase 1: Code Quality
- [ ] Fix TextArea legacy theme API
- [ ] Remove dead code from TextArea
- [ ] Remove unused BaseWidget fields
- [ ] Run tests, verify no breakage

### Phase 2: Create Widgets
- [ ] Create TabBar widget
- [ ] Create TabBar tests
- [ ] Create ScrollableList widget
- [ ] Create ScrollableList tests
- [ ] Create Grid widget
- [ ] Create Grid tests
- [ ] Create ColorSwatch helper

### Phase 3: Integration
- [ ] Refactor SemanticPicker
- [ ] Refactor PalettePicker
- [ ] Refactor ColorPicker
- [ ] Test ColorPicker thoroughly

### Phase 4: Validation
- [ ] Run `make test`
- [ ] Manual test ColorPicker
- [ ] Update TEXELUI_PLAN.md status
