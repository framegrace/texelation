# Client-Side DynamicColor Resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate animated DynamicColors from server-side rendering (60fps buffer deltas) to client-side resolution via serializable descriptors.

**Architecture:** DynamicColor gets a serializable `DynamicColorDesc` descriptor. `Cell` gains optional `DynFG`/`DynBG` fields. The protocol carries descriptors alongside static colors. The client resolves animated colors locally each frame. Standalone mode is unchanged.

**Tech Stack:** Go 1.24.3, texelui (color, core, animation), texelation (protocol, publisher, client renderer)

**Spec:** `docs/superpowers/specs/2026-03-30-client-side-dynamic-colors-design.md`

---

## File Structure

### texelui changes

| File | Responsibility |
|------|---------------|
| `color/dynamic.go` (modify) | Add `DynamicColorDesc`, `Describe()`, `FromDesc()`, `Pulse()`, `Fade()` |
| `color/dynamic_test.go` (modify/create) | Tests for descriptors and named constructors |
| `color/easing_table.go` (create) | Shared easing index/name/function mapping |
| `core/cell.go` (modify) | Add `DynFG`, `DynBG` fields |
| `core/painter.go` (modify) | Store descriptors in `SetDynamicCell` |
| `core/uimanager.go` (modify) | Add `ClientSideAnimations` flag |

### texelation changes

| File | Responsibility |
|------|---------------|
| `protocol/buffer_delta.go` (modify) | Extend `StyleEntry` with dynamic descriptors, update encode/decode |
| `protocol/messages_test.go` (modify) | Round-trip tests for dynamic styles |
| `internal/runtime/server/desktop_publisher.go` (modify) | Copy dynamic descriptors in `convertStyle` |
| `internal/runtime/client/renderer.go` (modify) | Resolve dynamic cells with `ColorContext` each frame |
| `apps/statusbar/statusbar.go` (modify) | Replace `makePulse()` with `color.Pulse()` |
| `cmd/texel-server/main.go` (modify) | Set `ClientSideAnimations = true` on status bar UIManager |

---

## Task 1: Add Easing Table to texelui

**Files:**
- Create: `/home/marc/projects/texel/texelui/color/easing_table.go`

### Steps

- [ ] **Step 1: Create the shared easing table**

Create `/home/marc/projects/texel/texelui/color/easing_table.go`:

```go
package color

import "github.com/framegrace/texelui/animation"

// EasingIndex maps easing function names to uint8 indices for protocol serialization.
const (
	EasingLinear       uint8 = 0
	EasingSmoothstep   uint8 = 1
	EasingSmootherstep uint8 = 2
	EasingInQuad       uint8 = 3
	EasingOutQuad      uint8 = 4
	EasingInOutQuad    uint8 = 5
	EasingInCubic      uint8 = 6
	EasingOutCubic     uint8 = 7
	EasingInOutCubic   uint8 = 8
)

// EasingByIndex returns the easing function for the given index.
// Returns EaseLinear for unknown indices.
var EasingByIndex = []animation.EasingFunc{
	animation.EaseLinear,
	animation.EaseSmoothstep,
	animation.EaseSmootherstep,
	animation.EaseInQuad,
	animation.EaseOutQuad,
	animation.EaseInOutQuad,
	animation.EaseInCubic,
	animation.EaseOutCubic,
	animation.EaseInOutCubic,
}

// EasingByName maps easing names to indices.
var EasingByName = map[string]uint8{
	"linear":        EasingLinear,
	"smoothstep":    EasingSmoothstep,
	"smootherstep":  EasingSmootherstep,
	"ease-in-quad":  EasingInQuad,
	"ease-out-quad": EasingOutQuad,
	"ease-in-out-quad": EasingInOutQuad,
	"ease-in-cubic":    EasingInCubic,
	"ease-out-cubic":   EasingOutCubic,
	"ease-in-out-cubic": EasingInOutCubic,
}

// LookupEasing returns the easing function for an index, defaulting to linear.
func LookupEasing(idx uint8) animation.EasingFunc {
	if int(idx) < len(EasingByIndex) {
		return EasingByIndex[idx]
	}
	return animation.EaseLinear
}

// LookupEasingByName returns the index for a named easing, defaulting to linear.
func LookupEasingByName(name string) uint8 {
	if idx, ok := EasingByName[name]; ok {
		return idx
	}
	return EasingLinear
}
```

