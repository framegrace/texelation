# TDE Architecture Analysis & Refactoring Plan

## Current Architecture Analysis

### Strengths
- Clean separation between Desktop (window manager), Screen (workspace), Pane (window), and App (content)
- Sophisticated effects pipeline with animation system
- Event-driven architecture with pub/sub messaging
- Flexible tiling system with dynamic layout animations
- Comprehensive terminal emulator with VT parsing

### Current Bottlenecks & Inflexibilities

1. **Hard-coded App Interface**: Apps must implement specific methods (Run, Stop, Resize, Render, etc.) making it difficult to integrate existing programs
2. **Tightly Coupled Effects**: Effects are directly tied to Desktop/Screen instances, limiting reusability
3. **Single App Factory Pattern**: Limited to predefined app types (shell, welcome) in main.go:33-38
4. **Monolithic Desktop**: Desktop handles too many concerns (window management, effects, events, animation)
5. **Cell-based Rendering**: While efficient, limits integration with other rendering backends

## Proposed Architectural Improvements

### 1. Plugin Architecture for Apps
Create a plugin system allowing dynamic loading of apps:

```go
// Plugin interface for dynamic app loading
type AppPlugin interface {
    Name() string
    Create(config map[string]interface{}) (App, error)
    Metadata() PluginMetadata
}

type PluginMetadata struct {
    Version    string
    Author     string
    Category   string
    Capabilities []string
}
```

### 2. Adapter Pattern for External Programs
Add adapters to wrap existing programs without modification:

```go
// Adapter for command-line programs
type ProcessAdapter struct {
    command string
    args    []string
    pty     *os.File
    // implements App interface
}

// Adapter for GUI programs via terminal protocols
type GuiAdapter struct {
    // Handles sixel, iTerm2 images, etc.
}
```

### 3. Modular Rendering Pipeline
Decouple rendering from Cell-based system:

```go
type RenderBackend interface {
    Render(content RenderContent) error
    GetSize() (width, height int)
    HandleInput(input InputEvent) error
}

// Multiple backends: tcell, HTML canvas, WebGL, etc.
type TcellBackend struct{}
type WebBackend struct{}
```

### 4. Configuration-Driven Layout System
Replace hardcoded layout with declarative configuration:

```yaml
# ~/.config/tde/layout.yaml
workspaces:
  1:
    name: "Development"
    layout:
      type: "hsplit"
      ratio: [0.7, 0.3]
      children:
        - type: "app"
          plugin: "terminal"
          config: {shell: "/bin/zsh"}
        - type: "vsplit"
          children:
            - {plugin: "file-browser"}
            - {plugin: "system-monitor"}
```

### 5. Effect System Improvements
Make effects standalone and composable:

```go
// Registry for effect discovery
type EffectRegistry struct {
    effects map[string]EffectFactory
}

// Hot-reloadable effect plugins
type EffectPlugin interface {
    CreateEffect(config EffectConfig) Effect
    GetPresets() []EffectPreset
}
```

### 6. Service-Oriented Architecture
Break Desktop into focused services:

```go
type WindowManager interface {
    CreateWindow(app App) WindowID
    MoveWindow(id WindowID, direction Direction)
    ResizeWindow(id WindowID, size Size)
}

type EffectService interface {
    ApplyEffect(target Target, effect Effect, duration time.Duration)
    RemoveEffect(effectID string)
}

type EventService interface {
    Subscribe(eventType EventType, handler EventHandler)
    Publish(event Event)
}
```

### 7. Developer SDK
Create a comprehensive development kit:

```go
// High-level app creation
func CreateApp(name string) *AppBuilder {
    return &AppBuilder{name: name}
}

func (ab *AppBuilder) WithRenderer(renderer Renderer) *AppBuilder
func (ab *AppBuilder) WithInputHandler(handler InputHandler) *AppBuilder
func (ab *AppBuilder) WithEffects(effects ...Effect) *AppBuilder
func (ab *AppBuilder) Build() App
```

### 8. Hot-reload Development System
Add development-time features:

```go
type DevServer interface {
    WatchFiles(patterns []string)
    ReloadApp(appName string) error
    InjectEffect(effect Effect, target string) error
    EnableDebugOverlay() error
}
```

## Implementation Priority

1. **Plugin Architecture** - Most impactful for developer flexibility
2. **Configuration System** - Enables user customization without code changes  
3. **Effect Registry** - Makes effects reusable and discoverable
4. **Service Architecture** - Improves testability and modularity
5. **Multiple Backends** - Future-proofs for different environments

## Backwards Compatibility Strategy

Implement these changes incrementally:
1. Add interfaces alongside existing concrete types
2. Create adapter layer for current apps
3. Gradually migrate internal components
4. Deprecate old APIs only after full migration

## Key Files Referenced
- `main.go:33-38` - App factory pattern
- `texel/desktop.go` - Main desktop management
- `texel/screen.go` - Workspace management
- `texel/pane.go` - Window/pane management
- `texel/app.go` - App interface definition
- `texel/effects.go` - Effects pipeline
- `texel/tree.go` - Tiling compositor
- `apps/texelterm/term.go` - Terminal emulator implementation

---

**Analysis Date**: 2025-01-28
**Status**: Planning Phase - Ready for Implementation