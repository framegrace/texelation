# EffectCard Migration Guide

## Overview

The `EffectCard` unifies visual effects by wrapping any registered effect from the `internal/effects` registry, eliminating the need for separate card implementations like `FlashCard` and `RainbowCard`.

## Available Effects

Registered effects in `internal/effects`:
- `"flash"` - Flash overlay effect
- `"rainbow"` - Rainbow tinting effect  
- `"fadeTint"` - Fade tint overlay effect

## Usage

### Before (Card-Specific)

```go
// Legacy snippet (cards removed)
subtle := tcell.NewRGBColor(160, 160, 160)
flash := cards.NewFlashCard(100*time.Millisecond, subtle)
rainbow := cards.NewRainbowCard(0.5, 0.6)

pipe := cards.NewPipeline(nil,
    cards.WrapApp(app),
    flash,
    rainbow,
)
```

### After (Unified EffectCard)

```go
import (
    "texelation/internal/effects"
    "texelation/texel/cards"
    "github.com/gdamore/tcell/v2"
)

flash, _ := cards.NewEffectCard("flash", effects.EffectConfig{
    "duration_ms":   100,
    "color":         "#A0A0A0",
    "max_intensity": 0.75,
})
rainbow, _ := cards.NewEffectCard("rainbow", effects.EffectConfig{
    "speed_hz": 0.5,
    "mix": 0.6,
})

pipe := cards.NewPipeline(nil,
    cards.WrapApp(app),
    flash,
    rainbow,
)
```

## Effect Configuration

Each effect has its own configuration parameters. Check the effect implementation for details:

### Flash Effect (`"flash"`)
```go
config := effects.EffectConfig{
    "duration_ms":   100,        // Flash duration in milliseconds
    "color":         "#FFFFFF", // Flash color (hex string)
    "max_intensity": 0.8,        // Optional: blend cap (0-1)
    "keys":          []rune{'F'},// Optional: keys that trigger flash
}
```

### Rainbow Effect (`"rainbow"`)
```go
config := effects.EffectConfig{
    "speed_hz": 0.5,  // Animation speed in Hz
    "mix": 0.6,       // Color mix intensity (0.0-1.0)
}
```

### FadeTint Effect (`"fadeTint"`)
```go
config := effects.EffectConfig{
    "color": tcell.Color,      // Tint color (default: dark blue)
    "intensity": 0.3,           // Tint intensity (0.0-1.0)
    "duration_ms": 200,        // Animation duration in milliseconds
}
```

## ControlBus Integration

`EffectCard` automatically implements `ControllableCard`, registering triggers as `"effects.<effectID>"`:

```go
// In your app:
bus := pipeline.ControlBus()
bus.Trigger("effects.flash", nil)
bus.Trigger("effects.rainbow", nil)
```

## Migration Path

1. **Replace specific card constructors** with `NewEffectCard()`
2. **Convert configuration** from constructor params to `EffectConfig` map
3. **Update imports** to include `texelation/internal/effects`
4. **Test** - effects should behave identically

## Deprecation

The following cards can be replaced with `EffectCard`:
- `FlashCard` → `EffectCard("flash", ...)`
- `RainbowCard` → `EffectCard("rainbow", ...)`

The legacy `FlashCard`/`RainbowCard` implementations have been removed; new code must use `EffectCard`.

## Adding New Effects

To add a new effect:
1. Implement `effects.Effect` interface in `internal/effects/`
2. Register it with `effects.Register(id, factory)` in `init()`
3. Use it via `NewEffectCard(id, config)` - no card code needed!