- [ ] **Step 2: Run tests**

Run: `cd /home/marc/projects/texel/texelui && go build ./color/`
Expected: builds clean.

- [ ] **Step 3: Commit**

```bash
cd /home/marc/projects/texel/texelui && git add color/easing_table.go
git commit -m "Add shared easing index table for DynamicColor serialization"
```

---

## Task 2: Add DynamicColorDesc and Named Constructors

**Files:**
- Modify: `/home/marc/projects/texel/texelui/color/dynamic.go`
- Create: `/home/marc/projects/texel/texelui/color/dynamic_test.go` (or modify if exists)

### Steps

- [ ] **Step 1: Write tests for DynamicColorDesc**

Create or append to `/home/marc/projects/texel/texelui/color/dynamic_test.go`:

```go
package color

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestDynamicColorDesc_Solid(t *testing.T) {
	dc := Solid(tcell.NewRGBColor(100, 150, 200))
	desc := dc.Describe()
	if desc.Type != DescTypeSolid {
		t.Fatalf("expected DescTypeSolid, got %d", desc.Type)
	}
	if desc.Base != packRGB(100, 150, 200) {
		t.Errorf("base color mismatch: got %x", desc.Base)
	}
}

func TestDynamicColorDesc_Pulse(t *testing.T) {
	dc := Pulse(tcell.NewRGBColor(100, 150, 200), 0.7, 1.0, 6)
	desc := dc.Describe()
	if desc.Type != DescTypePulse {
		t.Fatalf("expected DescTypePulse, got %d", desc.Type)
	}
	if desc.Min != 0.7 || desc.Max != 1.0 || desc.Speed != 6 {
		t.Errorf("pulse params: min=%.1f max=%.1f speed=%.1f", desc.Min, desc.Max, desc.Speed)
	}
}

func TestDynamicColorDesc_Fade(t *testing.T) {
	dc := Fade(tcell.NewRGBColor(255, 0, 0), tcell.NewRGBColor(0, 0, 255), "smoothstep", 0.5)
	desc := dc.Describe()
	if desc.Type != DescTypeFade {
		t.Fatalf("expected DescTypeFade, got %d", desc.Type)
	}
	if desc.Base != packRGB(255, 0, 0) {
		t.Errorf("base color mismatch")
	}
	if desc.Target != packRGB(0, 0, 255) {
		t.Errorf("target color mismatch")
	}
}

func TestDynamicColorDesc_RoundTrip(t *testing.T) {
	original := Pulse(tcell.NewRGBColor(100, 150, 200), 0.7, 1.0, 6)
	desc := original.Describe()
	reconstructed := FromDesc(desc)

	ctx := ColorContext{T: 0.5}
	origColor := original.Resolve(ctx)
	reconColor := reconstructed.Resolve(ctx)
	if origColor != reconColor {
		t.Errorf("round-trip mismatch: original=%v reconstructed=%v", origColor, reconColor)
	}
}

func TestDynamicColorDesc_FuncNotSerializable(t *testing.T) {
	dc := Func(func(ctx ColorContext) tcell.Color { return tcell.ColorRed })
	desc := dc.Describe()
	// Raw Func can't be described — returns None
	if desc.Type != DescTypeNone {
		t.Errorf("expected DescTypeNone for raw Func, got %d", desc.Type)
	}
}

func packRGB(r, g, b int32) uint32 {
	return (uint32(r)&0xFF)<<16 | (uint32(g)&0xFF)<<8 | uint32(b)&0xFF
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelui && go test ./color/ -run TestDynamicColorDesc -v`
Expected: FAIL — `DescTypeSolid`, `Describe`, `Pulse`, `Fade`, `FromDesc` undefined.

- [ ] **Step 3: Add DynamicColorDesc type and constants**

