# Gradient Descriptor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make linear and radial gradients serializable across the texelation protocol so the client can reconstruct and resolve them locally, including animated stops (Pulse, Fade).

**Architecture:** Extend `DynamicColorDesc` with `DescTypeLinearGrad`/`DescTypeRadialGrad` types and a `Stops []GradientStopDesc` slice. `GradientBuilder.Build()` stores the descriptor alongside the closure. Protocol encodes gradient stops as variable-length data after the fixed 22-byte descriptor. Client reconstructs via `FromDesc()`.

**Tech Stack:** Go 1.24.3, texelui (color package), texelation (protocol, publisher, client renderer)

**Spec:** `docs/superpowers/specs/2026-03-30-gradient-descriptor-design.md`

---

## File Structure

### texelui changes

| File | Responsibility |
|------|---------------|
| `color/dynamic.go` (modify) | Add `GradientStopDesc`, `DescTypeLinearGrad`, `DescTypeRadialGrad`, update `IsAnimated()`, update `FromDesc()` |
| `color/dynamic_test.go` (modify) | Tests for gradient descriptors and round-trips |
| `color/gradient.go` (modify) | `Build()` stores desc with stops, add `withSource()` helper |

### texelation changes

| File | Responsibility |
|------|---------------|
| `protocol/buffer_delta.go` (modify) | Add `Stops` to `DynColorDesc`, encode/decode gradient stops |
| `protocol/buffer_delta_test.go` (modify) | Round-trip test for gradient styles |
| `internal/runtime/server/desktop_publisher.go` (modify) | Copy `Stops` slice in `convertCell` |
| `internal/runtime/client/renderer.go` (modify) | Copy `Stops` slice in `protocolDescToColor` |
| `client/buffercache.go` (modify) | Store `Stops` on client `DynColorDesc` alias via protocol type |
| `apps/statusbar/blend_info_line.go` (modify) | Revert split back to single gradient |

---

## Task 1: Add GradientStopDesc and New Types to texelui

**Files:**
- Modify: `/home/marc/projects/texel/texelui/color/dynamic.go`
- Modify: `/home/marc/projects/texel/texelui/color/dynamic_test.go`

### Steps

- [ ] **Step 1: Add GradientStopDesc type and new constants**

In `/home/marc/projects/texel/texelui/color/dynamic.go`, after the existing `DescTypeFade` constant, add:

```go
const (
	DescTypeNone       uint8 = 0
	DescTypeSolid      uint8 = 1
	DescTypePulse      uint8 = 2
	DescTypeFade       uint8 = 3
	DescTypeLinearGrad uint8 = 4
	DescTypeRadialGrad uint8 = 5
)

// GradientStopDesc describes a single stop in a gradient descriptor.
type GradientStopDesc struct {
	Position float32
	Color    DynamicColorDesc // Solid, Pulse, or Fade — no nested gradients
}
```

Add `Stops` field to `DynamicColorDesc`:

```go
type DynamicColorDesc struct {
	Type   uint8
	Base   uint32
	Target uint32
	Easing uint8
	Speed  float32
	Min    float32
	Max    float32
	Stops  []GradientStopDesc // nil for non-gradient types
}
```

- [ ] **Step 2: Update IsAnimated**

Replace the existing `IsAnimated` method on `DynamicColorDesc`:

```go
func (d DynamicColorDesc) IsAnimated() bool {
	if d.Type >= DescTypePulse && d.Type <= DescTypeFade {
		return true
	}
	for _, s := range d.Stops {
		if s.Color.IsAnimated() {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Add IsGradient helper**

```go
// IsGradient returns true if this descriptor represents a gradient.
func (d DynamicColorDesc) IsGradient() bool {
	return d.Type == DescTypeLinearGrad || d.Type == DescTypeRadialGrad
}

