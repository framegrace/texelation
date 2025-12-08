# App Registry & Launcher - Implementation Status

## ✅ Completed: Registry Foundation

### What's Done

1. **App Registry Package** (`registry/`)
   - Manifest parsing and validation
   - Directory scanning for apps
   - Thread-safe app storage
   - Built-in app registration

2. **Wrapper App Support** ⭐
   - Apps can wrap built-ins with custom parameters
   - Example: `htop` = `texelterm` + `"htop"` command
   - No Go code needed - just manifest.json!
   - Custom wrapper factories for flexible app creation

3. **App Types**
   - `built-in`: Compiled into server
   - `wrapper`: Wraps built-in with args (PRIMARY USE CASE)
   - `external`: Standalone binary (future)

4. **Documentation**
   - Manifest format examples
   - Installation guide for users
   - htop, vim, btop, python examples

### Example: Adding htop

```bash
# User creates this:
~/.config/texelation/apps/htop/manifest.json
```

```json
{
  "name": "htop",
  "displayName": "System Monitor",
  "type": "wrapper",
  "wraps": "texelterm",
  "command": "htop",
  "icon": "📊",
  "category": "system"
}
```

```bash
# Reload apps
killall -HUP texel-server

# htop now appears in launcher!
```

## Current Wiring

- **Desktop integration**: `texel/desktop_engine_core.go` constructs a `registry.Registry`, registers the default texelterm factory, and scans `~/.config/texelation/apps` on startup and on `ForceRefresh`/SIGHUP.
- **Server built-ins**: `cmd/texel-server/main.go` registers the texelterm wrapper factory plus built-in `launcher`, `help`, and `flicker` apps. Snapshot restore factories are registered for texelterm.
- **Control bus**: `texel/pane.go` and `texel/desktop_engine_core.go` attach control handlers when an app exposes `ControlBusProvider`. Launcher uses `launcher.select-app` / `launcher.close` controls to replace the active pane or close itself.
- **Launcher app**: Lives in `apps/launcher/`, built with TexelUI widgets and covered by tests. Selecting an app fires control bus triggers; Escape closes via `launcher.close`.
- **Remaining gaps**: External app type is still a stub; manifest `config` is parsed but not passed through factories (see TODO in `registry.CreateApp`).

## 🔮 Floating Panels (For Launcher Overlay)

### Current State

- `texel/overlay.go` exists but only for buffer compositing
- ✅ Desktop-level floating panel support implemented
- ✅ Panes can be rendered as overlays on top of workspace

### Architecture Implemented

```go
type DesktopEngine struct {
    // ...
    floatingPanels []*FloatingPanel
}

type FloatingPanel struct {
    app    App
    x, y   int
    width  int
    height int
    modal  bool  // Blocks input to underlying panes
    id     [16]byte
}
```

### Rendering Order

```
1. Render workspace tree → base buffer
2. Render effects → effect buffer
3. Render floating panels → overlay buffer (DONE)
4. Composite all layers → final buffer
```

### Use Cases

- **Launcher**: Floating on Ctrl+A+L (Implemented)

## 📋 Summary

### Done ✅

#### Phase 1: Registry Foundation
- ✅ Registry package with wrapper support
- ✅ Manifest format and validation
- ✅ Example manifests and docs

#### Phase 2: Wire Registry to Desktop
- ✅ Registry integrated into DesktopEngine
- ✅ Built-in app registration (texelterm, launcher, help, flicker)
- ✅ Wrapper factory for texelterm
- ✅ App scanning from ~/.config/texelation/apps/
- ✅ SIGHUP reload support for apps

#### Phase 3: Control Bus Integration
- ✅ ControlBusProvider interface defined in texel/app.go
- ✅ Pipeline implements ControlBusProvider
- ✅ Desktop registers handlers on launcher's control bus
- ✅ Launcher signals events via control bus
- ✅ Consistent with Texelation's control bus pattern

#### Phase 4: Launcher App with TexelUI
- ✅ Launcher app implementation (apps/launcher/)
- ✅ TexelUI-based interface
- ✅ Keyboard navigation (Up/Down/Enter)
- ✅ Visual selection highlighting
- ✅ Comprehensive test suite (8 tests, all passing)
- ✅ Registered as built-in app "launcher"

#### Phase 5: Launcher Invocation & Floating Panels
- ✅ Floating panel support in DesktopEngine
- ✅ Input routing for modal panels
- ✅ Rendering pipeline update
- ✅ Ctrl+A+L keybinding
- ✅ Control bus handlers for launching apps into active pane

## 🎉 Current Status

**Phase 1-5 Complete with Control Bus Pattern!**
- Users can launch the launcher with `Ctrl+A L`.
- It appears as a floating modal overlay.
- Selecting an app launches it in the underlying active pane and closes the overlay.
- **Fully consistent with Texelation architecture**: Uses control bus pattern instead of direct injection.
- Apps signal events through their control bus, desktop listens and responds.
- No special-case interfaces or bidirectional dependencies.

External app launching and passing manifest `config` into factories remain open items.

**Next Step**: Wire manifest `config` through `registry.CreateApp` and implement the external app type.