In `/home/marc/projects/texel/texelui/color/dynamic.go`, add after the `DynamicStyle` struct:

```go
// DynamicColorDesc is a serializable descriptor for a DynamicColor.
// It captures the animation intent so it can travel across the protocol
// and be reconstructed on the client side.
type DynamicColorDesc struct {
	Type   uint8   // DescTypeNone, DescTypeSolid, DescTypePulse, DescTypeFade
	Base   uint32  // RGB packed base color
	Target uint32  // RGB packed target (for fade)
	Easing uint8   // index into EasingByIndex table
	Speed  float32 // oscillations/sec (pulse) or duration in seconds (fade)
	Min    float32 // min scale factor (pulse)
	Max    float32 // max scale factor (pulse)
}

const (
	DescTypeNone  uint8 = 0
	DescTypeSolid uint8 = 1
	DescTypePulse uint8 = 2
	DescTypeFade  uint8 = 3
)

// IsAnimated returns true if this descriptor represents an animated color.
func (d DynamicColorDesc) IsAnimated() bool {
	return d.Type >= DescTypePulse
}
```

- [ ] **Step 4: Add Describe method and helper functions**

In the same file, add:

```go
// PackRGB packs r, g, b int32 components into a uint32.
func PackRGB(r, g, b int32) uint32 {
	return (uint32(r)&0xFF)<<16 | (uint32(g)&0xFF)<<8 | uint32(b)&0xFF
}

// UnpackRGB unpacks a uint32 into r, g, b int32 components.
func UnpackRGB(rgb uint32) (int32, int32, int32) {
	return int32((rgb >> 16) & 0xFF), int32((rgb >> 8) & 0xFF), int32(rgb & 0xFF)
}

// Describe returns a serializable descriptor for this DynamicColor.
// Raw Func closures return DescTypeNone (cannot be serialized).
func (dc DynamicColor) Describe() DynamicColorDesc {
	return dc.desc
}
```

This requires adding a `desc` field to the `DynamicColor` struct:

```go
type DynamicColor struct {
	static   tcell.Color
	fn       ColorFunc
	animated bool
	desc     DynamicColorDesc
}
```

Update the `Solid` constructor:

```go
func Solid(c tcell.Color) DynamicColor {
	r, g, b := c.RGB()
	return DynamicColor{
		static: c,
		desc:   DynamicColorDesc{Type: DescTypeSolid, Base: PackRGB(r, g, b)},
	}
}
```

- [ ] **Step 5: Add Pulse and Fade constructors**

In the same file, add:

```go
// Pulse creates an oscillating brightness DynamicColor.
// Base color oscillates between min and max scale factors at speedHz oscillations/sec.
func Pulse(base tcell.Color, min, max, speedHz float32) DynamicColor {
	r, g, b := base.RGB()
	desc := DynamicColorDesc{
		Type:  DescTypePulse,
		Base:  PackRGB(r, g, b),
		Speed: speedHz,
		Min:   min,
		Max:   max,
	}
	fr, fg, fb := float64(r), float64(g), float64(b)
	return DynamicColor{
		static: base,
		fn: func(ctx ColorContext) tcell.Color {
			mid := float64(min+max) / 2
			amp := float64(max-min) / 2
			factor := mid + amp*math.Sin(float64(ctx.T)*float64(speedHz))
			return tcell.NewRGBColor(
				int32(fr*factor),
				int32(fg*factor),
				int32(fb*factor),
			)
		},
		animated: true,
		desc:     desc,
	}
}

// Fade creates a one-shot color transition DynamicColor.
func Fade(from, to tcell.Color, easing string, durationSec float32) DynamicColor {
	fr, fg, fb := from.RGB()
	tr, tg, tb := to.RGB()
	desc := DynamicColorDesc{
		Type:   DescTypeFade,
		Base:   PackRGB(fr, fg, fb),
		Target: PackRGB(tr, tg, tb),
		Easing: LookupEasingByName(easing),
		Speed:  durationSec,
	}
	easeFn := LookupEasing(desc.Easing)
	return DynamicColor{
		static: from,
		fn: func(ctx ColorContext) tcell.Color {
			progress := ctx.T / float32(durationSec)
			if progress >= 1 {
				return to
			}
			if progress < 0 {
				return from
			}
			t := easeFn(progress)
			blend := func(a, b int32) int32 {
				return a + int32(float32(b-a)*t)
			}
			return tcell.NewRGBColor(blend(fr, tr), blend(fg, tg), blend(fb, tb))
		},
		animated: true,
		desc:     desc,
	}
}
```