// IsDynamic returns true if this descriptor is non-trivial (not None or Solid).
func (d DynamicColorDesc) IsDynamic() bool {
	return d.Type >= DescTypePulse
}
```

- [ ] **Step 4: Run build**

Run: `cd /home/marc/projects/texel/texelui && go build ./color/`
Expected: builds clean.

- [ ] **Step 5: Commit**

```bash
cd /home/marc/projects/texel/texelui
git add color/dynamic.go
git commit -m "Add GradientStopDesc, DescTypeLinearGrad/RadialGrad to DynamicColorDesc"
```

---

## Task 2: Update GradientBuilder.Build() to Store Descriptors

**Files:**
- Modify: `/home/marc/projects/texel/texelui/color/gradient.go`

### Steps

- [ ] **Step 1: Add withSource helper**

In `/home/marc/projects/texel/texelui/color/gradient.go`, add after the `WithPane` method:

```go
// withSource sets the coordinate source directly (used by FromDesc reconstruction).
func (gb GradientBuilder) withSource(s coordSource) GradientBuilder {
	gb.source = s
	return gb
}
```

- [ ] **Step 2: Build gradient descriptor in buildDesc helper**

Add a helper that converts a `GradientBuilder` into a `DynamicColorDesc`:

```go
func (gb GradientBuilder) buildDesc() DynamicColorDesc {
	stops := make([]GradientStopDesc, len(gb.stops))
	for i, s := range gb.stops {
		if !s.Dynamic.IsZero() {
			stops[i] = GradientStopDesc{Position: s.Position, Color: s.Dynamic.Describe()}
		} else {
			r, g, b := s.Color.RGB()
			stops[i] = GradientStopDesc{
				Position: s.Position,
				Color:    DynamicColorDesc{Type: DescTypeSolid, Base: PackRGB(r, g, b)},
			}
		}
	}

	desc := DynamicColorDesc{Stops: stops, Easing: uint8(gb.source)}
	if gb.linear {
		desc.Type = DescTypeLinearGrad
		desc.Base = math.Float32bits(gb.angleDeg)
	} else {
		desc.Type = DescTypeRadialGrad
		desc.Speed = gb.cx
		desc.Target = math.Float32bits(gb.cy)
	}
	return desc
}
```

- [ ] **Step 3: Update Build() to store the descriptor**

Replace the `Build()` method. The key change: after creating the `Func` closure (unchanged), set `desc` on the returned `DynamicColor`. Also, if any stop has a DynamicColor, resolve stops at call time in the closure:

```go
func (gb GradientBuilder) Build() DynamicColor {
	source := gb.source
	desc := gb.buildDesc()

	// Check if any stop is dynamic (needs per-frame resolution).
	hasDyn := false
	for _, s := range gb.stops {
		if !s.Dynamic.IsZero() && !s.Dynamic.IsStatic() {
			hasDyn = true
			break
		}
	}

	if len(gb.stops) == 0 {
		return Solid(tcell.NewRGBColor(0, 0, 0))
	}
	if len(gb.stops) == 1 {
		if !gb.stops[0].Dynamic.IsZero() {
			dc := gb.stops[0].Dynamic
			dc.desc = desc
			return dc
		}
		s := Solid(gb.stops[0].Color)
		s.desc = desc
		return s
	}

	// Capture stops for the closure.
	capturedStops := gb.stops

	var fn ColorFunc
	if gb.linear {
		angleDeg := gb.angleDeg
		if hasDyn {
			fn = func(ctx ColorContext) tcell.Color {
				resolved := resolveStops(capturedStops, ctx)
				nx, ny := normalizedCoords(ctx, source)
				rad := float64(angleDeg) * math.Pi / 180.0
				t := nx*math.Cos(rad) + ny*math.Sin(rad)
				t = clampFloat(t, 0, 1)
				return interpolateStops(resolved, t)
			}
		} else {
			static := prepareStops(capturedStops)
			fn = func(ctx ColorContext) tcell.Color {
				nx, ny := normalizedCoords(ctx, source)
				rad := float64(angleDeg) * math.Pi / 180.0
				t := nx*math.Cos(rad) + ny*math.Sin(rad)
				t = clampFloat(t, 0, 1)
				return interpolateStops(static, t)
			}
		}
	} else {
		cx, cy := gb.cx, gb.cy
		if hasDyn {
			fn = func(ctx ColorContext) tcell.Color {
				resolved := resolveStops(capturedStops, ctx)
				nx, ny := normalizedCoords(ctx, source)
				dx := nx - float64(cx)
				dy := ny - float64(cy)
				t := math.Sqrt(dx*dx+dy*dy) * 2
				t = clampFloat(t, 0, 1)
				return interpolateStops(resolved, t)
			}
		} else {
			static := prepareStops(capturedStops)
			fn = func(ctx ColorContext) tcell.Color {
				nx, ny := normalizedCoords(ctx, source)
				dx := nx - float64(cx)
				dy := ny - float64(cy)
				t := math.Sqrt(dx*dx+dy*dy) * 2
				t = clampFloat(t, 0, 1)
				return interpolateStops(static, t)
			}
		}
	}

	return DynamicColor{
		fn:       fn,
		animated: hasDyn,
		desc:     desc,
	}
}
```

- [ ] **Step 4: Run existing tests**

Run: `cd /home/marc/projects/texel/texelui && go test ./color/ -v`
Expected: all existing tests pass (gradient behavior unchanged).

- [ ] **Step 5: Commit**

```bash
git add color/gradient.go
git commit -m "GradientBuilder.Build() stores descriptor alongside closure"
```

---

## Task 3: Update FromDesc for Gradient Types

**Files:**
- Modify: `/home/marc/projects/texel/texelui/color/dynamic.go`
- Modify: `/home/marc/projects/texel/texelui/color/dynamic_test.go`

### Steps

- [ ] **Step 1: Write tests for gradient descriptor round-trips**

Append to `/home/marc/projects/texel/texelui/color/dynamic_test.go`:

```go
func TestDynamicColorDesc_LinearGradient(t *testing.T) {
	grad := Linear(0, Stop(0, tcell.NewRGBColor(255, 0, 0)), Stop(1, tcell.NewRGBColor(0, 0, 255))).WithLocal().Build()
	desc := grad.Describe()
	if desc.Type != DescTypeLinearGrad {
		t.Fatalf("expected DescTypeLinearGrad, got %d", desc.Type)
	}
	if len(desc.Stops) != 2 {
		t.Fatalf("expected 2 stops, got %d", len(desc.Stops))
	}
	if desc.Stops[0].Color.Type != DescTypeSolid {
		t.Errorf("stop 0 should be solid, got %d", desc.Stops[0].Color.Type)
	}
}

