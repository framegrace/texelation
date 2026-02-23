# TexelBrowse — Semantic Terminal Browser

**Date:** 2026-02-22
**Status:** Approved

## Overview

A semantic/agentic browser for texelation that drives a real Chromium instance via CDP (chromedp) and presents web content through the Accessibility Tree, mapped to native texelui widgets. No pixel rendering — just structured, interactive content.

## Goals

1. **OIDC login flows** — Navigate auth URLs, fill credentials, handle redirects, persist sessions (e.g., `gcloud auth login`)
2. **Readable content** — Clean rendering of documentation, articles, search results with headings, paragraphs, links, code blocks
3. **Interactive JS apps** — SPAs work transparently since Chromium executes all JS; we observe the resulting AX tree

## Architecture

```
┌─────────────────────────────────────────────────┐
│  texelbrowse app  (texelui widgets + App iface) │
│  ┌─────────────┐  ┌──────────────────────────┐  │
│  │ Widget Tree  │  │ Adaptive Layout Manager  │  │
│  │ (from AX)   │  │ (reading / form / manual)│  │
│  └──────┬──────┘  └──────────┬───────────────┘  │
│         │                    │                   │
│  ┌──────┴────────────────────┴───────────────┐  │
│  │        Document Model (intermediate)       │  │
│  │  nodes: TextBlock | Link | Input | Button  │  │
│  │         | Image | Heading | List | Table   │  │
│  │  each node has stable ID + AX node ref     │  │
│  └──────────────────┬────────────────────────┘  │
│                     │                            │
│  ┌──────────────────┴────────────────────────┐  │
│  │          Browser Engine Layer              │  │
│  │  chromedp context, AX tree fetching,       │  │
│  │  CDP input dispatch, navigation, profiles  │  │
│  └───────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┘
         │
         ▼
   Chromium (headless, child process)
   --user-data-dir=~/.texelation/browser/profiles/default
```

### Data Flow

1. User enters URL or clicks a link widget
2. Browser engine navigates via CDP
3. On page settled (DOMContentLoaded + NetworkIdle), fetch AX tree snapshot
4. AX tree → Document Model (intermediate representation with stable IDs)
5. Document Model → texelui widget tree
6. Adaptive layout arranges widgets (reading or form mode)
7. `Render()` returns cell buffer

### User Interaction Flow

1. User tabs to a widget, types or presses Enter
2. Widget event handler calls browser engine layer
3. Engine dispatches CDP action (click, type, focus)
4. Page mutation detected via CDP events → incremental AX tree update → partial widget rebuild
5. Focus and scroll position preserved via stable node IDs

## AX Node to Widget Mapping

| AX Role | Widget | Behavior |
|---------|--------|----------|
| `heading` (1-6) | Styled text block | Bold, level prefix: `# H1`, `## H2` |
| `paragraph` / `text` | Text block | Word-wrapped plain text |
| `link` | Link widget (new) | Underlined text. Enter → CDP click → navigate |
| `button` | `widgets.Button` | Enter → CDP click |
| `textbox` / `searchbox` | `widgets.Input` | Keystrokes forwarded via CDP |
| `checkbox` | Checkbox widget (new) | Space toggles via CDP click |
| `radio` | Radio widget (new) | Space selects, grouped by `radiogroup` |
| `combobox` / `listbox` | `widgets.ComboBox` | Options from AX children |
| `tab` / `tablist` | Tab bar widget | Arrow keys switch tabs |
| `img` | Image widget (new) | Block art MVP, alt-text fallback |
| `list` / `listitem` | Indented text with bullet/number | Nested lists increase indent |
| `table` / `row` / `cell` | Table widget (new) | Column-aligned text grid |
| `separator` | Horizontal rule | `─────` line |
| `navigation` / `banner` / `contentinfo` | Collapsible region | Named, foldable |
| `alert` / `status` | Highlighted text block | Distinct background |
| Unknown roles | Generic text block | Fallback: name/value as plain text |

### New Widgets for texelui

- `Link` — clickable styled text with callback
- `Checkbox` / `Radio` — toggle inputs
- `Image` — terminal image rendering (block art MVP; Kitty/Sixel later)
- `Table` — column-aligned grid

## Adaptive Layout

### Mode Detection

Count interactive vs text nodes in AX tree:
- Interactive nodes (inputs, buttons, checkboxes) > 30% → **form mode**
- Otherwise → **reading mode**
- Manual toggle via Ctrl+M

### Reading Mode

Single scrollable column. Links inline. Tab/Shift-Tab cycles interactive elements. Optimized for articles, docs, search results.

```
┌─ URL bar ──────────────────────────────────────┐
│ https://docs.go.dev/effective-go               │
├────────────────────────────────────────────────┤
│  # Effective Go                                │
│                                                │
│  ## Introduction                               │
│  Go is a new language. Although it borrows...  │
│                                                │
│  [Formatting]  [Commentary]  [Names]           │
├─ status ───────────────────────────────────────┤
│ Reading mode | 42 links | Tab: next element    │
└────────────────────────────────────────────────┘
```

### Form Mode

Form elements rendered as full texelui widgets. Labels and instructions as text around them.