Add `"math"` to the imports of `dynamic.go`.

- [ ] **Step 6: Add FromDesc constructor**

In the same file:

```go
// FromDesc reconstructs a DynamicColor from a serializable descriptor.
func FromDesc(d DynamicColorDesc) DynamicColor {
	switch d.Type {
	case DescTypeSolid:
		r, g, b := UnpackRGB(d.Base)
		return Solid(tcell.NewRGBColor(r, g, b))
	case DescTypePulse:
		r, g, b := UnpackRGB(d.Base)
		return Pulse(tcell.NewRGBColor(r, g, b), d.Min, d.Max, d.Speed)
	case DescTypeFade:
		fr, fg, fb := UnpackRGB(d.Base)
		tr, tg, tb := UnpackRGB(d.Target)
		easingName := "linear"
		for name, idx := range EasingByName {
			if idx == d.Easing {
				easingName = name
				break
			}
		}
		return Fade(tcell.NewRGBColor(fr, fg, fb), tcell.NewRGBColor(tr, tg, tb), easingName, d.Speed)
	default:
		return DynamicColor{}
	}
}
```

- [ ] **Step 7: Run tests**

Run: `cd /home/marc/projects/texel/texelui && go test ./color/ -v`
Expected: All tests pass.

- [ ] **Step 8: Commit**

```bash
cd /home/marc/projects/texel/texelui && git add color/dynamic.go color/dynamic_test.go
git commit -m "Add DynamicColorDesc, Pulse/Fade constructors, Describe/FromDesc"
```

---

## Task 3: Extend Cell with DynFG/DynBG

**Files:**
- Modify: `/home/marc/projects/texel/texelui/core/cell.go`

### Steps

- [ ] **Step 1: Add DynFG and DynBG fields to Cell**

In `/home/marc/projects/texel/texelui/core/cell.go`, change the Cell struct:

```go
type Cell struct {
	Ch    rune
	Style tcell.Style
	DynFG color.DynamicColorDesc // zero Type = use Style fg
	DynBG color.DynamicColorDesc // zero Type = use Style bg
}
```

Add import for `"github.com/framegrace/texelui/color"`.

- [ ] **Step 2: Verify build**

Run: `cd /home/marc/projects/texel/texelui && go build ./...`
Expected: clean build. All existing code creates cells with `Cell{Ch: x, Style: s}` — the new fields default to zero value (Type=0, no animation).

- [ ] **Step 3: Run all tests**