func TestDynamicColorDesc_LinearGradientRoundTrip(t *testing.T) {
	original := Linear(45, Stop(0, tcell.NewRGBColor(255, 0, 0)), Stop(1, tcell.NewRGBColor(0, 0, 255))).WithLocal().Build()
	desc := original.Describe()
	reconstructed := FromDesc(desc)

	ctx := ColorContext{X: 5, Y: 0, W: 10, H: 1}
	origColor := original.Resolve(ctx)
	reconColor := reconstructed.Resolve(ctx)
	if origColor != reconColor {
		t.Errorf("round-trip mismatch at X=5: original=%v reconstructed=%v", origColor, reconColor)
	}
}

func TestDynamicColorDesc_GradientWithPulseStop(t *testing.T) {
	pulse := Pulse(tcell.NewRGBColor(137, 180, 250), 0.7, 1.0, 6)
	grad := Linear(0, DynStop(0, pulse), Stop(1, tcell.NewRGBColor(30, 30, 46))).WithLocal().Build()
	desc := grad.Describe()
	if desc.Type != DescTypeLinearGrad {
		t.Fatalf("expected DescTypeLinearGrad, got %d", desc.Type)
	}
	if desc.Stops[0].Color.Type != DescTypePulse {
		t.Errorf("stop 0 should be pulse, got %d", desc.Stops[0].Color.Type)
	}
	if !desc.IsAnimated() {
		t.Error("gradient with Pulse stop should be animated")
	}

	// Round-trip
	reconstructed := FromDesc(desc)
	ctx := ColorContext{X: 0, Y: 0, W: 10, H: 1, T: 0.5}
	origColor := grad.Resolve(ctx)
	reconColor := reconstructed.Resolve(ctx)
	if origColor != reconColor {
		t.Errorf("round-trip mismatch: original=%v reconstructed=%v", origColor, reconColor)
	}
}

