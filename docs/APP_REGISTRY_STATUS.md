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
- No desktop-level floating panel support yet
- Panes are always part of the tree

### Proposed Architecture

```go
type DesktopEngine struct {
    // Existing
    workspaces []*Workspace

    // NEW: Floating overlays
    floatingPanels []*FloatingPanel
}

type FloatingPanel struct {
    app    App
    x, y   int
    width  int
    height int
    modal  bool  // Blocks input to underlying panes
}

func (d *DesktopEngine) ShowFloatingPanel(app App, x, y, w, h int) {
    panel := &FloatingPanel{
        app: app,
        x: x, y: y,
        width: w, height: h,
        modal: true,
    }
    d.floatingPanels = append(d.floatingPanels, panel)
}

func (d *DesktopEngine) CloseFloatingPanel(panel *FloatingPanel) {
    // Remove from slice
    // Return focus to underlying workspace
}
```

### Rendering Order

```
1. Render workspace tree → base buffer
2. Render effects → effect buffer
3. Render floating panels → overlay buffer
4. Composite all layers → final buffer
```

### Use Cases

- **Launcher**: Floating on Ctrl+A+L
- **Command palette**: Quick commands
- **Notifications**: Toast messages
- **Dialogs**: Confirmation prompts
- **Context menus**: Right-click actions

### Implementation Effort

- **Small**: 2-3 hours
- Mostly rendering pipeline changes
- Input routing (modal vs non-modal)
- Focus management

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

### Next ⏭️

#### Phase 5: Launcher Invocation
1. Add floating panel support (2-3 hours) - **OPTIONAL**
   - Or use existing pane replacement for now
2. Wire up Ctrl+A+L keybind to show launcher (15 min)
3. Test end-to-end workflow

### Total Remaining: ~15 min - 3 hours (depending on floating panel approach)

## 🎉 Current Status

**Phase 1-4 Complete!** The launcher is fully functional and can be launched as a regular app.
Users can navigate apps with arrow keys and press Enter to launch them.

**Next Step**: Add Ctrl+A+L keybind to make launcher easily accessible from any pane.

**Optional Enhancement**: Floating panel overlay for launcher (can be deferred)