Run: `cd /home/marc/projects/texel/texelui && go test ./...`
Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add core/cell.go
git commit -m "Add DynFG/DynBG DynamicColorDesc fields to Cell"
```

---

## Task 4: Update Painter to Store Descriptors

**Files:**
- Modify: `/home/marc/projects/texel/texelui/core/painter.go`

### Steps

- [ ] **Step 1: Update SetDynamicCell to store descriptors**

In `SetDynamicCell` (around line 193), after resolving the static color and writing the cell, store the descriptors. Find the line that writes the cell (around line 237-241):

```go
p.buf[y][x] = Cell{Ch: ch, Style: style}
```

Replace with:

```go
p.buf[y][x] = Cell{
	Ch:    ch,
	Style: style,
	DynFG: ds.FG.Describe(),
	DynBG: ds.BG.Describe(),
}
```

But only store descriptors that are animated (Type >= 2) to avoid overhead:

```go
cell := Cell{Ch: ch, Style: style}
if fgDesc := ds.FG.Describe(); fgDesc.Type >= color.DescTypePulse {
	cell.DynFG = fgDesc
}
if bgDesc := ds.BG.Describe(); bgDesc.Type >= color.DescTypePulse {
	cell.DynBG = bgDesc
}
p.buf[y][x] = cell
```

Do the same for `SetDynamicCellKeepBG` (around line 246).

- [ ] **Step 2: Run tests**

Run: `cd /home/marc/projects/texel/texelui && go test ./...`
Expected: all pass.

- [ ] **Step 3: Commit**

```bash
git add core/painter.go
git commit -m "Store DynamicColorDesc on cells in SetDynamicCell"
```

---

## Task 5: Add ClientSideAnimations Flag to UIManager

**Files:**
- Modify: `/home/marc/projects/texel/texelui/core/uimanager.go`

### Steps

- [ ] **Step 1: Add the flag**

In the `UIManager` struct (around line 22), add:

```go
// ClientSideAnimations suppresses server-side animation refresh scheduling.
// When true, HasAnimations() in Render does NOT trigger scheduleAnimationRefreshLocked.
// The client handles animation refresh instead.
ClientSideAnimations bool
```

- [ ] **Step 2: Guard the animation refresh calls**

In `Render()`, find the two `HasAnimations()` checks (around lines 925 and 969). Wrap them:

```go
if p.HasAnimations() && !u.ClientSideAnimations {
	u.scheduleAnimationRefreshLocked()
}
```

Do this for both occurrences.

- [ ] **Step 3: Run tests**

Run: `cd /home/marc/projects/texel/texelui && go test ./...`
Expected: all pass (flag defaults to false, so standalone behavior unchanged).

- [ ] **Step 4: Commit**

```bash
git add core/uimanager.go
git commit -m "Add ClientSideAnimations flag to suppress server-side animation refresh"
```

---

## Task 6: Extend Protocol StyleEntry

**Files:**
- Modify: `/home/marc/projects/texel/texelation/protocol/buffer_delta.go`
- Modify: `/home/marc/projects/texel/texelation/protocol/messages_test.go`

### Steps

- [ ] **Step 1: Add DynamicColorDesc to protocol package**

In `protocol/buffer_delta.go`, add before `StyleEntry`:

```go
// DynColorDesc is the protocol representation of a DynamicColor descriptor.
type DynColorDesc struct {
	Type   uint8
	Base   uint32
	Target uint32
	Easing uint8
	Speed  float32
	Min    float32
	Max    float32
}