func TestDynamicColorDesc_RadialGradientRoundTrip(t *testing.T) {
	original := Radial(0.5, 0.5, Stop(0, tcell.NewRGBColor(255, 255, 200)), Stop(1, tcell.NewRGBColor(20, 20, 80))).WithLocal().Build()
	desc := original.Describe()
	if desc.Type != DescTypeRadialGrad {
		t.Fatalf("expected DescTypeRadialGrad, got %d", desc.Type)
	}
	reconstructed := FromDesc(desc)

	ctx := ColorContext{X: 5, Y: 5, W: 10, H: 10}
	origColor := original.Resolve(ctx)
	reconColor := reconstructed.Resolve(ctx)
	if origColor != reconColor {
		t.Errorf("round-trip mismatch: original=%v reconstructed=%v", origColor, reconColor)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelui && go test ./color/ -run TestDynamicColorDesc_Linear -v`
Expected: FAIL — `FromDesc` doesn't handle gradient types yet.

- [ ] **Step 3: Add FromDesc cases for gradients**

In `/home/marc/projects/texel/texelui/color/dynamic.go`, add to the `FromDesc` switch:

```go
case DescTypeLinearGrad:
	angleDeg := math.Float32frombits(d.Base)
	source := coordSource(d.Easing)
	stops := make([]ColorStop, len(d.Stops))
	for i, sd := range d.Stops {
		stops[i] = ColorStop{
			Position: sd.Position,
			Dynamic:  FromDesc(sd.Color),
		}
	}
	return Linear(angleDeg, stops...).withSource(source).Build()

case DescTypeRadialGrad:
	cx := d.Speed
	cy := math.Float32frombits(d.Target)
	source := coordSource(d.Easing)
	stops := make([]ColorStop, len(d.Stops))
	for i, sd := range d.Stops {
		stops[i] = ColorStop{
			Position: sd.Position,
			Dynamic:  FromDesc(sd.Color),
		}
	}
	return Radial(cx, cy, stops...).withSource(source).Build()
```

Add `"math"` to imports if not already present (it is).

- [ ] **Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelui && go test ./color/ -v`
Expected: all tests pass.

- [ ] **Step 5: Run full texelui test suite**

Run: `cd /home/marc/projects/texel/texelui && go test ./...`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add color/dynamic.go color/dynamic_test.go
git commit -m "FromDesc reconstructs linear and radial gradients with animated stops"
```

---

## Task 4: Extend Protocol Encoding/Decoding for Gradient Stops

**Files:**
- Modify: `/home/marc/projects/texel/texelation/protocol/buffer_delta.go`
- Modify: `/home/marc/projects/texel/texelation/protocol/buffer_delta_test.go`

### Steps

- [ ] **Step 1: Add Stops field to protocol DynColorDesc**

In `/home/marc/projects/texel/texelation/protocol/buffer_delta.go`, add `Stops` to `DynColorDesc` and the stop type:

```go
type DynColorStopDesc struct {
	Position float32
	Color    DynColorDesc // always Solid/Pulse/Fade, no nested gradients
}

type DynColorDesc struct {
	Type   uint8
	Base   uint32
	Target uint32
	Easing uint8
	Speed  float32
	Min    float32
	Max    float32
	Stops  []DynColorStopDesc // nil for non-gradient types
}
```

- [ ] **Step 2: Update EncodeBufferDelta**

After the existing encode loop that writes each `DynColorDesc`'s fixed 22 bytes, add gradient stop encoding. Replace the encode block inside `if style.AttrFlags&AttrHasDynamic != 0`:

```go
if style.AttrFlags&AttrHasDynamic != 0 {
	for _, d := range [2]DynColorDesc{style.DynFG, style.DynBG} {
		buf.WriteByte(d.Type)
		binary.Write(buf, binary.LittleEndian, d.Base)
		binary.Write(buf, binary.LittleEndian, d.Target)
		buf.WriteByte(d.Easing)
		binary.Write(buf, binary.LittleEndian, d.Speed)
		binary.Write(buf, binary.LittleEndian, d.Min)
		binary.Write(buf, binary.LittleEndian, d.Max)
		// Gradient stops (variable length, only for Type >= 4)
		if d.Type >= 4 {
			buf.WriteByte(uint8(len(d.Stops)))
			for _, s := range d.Stops {
				binary.Write(buf, binary.LittleEndian, s.Position)
				buf.WriteByte(s.Color.Type)
				binary.Write(buf, binary.LittleEndian, s.Color.Base)
				binary.Write(buf, binary.LittleEndian, s.Color.Target)
				buf.WriteByte(s.Color.Easing)
				binary.Write(buf, binary.LittleEndian, s.Color.Speed)
				binary.Write(buf, binary.LittleEndian, s.Color.Min)
				binary.Write(buf, binary.LittleEndian, s.Color.Max)
			}
		}
	}
}
```

- [ ] **Step 3: Update DecodeBufferDelta**

After reading each `DynColorDesc`'s fixed 22 bytes, decode gradient stops. Replace the decode block inside `if delta.Styles[i].AttrFlags&AttrHasDynamic != 0`:

```go
if delta.Styles[i].AttrFlags&AttrHasDynamic != 0 {
	if len(b) < 44 {
		return delta, ErrPayloadShort
	}
	for _, d := range [2]*DynColorDesc{&delta.Styles[i].DynFG, &delta.Styles[i].DynBG} {
		d.Type = b[0]
		d.Base = binary.LittleEndian.Uint32(b[1:5])
		d.Target = binary.LittleEndian.Uint32(b[5:9])
		d.Easing = b[9]
		d.Speed = math.Float32frombits(binary.LittleEndian.Uint32(b[10:14]))
		d.Min = math.Float32frombits(binary.LittleEndian.Uint32(b[14:18]))
		d.Max = math.Float32frombits(binary.LittleEndian.Uint32(b[18:22]))
		b = b[22:]
		// Gradient stops
		if d.Type >= 4 {
			if len(b) < 1 {
				return delta, ErrPayloadShort
			}
			stopCount := int(b[0])
			b = b[1:]
			if len(b) < stopCount*26 { // 4 (position) + 22 (DynColorDesc) per stop
				return delta, ErrPayloadShort
			}
			d.Stops = make([]DynColorStopDesc, stopCount)
			for j := 0; j < stopCount; j++ {
				d.Stops[j].Position = math.Float32frombits(binary.LittleEndian.Uint32(b[:4]))
				b = b[4:]
				d.Stops[j].Color.Type = b[0]
				d.Stops[j].Color.Base = binary.LittleEndian.Uint32(b[1:5])
				d.Stops[j].Color.Target = binary.LittleEndian.Uint32(b[5:9])
				d.Stops[j].Color.Easing = b[9]
				d.Stops[j].Color.Speed = math.Float32frombits(binary.LittleEndian.Uint32(b[10:14]))
				d.Stops[j].Color.Min = math.Float32frombits(binary.LittleEndian.Uint32(b[14:18]))
				d.Stops[j].Color.Max = math.Float32frombits(binary.LittleEndian.Uint32(b[18:22]))
				b = b[22:]
			}
		}
	}
}
```

Note: the outer `if len(b) < 44` check is a minimum — gradient types may need more. The per-desc loop handles the exact size after reading the fixed 22 bytes.

- [ ] **Step 4: Write round-trip test**

Append to `/home/marc/projects/texel/texelation/protocol/buffer_delta_test.go`:

```go
func TestBufferDeltaGradientRoundTrip(t *testing.T) {
	delta := BufferDelta{
		PaneID:   [16]byte{1},
		Revision: 1,
		Styles: []StyleEntry{
			{
				AttrFlags: AttrHasDynamic,
				FgModel:   ColorModelRGB,
				FgValue:   0xFFFFFF,
				BgModel:   ColorModelRGB,
				BgValue:   0x000000,
				DynBG: DynColorDesc{
					Type:   4, // linear gradient
					Base:   0, // angle 0
					Easing: 2, // local coords
					Stops: []DynColorStopDesc{
						{Position: 0, Color: DynColorDesc{Type: 2, Base: 0x89B4FA, Speed: 6, Min: 0.7, Max: 1.0}}, // Pulse
						{Position: 0.3, Color: DynColorDesc{Type: 2, Base: 0x89B4FA, Speed: 6, Min: 0.7, Max: 1.0}},
						{Position: 1.0, Color: DynColorDesc{Type: 1, Base: 0x1E1E2E}}, // Solid
					},
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
	bg := decoded.Styles[0].DynBG
	if bg.Type != 4 {
		t.Fatalf("expected linear gradient type 4, got %d", bg.Type)
	}
	if len(bg.Stops) != 3 {
		t.Fatalf("expected 3 stops, got %d", len(bg.Stops))
	}
	if bg.Stops[0].Color.Type != 2 {
		t.Errorf("stop 0 should be pulse (2), got %d", bg.Stops[0].Color.Type)
	}
	if bg.Stops[0].Color.Speed != 6 {
		t.Errorf("stop 0 speed mismatch: %f", bg.Stops[0].Color.Speed)
	}
	if bg.Stops[2].Color.Type != 1 {
		t.Errorf("stop 2 should be solid (1), got %d", bg.Stops[2].Color.Type)
	}
	if bg.Stops[2].Color.Base != 0x1E1E2E {
		t.Errorf("stop 2 base mismatch: %x", bg.Stops[2].Color.Base)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./protocol/ -v`
Expected: all pass including the new gradient test.

- [ ] **Step 6: Commit**

```bash
git add protocol/buffer_delta.go protocol/buffer_delta_test.go
git commit -m "Protocol encode/decode for gradient DynColorDesc with variable-length stops"
```

---

## Task 5: Update Publisher and Client to Copy Gradient Stops

**Files:**
- Modify: `/home/marc/projects/texel/texelation/internal/runtime/server/desktop_publisher.go`
- Modify: `/home/marc/projects/texel/texelation/internal/runtime/client/renderer.go`

### Steps

- [ ] **Step 1: Update publisher convertCell to copy Stops**

In `/home/marc/projects/texel/texelation/internal/runtime/server/desktop_publisher.go`, the `convertCell` function currently checks `cell.DynFG.IsAnimated() || cell.DynBG.IsAnimated()`. Gradients with all-static stops are not "animated" but still need serialization. Change the condition and copy Stops:

```go
if cell.DynFG.IsDynamic() || cell.DynBG.IsDynamic() {
	entry.AttrFlags |= protocol.AttrHasDynamic
	key.attrFlags |= protocol.AttrHasDynamic
	entry.DynFG = convertDynDesc(cell.DynFG)
	entry.DynBG = convertDynDesc(cell.DynBG)
	key.dynFGType = cell.DynFG.Type
	key.dynBGType = cell.DynBG.Type
}
```

Add the helper:

```go
func convertDynDesc(d color.DynamicColorDesc) protocol.DynColorDesc {
	pd := protocol.DynColorDesc{
		Type: d.Type, Base: d.Base, Target: d.Target,
		Easing: d.Easing, Speed: d.Speed, Min: d.Min, Max: d.Max,
	}
	if len(d.Stops) > 0 {
		pd.Stops = make([]protocol.DynColorStopDesc, len(d.Stops))
		for i, s := range d.Stops {
			pd.Stops[i] = protocol.DynColorStopDesc{
				Position: s.Position,
				Color: protocol.DynColorDesc{
					Type: s.Color.Type, Base: s.Color.Base, Target: s.Color.Target,
					Easing: s.Color.Easing, Speed: s.Color.Speed, Min: s.Color.Min, Max: s.Color.Max,
				},
			}
		}
	}
	return pd
}
```

Also update the Painter's descriptor storage condition in `/home/marc/projects/texel/texelui/core/painter.go` — change `fgDesc.IsAnimated()` to `fgDesc.IsDynamic()` (and same for BG) so gradient descriptors are stored on cells even when all stops are static:

```go
if fgDesc := ds.FG.Describe(); fgDesc.IsDynamic() {
	cell.DynFG = fgDesc
}
if bgDesc := ds.BG.Describe(); bgDesc.IsDynamic() {
	cell.DynBG = bgDesc
}
```

- [ ] **Step 2: Update client protocolDescToColor to copy Stops**

In `/home/marc/projects/texel/texelation/internal/runtime/client/renderer.go`, replace `protocolDescToColor`:

```go
func protocolDescToColor(d protocol.DynColorDesc) color.DynamicColorDesc {
	desc := color.DynamicColorDesc{
		Type:   d.Type,
		Base:   d.Base,
		Target: d.Target,
		Easing: d.Easing,
		Speed:  d.Speed,
		Min:    d.Min,
		Max:    d.Max,
	}
	if len(d.Stops) > 0 {
		desc.Stops = make([]color.GradientStopDesc, len(d.Stops))
		for i, s := range d.Stops {
			desc.Stops[i] = color.GradientStopDesc{
				Position: s.Position,
				Color: color.DynamicColorDesc{
					Type: s.Color.Type, Base: s.Color.Base, Target: s.Color.Target,
					Easing: s.Color.Easing, Speed: s.Color.Speed, Min: s.Color.Min, Max: s.Color.Max,
				},
			}
		}
	}
	return desc
}
```

- [ ] **Step 3: Update client compositeInto to handle gradient types**

In the compositeInto function, the existing check `cell.DynBG.Type >= 2` already covers gradient types (4, 5 >= 2). No change needed — `FromDesc` handles reconstruction and `Resolve` handles resolution. But verify the `IsDynamic` threshold covers gradients: `Type >= 2` includes 4 and 5. ✓

- [ ] **Step 4: Run all tests**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add internal/runtime/server/desktop_publisher.go internal/runtime/client/renderer.go
cd /home/marc/projects/texel/texelui
git add core/painter.go
git commit -m "Publisher and client copy gradient Stops through protocol"
```

---

## Task 6: Revert BlendInfoLine Split and Test End-to-End

**Files:**
- Modify: `/home/marc/projects/texel/texelation/apps/statusbar/blend_info_line.go`

### Steps

- [ ] **Step 1: Revert BlendInfoLine to original single-gradient code**

In `/home/marc/projects/texel/texelation/apps/statusbar/blend_info_line.go`, revert the Draw method's gradient section back to the original single-gradient approach, but using `DynStop` for the accent so the Pulse descriptor flows into the gradient:

```go
	// Resolve the dynamic accent color for this frame.
	ctx := color.ColorContext{X: x, Y: y, W: w, H: 1, T: painter.Time()}
	resolvedAccent := accent.Resolve(ctx)
	if !accent.IsStatic() {
		painter.MarkAnimated()
	}

	// Choose the accent color for the gradient.
	gradAccent := accent
	if toastActive {
		switch toastSev {
		case texel.ToastSuccess:
			gradAccent = color.Solid(tm.GetSemanticColor("action.success"))
		case texel.ToastWarning:
			gradAccent = color.Solid(tm.GetSemanticColor("action.warning"))
		case texel.ToastError:
			gradAccent = color.Solid(tm.GetSemanticColor("action.danger"))
		}
	}

	// Build gradient: accent → accent (30%) → contentBG.
	// Uses DynStop so animated accent (Pulse) descriptors propagate
	// through the protocol for client-side resolution.
	grad := color.Linear(
		0,
		color.DynStop(0, gradAccent),
		color.DynStop(0.3, gradAccent),
		color.Stop(1, contentBG),
	).WithLocal().Build()

	bgStyle := color.DynamicStyle{FG: color.Solid(tcell.ColorDefault), BG: grad}

	// Fill the row with the gradient background.
	painter.SetWidgetRect(bil.Rect)
	for col := x; col < x+w; col++ {
		painter.SetDynamicCell(col, y, ' ', bgStyle)
	}

	// Resolve text colors.
	darkFG := tm.GetSemanticColor("text.inverse")
```

Note: the text drawing code below still uses `resolvedAccent` for the right-side accent text — keep that line.

- [ ] **Step 2: Run full test suite**

Run: `cd /home/marc/projects/texel/texelation && make test && make build`
Expected: all pass, build clean.

- [ ] **Step 3: Manual verification**

Test: Start `./bin/texelation`, enter tab nav mode (Shift+Up past top).
1. Tab label should pulse smoothly
2. Blend line should pulse in sync — including the fade portion
3. No jitter or desynchronization
4. CPU near 0% when idle (no server-side 60fps deltas)
5. Exiting nav mode restores static colors

- [ ] **Step 4: Commit**

```bash
git add apps/statusbar/blend_info_line.go
git commit -m "Revert BlendInfoLine to single gradient with DynStop for animated accent"
```

---

## Task Summary

| Task | Description | Depends On |
|------|-------------|------------|
| 1 | GradientStopDesc + new desc types in texelui | — |
| 2 | GradientBuilder.Build() stores descriptors | 1 |
| 3 | FromDesc reconstructs gradients + tests | 1, 2 |
| 4 | Protocol encode/decode for gradient stops | 1 |
| 5 | Publisher + client copy gradient stops | 1, 2, 3, 4 |
| 6 | Revert BlendInfoLine, end-to-end test | 5 |
