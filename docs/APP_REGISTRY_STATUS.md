# App Registry & Launcher - Implementation Status

## ✅ Completed: Registry Foundation

### What's Done

1. **App Registry Package** (`texel/registry/`)
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

## 🚧 Next Steps

### Phase 2: Wire Registry to Desktop

1. **Add Registry to Desktop**
   ```go
   type DesktopEngine struct {
       // ...
       registry *registry.Registry
   }
   ```

2. **Register Built-in Apps**
   ```go
   registry.RegisterBuiltIn("texelterm", func() App {
       return texelterm.New("term", "/bin/bash")
   })

   registry.RegisterBuiltIn("welcome", func() App {
       return welcome.NewWelcomeApp()
   })
   ```

3. **Register TexelTerm Wrapper Factory**
   ```go
   registry.RegisterWrapperFactory("texelterm", func(m *Manifest) App {
       return texelterm.New(m.DisplayName, m.Command)
   })
   ```

4. **Scan Apps on Startup**
   ```go
   configDir := os.UserConfigDir()
   appsDir := filepath.Join(configDir, "texelation", "apps")
   registry.Scan(appsDir)
   ```

5. **Reload on SIGHUP**
   - Rescan apps directory
   - Like theme reload

### Phase 3: AppReplacer Interface

Add ability for apps to replace themselves (for launcher):

```go
// In texel/app.go
type AppReplacer interface {
    ReplaceWithApp(name string, config map[string]interface{})
}

// In texel/pane.go
func (p *pane) ReplaceWithApp(name string, config map[string]interface{}) {
    newApp := p.screen.desktop.registry.CreateApp(name, config)
    p.AttachApp(newApp, p.screen.refreshChan)
    p.screen.desktop.broadcastStateUpdate()
}

func (p *pane) AttachApp(app App, refreshChan chan<- bool) {
    // ... existing code ...

    // Give app ability to replace itself
    if replaceable, ok := app.(interface{ SetReplacer(AppReplacer) }); ok {
        replaceable.SetReplacer(p)
    }
}
```

### Phase 4: Launcher App (TexelUI)

Create `apps/launcher/` using TexelUI:

```go
type Launcher struct {
    registry *registry.Registry
    replacer texel.AppReplacer
    // ... UI state ...
}

func (l *Launcher) SetReplacer(r texel.AppReplacer) {
    l.replacer = r
}

func (l *Launcher) HandleKey(ev *tcell.EventKey) {
    if ev.Key() == tcell.KeyEnter {
        selected := l.selectedApp

        // Replace launcher with selected app
        l.replacer.ReplaceWithApp(selected, nil)
    }
}
```

### Phase 5: Launcher Invocation (Hybrid Mode)

**Default Shell**: Terminal
```go
shellFactory := func() texel.App {
    return texelterm.New("terminal", "/bin/bash")
}
```

**Ctrl+A+L**: Show launcher in current pane
```go
// In desktop key handler
if key == tcell.KeyRune && rune == 'l' {
    // Replace current pane's app with launcher
    currentPane.ReplaceWithApp("launcher", nil)
}
```

**Launcher Features**:
- Grid/list view of apps
- Category filtering
- Search/fuzzy find
- Icons and descriptions
- **Enter**: Replace with app
- **Ctrl+Enter**: Spawn in new split (future)

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
- ✅ Built-in app registration (texelterm, welcome)
- ✅ Wrapper factory for texelterm
- ✅ App scanning from ~/.config/texelation/apps/
- ✅ SIGHUP reload support for apps

#### Phase 3: AppReplacer Interface
- ✅ AppReplacer interface defined
- ✅ ReplacerReceiver interface for apps
- ✅ Pane implements ReplaceWithApp
- ✅ Automatic replacer injection in AttachApp

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
- ✅ FloatingLauncherReplacer for launching apps into active pane

## 🎉 Current Status

**Phase 1-5 Complete!**
- Users can launch the launcher with `Ctrl+A L`.
- It appears as a floating modal overlay.
- Selecting an app launches it in the underlying active pane and closes the overlay.

**Next Step**: Enjoy the new launcher experience!