// AttrHasDynamic is a flag bit in AttrFlags indicating dynamic color data follows.
const AttrHasDynamic uint16 = 1 << 8
```

Extend `StyleEntry`:

```go
type StyleEntry struct {
	AttrFlags uint16
	FgModel   ColorModel
	FgValue   uint32
	BgModel   ColorModel
	BgValue   uint32
	DynFG     DynColorDesc
	DynBG     DynColorDesc
}
```

- [ ] **Step 2: Update EncodeBufferDelta**

In `EncodeBufferDelta`, after encoding `BgModel` and `BgValue` for each style, check if `AttrHasDynamic` is set and encode the dynamic descriptors:

```go
if s.AttrFlags&AttrHasDynamic != 0 {
	binary.Write(buf, binary.LittleEndian, s.DynFG.Type)
	binary.Write(buf, binary.LittleEndian, s.DynFG.Base)
	binary.Write(buf, binary.LittleEndian, s.DynFG.Target)
	binary.Write(buf, binary.LittleEndian, s.DynFG.Easing)
	binary.Write(buf, binary.LittleEndian, s.DynFG.Speed)
	binary.Write(buf, binary.LittleEndian, s.DynFG.Min)
	binary.Write(buf, binary.LittleEndian, s.DynFG.Max)
	binary.Write(buf, binary.LittleEndian, s.DynBG.Type)
	binary.Write(buf, binary.LittleEndian, s.DynBG.Base)
	binary.Write(buf, binary.LittleEndian, s.DynBG.Target)
	binary.Write(buf, binary.LittleEndian, s.DynBG.Easing)
	binary.Write(buf, binary.LittleEndian, s.DynBG.Speed)
	binary.Write(buf, binary.LittleEndian, s.DynBG.Min)
	binary.Write(buf, binary.LittleEndian, s.DynBG.Max)
}
```

- [ ] **Step 3: Update DecodeBufferDelta**

In `DecodeBufferDelta`, after reading `BgModel` and `BgValue`, check the flag and decode:

```go
if s.AttrFlags&AttrHasDynamic != 0 {
	binary.Read(r, binary.LittleEndian, &s.DynFG.Type)
	binary.Read(r, binary.LittleEndian, &s.DynFG.Base)
	binary.Read(r, binary.LittleEndian, &s.DynFG.Target)
	binary.Read(r, binary.LittleEndian, &s.DynFG.Easing)
	binary.Read(r, binary.LittleEndian, &s.DynFG.Speed)
	binary.Read(r, binary.LittleEndian, &s.DynFG.Min)
	binary.Read(r, binary.LittleEndian, &s.DynFG.Max)
	binary.Read(r, binary.LittleEndian, &s.DynBG.Type)
	binary.Read(r, binary.LittleEndian, &s.DynBG.Base)
	binary.Read(r, binary.LittleEndian, &s.DynBG.Target)
	binary.Read(r, binary.LittleEndian, &s.DynBG.Easing)
	binary.Read(r, binary.LittleEndian, &s.DynBG.Speed)
	binary.Read(r, binary.LittleEndian, &s.DynBG.Min)
	binary.Read(r, binary.LittleEndian, &s.DynBG.Max)
}
```

- [ ] **Step 4: Write round-trip test**

In `protocol/messages_test.go`, add:

```go
func TestBufferDeltaDynamicColorRoundTrip(t *testing.T) {
	delta := BufferDelta{
		PaneID:   [16]byte{1},
		Revision: 1,
		Styles: []StyleEntry{
			{
				AttrFlags: AttrHasDynamic,
				FgModel:   ColorModelRGB,
				FgValue:   0xFF0000,
				BgModel:   ColorModelRGB,
				BgValue:   0x0000FF,
				DynBG: DynColorDesc{
					Type:  2, // pulse
					Base:  0x89B4FA,
					Speed: 6,
					Min:   0.7,
					Max:   1.0,
				},
			},
		},
		Rows: []RowDelta{{Row: 0, Spans: []CellSpan{{StartCol: 0, Text: "x", StyleIndex: 0}}}},
	}
	encoded, err := EncodeBufferDelta(delta)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeBufferDelta(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Styles[0].DynBG.Type != 2 {
		t.Errorf("expected pulse type 2, got %d", decoded.Styles[0].DynBG.Type)
	}
	if decoded.Styles[0].DynBG.Base != 0x89B4FA {
		t.Errorf("base color mismatch: %x", decoded.Styles[0].DynBG.Base)
	}
	if decoded.Styles[0].DynBG.Speed != 6 {
		t.Errorf("speed mismatch: %f", decoded.Styles[0].DynBG.Speed)
	}
}

func TestBufferDeltaStaticBackwardCompat(t *testing.T) {
	delta := BufferDelta{
		PaneID:   [16]byte{1},
		Revision: 1,
		Styles:   []StyleEntry{{FgModel: ColorModelRGB, FgValue: 0xFF0000}},
		Rows:     []RowDelta{{Row: 0, Spans: []CellSpan{{StartCol: 0, Text: "x", StyleIndex: 0}}}},
	}
	encoded, err := EncodeBufferDelta(delta)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeBufferDelta(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Styles[0].AttrFlags&AttrHasDynamic != 0 {
		t.Error("static style should not have dynamic flag")
	}
	if decoded.Styles[0].DynBG.Type != 0 {
		t.Error("static style should have zero DynBG")
	}
}
```

- [ ] **Step 5: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./protocol/ -v`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add protocol/buffer_delta.go protocol/messages_test.go
git commit -m "Extend protocol StyleEntry with DynamicColorDesc descriptors"
```

---

## Task 7: Update Publisher to Copy Dynamic Descriptors

**Files:**
- Modify: `/home/marc/projects/texel/texelation/internal/runtime/server/desktop_publisher.go`

### Steps

- [ ] **Step 1: Update convertStyle to include dynamic descriptors**

In `convertStyle` (around line 177), change the function signature to accept the full `Cell` instead of just `tcell.Style`:

```go
func convertCell(cell texel.Cell) (styleKey, protocol.StyleEntry)
```

The function body:
1. Keeps the existing style decomposition: `fg, bg, attrs := cell.Style.Decompose()`
2. After building `key` and `entry`, checks for dynamic descriptors:

```go
if cell.DynFG.Type >= 2 || cell.DynBG.Type >= 2 {
	entry.AttrFlags |= protocol.AttrHasDynamic
	entry.DynFG = protocol.DynColorDesc{
		Type: cell.DynFG.Type, Base: cell.DynFG.Base, Target: cell.DynFG.Target,
		Easing: cell.DynFG.Easing, Speed: cell.DynFG.Speed, Min: cell.DynFG.Min, Max: cell.DynFG.Max,
	}
	entry.DynBG = protocol.DynColorDesc{
		Type: cell.DynBG.Type, Base: cell.DynBG.Base, Target: cell.DynBG.Target,
		Easing: cell.DynBG.Easing, Speed: cell.DynBG.Speed, Min: cell.DynBG.Min, Max: cell.DynBG.Max,
	}
	// Include dynamic info in the key so animated styles are distinct
	key.dynFGType = cell.DynFG.Type
	key.dynBGType = cell.DynBG.Type
}
```

Update `styleKey` to include dynamic type fields for deduplication:

```go
type styleKey struct {
	attrFlags uint16
	fgModel   protocol.ColorModel
	fgValue   uint32
	bgModel   protocol.ColorModel
	bgValue   uint32
	dynFGType uint8
	dynBGType uint8
}
```

Update the call site in `bufferToDelta` (around line 137) from `convertStyle(cell.Style)` to `convertCell(cell)`.

- [ ] **Step 2: Run tests**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: all pass.

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/server/desktop_publisher.go
git commit -m "Publisher copies DynamicColorDesc from cells to protocol StyleEntry"
```

---

## Task 8: Update Client Renderer to Resolve Dynamic Cells

**Files:**
- Modify: `/home/marc/projects/texel/texelation/internal/runtime/client/renderer.go`
- Modify: `/home/marc/projects/texel/texelation/client/buffercache.go` (if Cell type needs updating)

### Steps

- [ ] **Step 1: Check client Cell type**

The client uses `client.Cell` from the `client/` package. Check if it has the same structure as `texelui/core.Cell`. If not, it needs `DynFG`/`DynBG` fields too. The implementing agent should check `client/buffercache.go` for the Cell definition and add the fields if missing.

Also check how `DecodeBufferDelta` results are applied to the client cache — the `DynFG`/`DynBG` from `StyleEntry` need to be stored on the cached cells.

- [ ] **Step 2: Update compositeInto to resolve dynamic colors**

In `compositeInto` (around line 116), after building `paneBuffer` and before writing to `workspaceBuffer`, add dynamic color resolution:

```go
// Track if any dynamic cells need animation refresh
hasDynamic := false

// ... existing compositing loop ...

for col := 0; col < w; col++ {
	targetX := x + col
	if targetX < 0 || targetX >= screenW {
		continue
	}

	cell := row[col]
	style := cell.Style

	// Resolve dynamic colors
	if cell.DynBG.Type >= 2 || cell.DynFG.Type >= 2 {
		ctx := color.ColorContext{
			X: col, Y: rowIdx,
			W: w, H: h,
			PX: x, PY: y,
			PW: w, PH: h,
			SX: targetX, SY: targetY,
			SW: screenW, SH: screenH,
			T: state.animTime,
		}
		fg, bg, attrs := style.Decompose()
		if cell.DynBG.Type >= 2 {
			bg = color.FromDesc(cell.DynBG).Resolve(ctx)
		}
		if cell.DynFG.Type >= 2 {
			fg = color.FromDesc(cell.DynFG).Resolve(ctx)
		}
		style = tcell.StyleDefault.Foreground(fg).Background(bg).Attributes(attrs)
		hasDynamic = true
	}

	if zoomOverlay {
		style = applyZoomOverlay(style, 0.2, state)
	}
	workspaceBuffer[targetY][targetX] = client.Cell{Ch: cell.Ch, Style: style}
}
```

Add `color "github.com/framegrace/texelui/color"` to imports.

- [ ] **Step 3: Add animTime to clientState and request frames**

In `clientState` (in `client_state.go` or wherever it's defined), add:

```go
animTime  float32
animStart time.Time
```

Initialize `animStart = time.Now()` at client startup.

In the `render()` function, before compositing, compute:

```go
state.animTime = float32(time.Since(state.animStart).Seconds())
```

After compositing, if `hasDynamic` is true, request another frame:

```go
if hasDynamic {
	select {
	case renderCh <- struct{}{}:
	default:
	}
}
```

The implementing agent should find where `renderCh` is accessible and wire this up — it may need to be passed into `compositeInto` or returned as a boolean from the function.

- [ ] **Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && make test && make build`
Expected: all pass, build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/client/renderer.go internal/runtime/client/client_state.go client/buffercache.go
git commit -m "Client resolves DynamicColor cells locally with full ColorContext"
```

---

## Task 9: Migrate StatusBar and Wire Server Flag

**Files:**
- Modify: `/home/marc/projects/texel/texelation/apps/statusbar/statusbar.go`
- Modify: `/home/marc/projects/texel/texelation/cmd/texel-server/main.go`

### Steps

- [ ] **Step 1: Replace makePulse with color.Pulse**

In `apps/statusbar/statusbar.go`, replace the `makePulse` function (around line 345):

```go
// Remove makePulse entirely and replace all calls with:
dyncolor.Pulse(base, 0.7, 1.0, 6)
```

Find all call sites of `makePulse(base)` and replace with `dyncolor.Pulse(base, 0.7, 1.0, 6)`.

Remove the `math` import if no longer needed.

- [ ] **Step 2: Set ClientSideAnimations on status bar UIManager**

In `cmd/texel-server/main.go`, after creating the status bar (around line 164-168), set the flag:

```go
statusApp := desktop.Registry().CreateApp("statusbar", nil)
if sb, ok := statusApp.(*statusbar.StatusBarApp); ok {
	sb.SetActions(desktop)
	sb.UI().ClientSideAnimations = true
}
```

Check if `StatusBarApp` exposes `UI()` — it returns `sb.ui` (the UIManager). If not exposed, add a getter or set it through `sb.app.UI()`.

- [ ] **Step 3: Run full tests and build**

Run: `cd /home/marc/projects/texel/texelation && make test && make build`
Expected: all pass, build clean.

- [ ] **Step 4: Manual verification**

Test:
1. Start texelation, enter tab nav mode (Shift+Up past top). Pulse should animate smoothly on the client side.
2. Check CPU usage — should be near 0% at idle even during pulsation.
3. Standalone apps using DynamicColor continue to work (UIManager drives animation locally).

- [ ] **Step 5: Commit**

```bash
git add apps/statusbar/statusbar.go cmd/texel-server/main.go
git commit -m "Migrate status bar to color.Pulse(), set ClientSideAnimations flag"
```

---

## Task Summary

| Task | Description | Depends On |
|------|-------------|------------|
| 1 | Easing table in texelui | — |
| 2 | DynamicColorDesc + Pulse/Fade constructors | 1 |
| 3 | Cell DynFG/DynBG fields | 2 |
| 4 | Painter stores descriptors | 2, 3 |
| 5 | UIManager ClientSideAnimations flag | — |
| 6 | Protocol StyleEntry extension | 3 |
| 7 | Publisher copies descriptors | 4, 6 |
| 8 | Client resolves dynamic cells | 6 |
| 9 | StatusBar migration + server flag | 2, 5, 7, 8 |

Tasks 1 and 5 are independent. Tasks 2, 3, 4 are sequential (texelui). Tasks 6, 7, 8 are sequential (texelation protocol/renderer). Task 9 ties everything together.