```
┌─ URL bar ──────────────────────────────────────┐
│ https://accounts.google.com/signin             │
├────────────────────────────────────────────────┤
│  Sign in — Use your Google Account             │
│                                                │
│  ┌─ Email or phone ─────────────────────────┐  │
│  │                                          │  │
│  └──────────────────────────────────────────┘  │
│  [ Forgot email? ]                             │
│  ┌──────────────┐                              │
│  │   Next   →   │                              │
│  └──────────────┘                              │
├─ status ───────────────────────────────────────┤
│ Form mode | 3 fields | Tab: next field         │
└────────────────────────────────────────────────┘
```

## Browser Engine Layer

### Chromium Management

- One Chromium process per app instance (child process, killed on Stop())
- `chromedp.NewExecAllocator()` with persistent `--user-data-dir`
- Headless mode, no GPU
- Each browser pane gets its own CDP Target (tab) within the same process

### Engine API

```go
type Engine struct {
    allocCtx   context.Context
    profileDir string
    mu         sync.Mutex
}

type Tab struct {
    ctx        context.Context
    engine     *Engine
    onNavigate func(url, title string)
    onAXUpdate func(snapshot *AXSnapshot)
    onLoading  func(state LoadingState)
}

// Navigation
func (t *Tab) Navigate(url string) error
func (t *Tab) Back() error
func (t *Tab) Forward() error
func (t *Tab) Reload() error

// AX tree
func (t *Tab) FetchAXTree() (*AXSnapshot, error)

// Input dispatch
func (t *Tab) Click(axNodeID string) error
func (t *Tab) Focus(axNodeID string) error
func (t *Tab) TypeText(axNodeID string, text string) error
func (t *Tab) SetValue(axNodeID string, value string) error
func (t *Tab) KeyPress(key string, modifiers int) error

// Resources
func (t *Tab) FetchImageBytes(url string) ([]byte, error)
```

### CDP Events

| CDP Event | Action |
|-----------|--------|
| `page.loadEventFired` | Update loading state, re-fetch AX tree |
| `page.navigatedWithinDocument` | SPA navigation — re-fetch AX tree |
| `page.frameNavigated` | Full navigation — show loading, re-fetch |
| `dom.documentUpdated` | Major DOM change — re-fetch AX tree |
| `network.requestWillBeSent` / `loadingFinished` | Loading counter in status bar |

### AX Tree Diffing

- Diff new snapshot against previous by node ID
- Only update/add/remove changed document nodes
- Preserve focus and scroll position across updates

## Profile Management

```
~/.texelation/browser/
├── profiles/
│   ├── default/          # Main profile (Chromium user-data-dir)
│   ├── work-google/      # Named isolated profile
│   └── github/           # Named profile
└── config.json           # Default profile, startup URL
```

- Single default profile for general browsing
- `:profile <name>` creates/switches to named isolated profiles
- Profiles persist cookies, localStorage, sessions — OIDC works across restarts

## Image Widget (texelui)

### Capability Detection (at startup)

```go
type ImageCapability int
const (
    ImageNone     ImageCapability = iota  // alt-text only
    ImageBlockArt                         // ANSI half-block chars (▀▄█)
    ImageSixel                            // Sixel protocol
    ImageKitty                            // Kitty graphics protocol
)
```

Detection: Kitty query → DA1 for Sixel → block art fallback.

### MVP: Block Art + Alt-text

- Decode image bytes (stdlib `image` package)
- Quantize colors, use half-block characters for 2 pixels per cell height
- Falls back to `[img: alt text]` if image can't be decoded
- Kitty and Sixel added later — same widget API, just different render backend

### Browser Integration

- `Tab.FetchImageBytes(url)` gets images through Chromium (respects cookies/auth)
- CAPTCHA: rendered inline as image, user types answer in adjacent Input widget

## Key Bindings

| Key | Action |
|-----|--------|
| Tab / Shift-Tab | Cycle focusable elements |
| Enter | Activate focused element |
| Ctrl+L | Focus URL bar |
| Ctrl+M | Toggle reading/form mode |
| Alt+Left / Alt+Right | Back / Forward |
| Ctrl+R | Reload |
| Page Up/Down | Scroll |
| / | Search in page text |
| Escape | Cancel search, unfocus URL bar |

## File Structure

```
texelation/apps/texelbrowse/
├── browse.go          # App struct, lifecycle, App interface
├── register.go        # Registry registration
├── engine.go          # Chromium/chromedp wrapper
├── axtree.go          # AX tree fetching and diffing
├── document.go        # Intermediate document model
├── mapper.go          # AX node → document node → widget
├── layout.go          # Adaptive layout manager
├── commands.go        # URL bar, :profile, navigation
└── browse_test.go     # Tests

texelui/widgets/image.go   # Image widget

texelation/cmd/texelbrowse/main.go  # Standalone binary
```

## Dependencies

```
github.com/chromedp/chromedp   # CDP client
github.com/chromedp/cdproto    # CDP protocol types
```

Image (MVP): stdlib `image`, `image/png`, `image/jpeg` only.

## Out of Scope (MVP)

- Kitty/Sixel image rendering
- JavaScript console / DevTools
- Downloads management
- Bookmarks
- Multiple tabs within one pane (use texelation's pane system)
- Cookie/storage inspector
