# List Editor Widget

## Overview

A generic `ListEditor` widget in `texelui/primitives` for editing arrays of objects. Each item shows a label, a toggle checkbox, expandable detail fields, reorder buttons, and a remove button. Used by the config editor to replace raw JSON textareas for structured arrays like the transformer pipeline.

## Motivation

The transformer pipeline config (`transformers.pipeline`) is an array of objects currently rendered as a raw JSON textarea. This is error-prone and ugly. A proper list widget with per-item toggle, expand/collapse, and reorder makes the config editor usable for structured array data.

## Widget: `texelui/primitives/ListEditor`

### Configuration

```go
type ListEditorConfig struct {
    LabelKey  string // map key to use as the row label (e.g., "id")
    ToggleKey string // map key for the checkbox (e.g., "enabled"), "" = no checkbox
}
```

### Constructor

```go
func NewListEditor(cfg ListEditorConfig) *ListEditor
```

### Data

```go
func (le *ListEditor) SetItems(items []map[string]interface{})
func (le *ListEditor) Items() []map[string]interface{}
```

### Callback

```go
le.OnChange = func(items []map[string]interface{})
```

Fired on any change: toggle, reorder, remove, add, field edit.

### Per-Item Layout

**Collapsed row:**
```
☑ txfmt                    ▲ ▼ ✕ ▸
```
- Checkbox: bound to `ToggleKey` field
- Label: value of `LabelKey` field
- ▲/▼: reorder (disabled at top/bottom)
- ✕: remove item
- ▸: expand to show detail fields

**Expanded row:**
```
☑ txfmt                    ▲ ▼ ✕ ▾
  style: [catppuccin-mocha    ]
```
- ▾: collapse
- Detail fields: all map keys except `LabelKey` and `ToggleKey`
- Field type inferred from value: `bool` → checkbox, `float64` → number input, `string` → text input

**Add button at bottom:**
```
[+ Add]
```
Adds a new empty item with default values. The `LabelKey` field gets an input for the user to type the ID.

### Interaction

- **Click checkbox**: toggles the `ToggleKey` field, fires `OnChange`
- **Click ▸/▾**: expand/collapse detail area
- **Click ▲/▼**: swap item with neighbor, fires `OnChange`
- **Click ✕**: remove item from list, fires `OnChange`
- **Edit detail field**: updates the map value, fires `OnChange`
- **Click Add**: appends new item, expands it for editing
- **Keyboard**: Tab focuses between items, Enter toggles expand, arrow keys for reorder when focused

### Rendering

The widget renders as a vertical list within its bounds. Each item is 1 row when collapsed, 1 + field count rows when expanded. The widget scrolls if content exceeds height (uses existing scroll infrastructure).

## Config Editor Integration

### `apps/configeditor/field_builder.go`

In the `case []interface{}` branch, check if the array elements are maps with an `"id"` key (or whatever `LabelKey` the caller specifies). If so, create a `ListEditor` instead of a textarea:

```go
case []interface{}:
    if isListOfMapsWithKey(v, "id") {
        return fb.buildListEditor(fc, v, pane)
    }
    return fb.buildTextArea(fc, v, pane)
```

The `buildListEditor` method creates a `ListEditor` with `LabelKey: "id"`, `ToggleKey: "enabled"`, sets the items, and wires `OnChange` to update the config.

### Detection Heuristic

`isListOfMapsWithKey(v []interface{}, key string) bool` — returns true if all elements are `map[string]interface{}` and contain the specified key. This is generic enough for any array of objects with an ID field.

## Testing

- **TestListEditor_AddRemove**: add items, remove one, verify count and order
- **TestListEditor_Reorder**: move item up/down, verify order
- **TestListEditor_Toggle**: toggle checkbox, verify map value changes
- **TestListEditor_FieldEdit**: expand item, change a field, verify map updated
- **TestListEditor_OnChange**: verify callback fires on each mutation

## Out of Scope

- Drag and drop reordering
- Nested arrays within items
- Custom field widgets per transformer type (all fields are generic inputs)
- Validation of field values
