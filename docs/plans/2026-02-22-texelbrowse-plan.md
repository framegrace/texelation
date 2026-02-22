# TexelBrowse Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a semantic terminal browser that drives Chromium via CDP and maps the Accessibility Tree to native texelui widgets.

**Architecture:** Three layers — browser engine (chromedp wrapper), document model (AX tree to intermediate nodes), widget layer (texelui widgets + adaptive layout). The app implements `texelui/core.App` via `adapter.UIApp`, registers with the texelation registry, and can also run standalone via `cmd/texelbrowse/main.go`.

**Tech Stack:** Go 1.24.3, `chromedp`/`cdproto` for CDP, `texelui` for widgets, `tcell` for terminal rendering, stdlib `image` for block-art image decoding.

---

### Task 1: Add chromedp dependency and scaffold the app package

**Files:**
- Modify: `go.mod` (add chromedp)
- Create: `apps/texelbrowse/browse.go`
- Create: `apps/texelbrowse/register.go`

**Step 1: Add chromedp dependency**

Run:
```bash
cd /home/marc/projects/texel/texelation
go get github.com/chromedp/chromedp@latest
go get github.com/chromedp/cdproto@latest
```

**Step 2: Create the app scaffold with minimal App interface**

Create `apps/texelbrowse/browse.go`:

```go
package texelbrowse

import (
	"sync"

	"github.com/framegrace/texelui/adapter"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
	"github.com/gdamore/tcell/v2"
)

// BrowseApp is a semantic terminal browser that drives Chromium via CDP
// and renders web content through the Accessibility Tree.
type BrowseApp struct {
	*adapter.UIApp
	mu        sync.Mutex
	startURL  string
	statusBar *widgets.StatusBar
}

func New(startURL string) core.App {
	ui := core.NewUIManager()
	app := &BrowseApp{
		startURL: startURL,
	}
	app.UIApp = adapter.NewUIApp("TexelBrowse", ui)
	app.statusBar = app.UIApp.StatusBar()
	if app.statusBar != nil {
		app.statusBar.SetLeft("texelbrowse")
		app.statusBar.SetCenter("No page loaded")
	}
	return app
}
```

**Step 3: Create the registry registration**

Create `apps/texelbrowse/register.go`:

```go
package texelbrowse

import "github.com/framegrace/texelation/registry"

func init() {
	registry.RegisterBuiltInProvider(func(_ *registry.Registry) (*registry.Manifest, registry.AppFactory) {
		return &registry.Manifest{
			Name:        "texelbrowse",
			DisplayName: "TexelBrowse",
			Description: "Semantic terminal browser",
			Icon:        "🌐",
			Category:    "utility",
			ThemeSchema: registry.ThemeSchema{
				"desktop": {"default_bg"},
				"ui":      {"text.primary", "text.secondary", "text.active"},
			},
		}, func() interface{} {
			return New("")
		}
	})
}
```

**Step 4: Verify it compiles**

Run:
```bash
cd /home/marc/projects/texel/texelation && go build ./apps/texelbrowse/...
```
Expected: No errors.

**Step 5: Commit**

```bash
git add apps/texelbrowse/ go.mod go.sum
git commit -m "Scaffold texelbrowse app package with chromedp dependency"
```

---

### Task 2: Browser engine — Chromium lifecycle and navigation

**Files:**
- Create: `apps/texelbrowse/engine.go`
- Create: `apps/texelbrowse/engine_test.go`

**Step 1: Write a test for engine launch and navigation**

Create `apps/texelbrowse/engine_test.go`:

```go
package texelbrowse

import (
	"os"
	"testing"
)

func TestEngine_LaunchAndNavigate(t *testing.T) {
	if os.Getenv("TEXELBROWSE_INTEGRATION") == "" {
		t.Skip("set TEXELBROWSE_INTEGRATION=1 to run browser tests (requires chromium)")
	}

	profileDir := t.TempDir()
	engine, err := NewEngine(profileDir)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	tab, err := engine.NewTab()
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}

	if err := tab.Navigate("https://example.com"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	url, title := tab.Location()
	if url == "" {
		t.Error("expected non-empty URL after navigation")
	}
	if title == "" {
		t.Error("expected non-empty title after navigation")
	}
	t.Logf("Navigated to: %s (%s)", url, title)
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
TEXELBROWSE_INTEGRATION=1 go test ./apps/texelbrowse/ -run TestEngine_LaunchAndNavigate -v -timeout 30s
```
Expected: FAIL — `NewEngine` not defined.

**Step 3: Implement the engine**

Create `apps/texelbrowse/engine.go`:

```go
package texelbrowse

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Engine manages a Chromium process with a persistent profile.
type Engine struct {
	allocCtx    context.Context
	allocCancel context.CancelFunc
	profileDir  string
	mu          sync.Mutex
}

// NewEngine launches Chromium with the given profile directory.
func NewEngine(profileDir string) (*Engine, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(profileDir),
		chromedp.DisableGPU,
		chromedp.Flag("disable-software-rasterizer", true),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)

	return &Engine{
		allocCtx:    allocCtx,
		allocCancel: allocCancel,
		profileDir:  profileDir,
	}, nil
}

// Close shuts down the Chromium process.
func (e *Engine) Close() {
	e.allocCancel()
}

// Tab represents a single browser tab (CDP target).
type Tab struct {
	ctx    context.Context
	cancel context.CancelFunc
	engine *Engine
	mu     sync.Mutex
	url    string
	title  string

	// Callbacks
	OnNavigate func(url, title string)
	OnLoading  func(loading bool)
}

// NewTab opens a new browser tab.
func (e *Engine) NewTab() (*Tab, error) {
	ctx, cancel := chromedp.NewContext(e.allocCtx)

	tab := &Tab{
		ctx:    ctx,
		cancel: cancel,
		engine: e,
	}

	// Start the browser by running an empty action.
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("start browser: %w", err)
	}

	// Enable lifecycle events.
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return page.SetLifecycleEventsEnabled(true).Do(ctx)
	})); err != nil {
		cancel()
		return nil, fmt.Errorf("enable lifecycle: %w", err)
	}

	tab.setupListeners()
	return tab, nil
}

// Navigate loads a URL.
func (t *Tab) Navigate(url string) error {
	return chromedp.Run(t.ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return chromedp.Location(&t.url).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return chromedp.Title(&t.title).Do(ctx)
		}),
	)
}

// Back navigates backward in history.
func (t *Tab) Back() error {
	return chromedp.Run(t.ctx, chromedp.NavigateBack())
}

// Forward navigates forward in history.
func (t *Tab) Forward() error {
	return chromedp.Run(t.ctx, chromedp.NavigateForward())
}

// Reload reloads the current page.
func (t *Tab) Reload() error {
	return chromedp.Run(t.ctx, chromedp.Reload())
}

// Location returns current URL and title.
func (t *Tab) Location() (string, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.url, t.title
}

// Close closes the tab.
func (t *Tab) Close() {
	t.cancel()
}

func (t *Tab) setupListeners() {
	chromedp.ListenTarget(t.ctx, func(ev any) {
		switch ev := ev.(type) {
		case *page.EventFrameNavigated:
			t.mu.Lock()
			t.url = ev.Frame.URL
			t.title = ev.Frame.Name
			t.mu.Unlock()
			if t.OnNavigate != nil {
				t.OnNavigate(ev.Frame.URL, ev.Frame.Name)
			}

		case *page.EventLifecycleEvent:
			switch ev.Name {
			case "load":
				// Re-fetch title after load.
				var title string
				ctx, cancel := context.WithTimeout(t.ctx, 2*time.Second)
				defer cancel()
				if err := chromedp.Run(ctx, chromedp.Title(&title)); err == nil {
					t.mu.Lock()
					t.title = title
					t.mu.Unlock()
				}
			case "networkIdle":
				if t.OnLoading != nil {
					t.OnLoading(false)
				}
			}

		case *page.EventFrameStartedLoading:
			if t.OnLoading != nil {
				t.OnLoading(true)
			}
		}
	})
}
```

**Step 4: Run test to verify it passes**

Run:
```bash
TEXELBROWSE_INTEGRATION=1 go test ./apps/texelbrowse/ -run TestEngine_LaunchAndNavigate -v -timeout 30s
```
Expected: PASS (requires chromium installed).

**Step 5: Commit**

```bash
git add apps/texelbrowse/engine.go apps/texelbrowse/engine_test.go
git commit -m "Add browser engine with Chromium lifecycle and navigation"
```

---

### Task 3: AX tree fetching and document model

**Files:**
- Create: `apps/texelbrowse/axtree.go`
- Create: `apps/texelbrowse/document.go`
- Create: `apps/texelbrowse/document_test.go`

**Step 1: Write a test for AX tree to document model conversion**

Create `apps/texelbrowse/document_test.go`:

```go
package texelbrowse

import (
	"testing"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
)

func TestDocumentModel_FromAXNodes(t *testing.T) {
	// Build a minimal AX tree: root > heading > text, root > link
	nodes := []*accessibility.Node{
		makeAXNode("1", "", "RootWebArea", "Example Page", false, "2", "3"),
		makeAXNode("2", "1", "heading", "Welcome", false),
		makeAXNode("3", "1", "link", "Click here", false),
	}

	doc := BuildDocument(nodes)

	if doc == nil {
		t.Fatal("expected non-nil document")
	}
	if len(doc.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(doc.Nodes))
	}

	// Check heading node
	heading := doc.Nodes[1]
	if heading.Role != "heading" {
		t.Errorf("expected heading role, got %s", heading.Role)
	}
	if heading.Name != "Welcome" {
		t.Errorf("expected name 'Welcome', got %s", heading.Name)
	}

	// Check link node
	link := doc.Nodes[2]
	if link.Role != "link" {
		t.Errorf("expected link role, got %s", link.Role)
	}
	if !link.Interactive {
		t.Error("expected link to be interactive")
	}
}

func TestDocumentModel_ModeDetection(t *testing.T) {
	// Page with mostly text -> reading mode
	textNodes := []*accessibility.Node{
		makeAXNode("1", "", "RootWebArea", "Article", false, "2", "3", "4", "5"),
		makeAXNode("2", "1", "heading", "Title", false),
		makeAXNode("3", "1", "paragraph", "Some text content", false),
		makeAXNode("4", "1", "paragraph", "More text content", false),
		makeAXNode("5", "1", "link", "Click", false),
	}
	doc := BuildDocument(textNodes)
	if doc.SuggestedMode() != ModeReading {
		t.Errorf("expected reading mode, got %v", doc.SuggestedMode())
	}

	// Page with mostly form elements -> form mode
	formNodes := []*accessibility.Node{
		makeAXNode("1", "", "RootWebArea", "Login", false, "2", "3", "4"),
		makeAXNode("2", "1", "textbox", "Email", false),
		makeAXNode("3", "1", "textbox", "Password", false),
		makeAXNode("4", "1", "button", "Sign in", false),
	}
	doc = BuildDocument(formNodes)
	if doc.SuggestedMode() != ModeForm {
		t.Errorf("expected form mode, got %v", doc.SuggestedMode())
	}
}

// Helper to construct AX nodes for testing.
func makeAXNode(id, parentID, role, name string, ignored bool, childIDs ...string) *accessibility.Node {
	node := &accessibility.Node{
		NodeID:  accessibility.NodeID(id),
		Ignored: ignored,
	}
	if parentID != "" {
		node.ParentID = accessibility.NodeID(parentID)
	}
	node.Role = &accessibility.Value{Value: []byte(`"` + role + `"`)}
	node.Name = &accessibility.Value{Value: []byte(`"` + name + `"`)}
	for _, cid := range childIDs {
		node.ChildIDs = append(node.ChildIDs, accessibility.NodeID(cid))
	}
	node.BackendDOMNodeID = cdp.BackendNodeID(0)
	return node
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
go test ./apps/texelbrowse/ -run TestDocumentModel -v
```
Expected: FAIL — `BuildDocument` not defined.

**Step 3: Create the document model**

Create `apps/texelbrowse/document.go`:

```go
package texelbrowse

// DisplayMode represents how the document should be rendered.
type DisplayMode int

const (
	ModeReading DisplayMode = iota
	ModeForm
)

// DocNode is an intermediate representation of a semantic element.
type DocNode struct {
	ID          string // Stable ID from AX node
	Role        string // AX role: heading, link, textbox, button, etc.
	Name        string // Accessible name
	Value       string // Current value (for inputs)
	Description string // Accessible description
	Level       int    // Heading level (1-6), list nesting depth
	Interactive bool   // Whether this element is clickable/typeable
	Children    []string // Child node IDs
	ParentID    string
	Properties  map[string]string // Flattened AX properties

	// Back-reference for CDP actions.
	BackendNodeID int64
}

// Document represents a parsed page as a list of semantic nodes.
type Document struct {
	Nodes  []*DocNode
	ByID   map[string]*DocNode
	URL    string
	Title  string
}

// SuggestedMode returns the auto-detected display mode.
// If interactive elements are > 30% of total meaningful nodes, suggest form mode.
func (d *Document) SuggestedMode() DisplayMode {
	if len(d.Nodes) == 0 {
		return ModeReading
	}
	interactive := 0
	total := 0
	for _, n := range d.Nodes {
		if n.Role == "RootWebArea" || n.Role == "generic" || n.Role == "none" || n.Role == "group" {
			continue
		}
		total++
		if n.Interactive {
			interactive++
		}
	}
	if total == 0 {
		return ModeReading
	}
	ratio := float64(interactive) / float64(total)
	if ratio > 0.3 {
		return ModeForm
	}
	return ModeReading
}

// InteractiveNodes returns all interactive nodes in document order.
func (d *Document) InteractiveNodes() []*DocNode {
	var result []*DocNode
	for _, n := range d.Nodes {
		if n.Interactive {
			result = append(result, n)
		}
	}
	return result
}
```

**Step 4: Create the AX tree converter**

Create `apps/texelbrowse/axtree.go`:

```go
package texelbrowse

import (
	"encoding/json"
	"strings"

	"github.com/chromedp/cdproto/accessibility"
)

// interactiveRoles are AX roles that represent user-interactive elements.
var interactiveRoles = map[string]bool{
	"link":        true,
	"button":      true,
	"textbox":     true,
	"searchbox":   true,
	"checkbox":    true,
	"radiobutton": true,
	"combobox":    true,
	"listbox":     true,
	"menuitem":    true,
	"tab":         true,
	"switch":      true,
	"slider":      true,
	"spinbutton":  true,
}

// BuildDocument converts a flat list of AX nodes into a Document model.
func BuildDocument(axNodes []*accessibility.Node) *Document {
	doc := &Document{
		ByID: make(map[string]*DocNode),
	}

	for _, ax := range axNodes {
		if ax.Ignored {
			continue
		}

		role := extractValue(ax.Role)
		name := extractValue(ax.Name)

		if role == "" && name == "" {
			continue
		}

		node := &DocNode{
			ID:            string(ax.NodeID),
			Role:          role,
			Name:          name,
			Value:         extractValue(ax.Value),
			Description:   extractValue(ax.Description),
			Interactive:   interactiveRoles[role],
			ParentID:      string(ax.ParentID),
			BackendNodeID: int64(ax.BackendDOMNodeID),
			Properties:    make(map[string]string),
		}

		for _, childID := range ax.ChildIDs {
			node.Children = append(node.Children, string(childID))
		}

		// Extract relevant properties.
		for _, prop := range ax.Properties {
			node.Properties[string(prop.Name)] = extractValue(prop.Value)
		}

		// Detect heading level from properties or role.
		if role == "heading" {
			if lvl, ok := node.Properties["level"]; ok {
				switch lvl {
				case "1":
					node.Level = 1
				case "2":
					node.Level = 2
				case "3":
					node.Level = 3
				case "4":
					node.Level = 4
				case "5":
					node.Level = 5
				case "6":
					node.Level = 6
				default:
					node.Level = 2
				}
			} else {
				node.Level = 2
			}
		}

		doc.Nodes = append(doc.Nodes, node)
		doc.ByID[node.ID] = node
	}

	return doc
}

// extractValue gets the string content from an AX Value.
func extractValue(v *accessibility.Value) string {
	if v == nil || len(v.Value) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(v.Value, &s); err != nil {
		// Fallback: trim quotes manually.
		s = strings.Trim(string(v.Value), `"`)
	}
	return s
}
```

**Step 5: Run tests to verify they pass**

Run:
```bash
go test ./apps/texelbrowse/ -run TestDocumentModel -v
```
Expected: PASS.

**Step 6: Commit**

```bash
git add apps/texelbrowse/axtree.go apps/texelbrowse/document.go apps/texelbrowse/document_test.go
git commit -m "Add AX tree parser and document model with mode detection"
```

---

### Task 4: AX tree fetching from a live tab

**Files:**
- Modify: `apps/texelbrowse/engine.go` (add FetchAXTree method)
- Modify: `apps/texelbrowse/engine_test.go` (add integration test)

**Step 1: Write integration test for AX tree fetching**

Add to `apps/texelbrowse/engine_test.go`:

```go
func TestEngine_FetchAXTree(t *testing.T) {
	if os.Getenv("TEXELBROWSE_INTEGRATION") == "" {
		t.Skip("set TEXELBROWSE_INTEGRATION=1 to run browser tests")
	}

	profileDir := t.TempDir()
	engine, err := NewEngine(profileDir)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	tab, err := engine.NewTab()
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}

	if err := tab.Navigate("https://example.com"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	doc, err := tab.FetchDocument()
	if err != nil {
		t.Fatalf("FetchDocument: %v", err)
	}

	if len(doc.Nodes) == 0 {
		t.Error("expected non-empty document")
	}

	// example.com should have at least a heading and a link
	hasHeading := false
	hasLink := false
	for _, n := range doc.Nodes {
		if n.Role == "heading" {
			hasHeading = true
			t.Logf("Heading: %s (level %d)", n.Name, n.Level)
		}
		if n.Role == "link" {
			hasLink = true
			t.Logf("Link: %s", n.Name)
		}
	}

	if !hasHeading {
		t.Error("expected at least one heading on example.com")
	}
	if !hasLink {
		t.Error("expected at least one link on example.com")
	}

	t.Logf("Document has %d nodes, suggested mode: %v", len(doc.Nodes), doc.SuggestedMode())
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
TEXELBROWSE_INTEGRATION=1 go test ./apps/texelbrowse/ -run TestEngine_FetchAXTree -v -timeout 30s
```
Expected: FAIL — `FetchDocument` not defined on Tab.

**Step 3: Add FetchDocument to Tab**

Add to `apps/texelbrowse/engine.go`:

```go
import (
	// ... existing imports ...
	"github.com/chromedp/cdproto/accessibility"
)

// FetchDocument fetches the AX tree and converts it to a Document model.
func (t *Tab) FetchDocument() (*Document, error) {
	var axNodes []*accessibility.Node

	err := chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		axNodes, err = accessibility.GetFullAXTree().Do(ctx)
		return err
	}))
	if err != nil {
		return nil, fmt.Errorf("fetch AX tree: %w", err)
	}

	doc := BuildDocument(axNodes)

	t.mu.Lock()
	doc.URL = t.url
	doc.Title = t.title
	t.mu.Unlock()

	return doc, nil
}
```

**Step 4: Run test to verify it passes**

Run:
```bash
TEXELBROWSE_INTEGRATION=1 go test ./apps/texelbrowse/ -run TestEngine_FetchAXTree -v -timeout 30s
```
Expected: PASS.

**Step 5: Commit**

```bash
git add apps/texelbrowse/engine.go apps/texelbrowse/engine_test.go
git commit -m "Add AX tree fetching from live Chromium tab"
```

---

### Task 5: CDP input dispatch (click, type, focus)

**Files:**
- Modify: `apps/texelbrowse/engine.go` (add input methods)
- Modify: `apps/texelbrowse/engine_test.go` (add input tests)

**Step 1: Write integration test for click and type**

Add to `apps/texelbrowse/engine_test.go`:

```go
func TestEngine_ClickAndType(t *testing.T) {
	if os.Getenv("TEXELBROWSE_INTEGRATION") == "" {
		t.Skip("set TEXELBROWSE_INTEGRATION=1 to run browser tests")
	}

	profileDir := t.TempDir()
	engine, err := NewEngine(profileDir)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	tab, err := engine.NewTab()
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}

	// Navigate to a page with a search box (use a data URI for determinism)
	html := `data:text/html,<html><body>
		<input type="text" id="search" aria-label="Search">
		<button id="btn" onclick="document.getElementById('search').value='clicked'">Click Me</button>
	</body></html>`

	if err := tab.Navigate(html); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	doc, err := tab.FetchDocument()
	if err != nil {
		t.Fatalf("FetchDocument: %v", err)
	}

	// Find the textbox
	var textbox *DocNode
	for _, n := range doc.Nodes {
		if n.Role == "textbox" {
			textbox = n
			break
		}
	}
	if textbox == nil {
		t.Fatal("expected to find a textbox")
	}

	// Focus and type
	if err := tab.FocusNode(textbox.BackendNodeID); err != nil {
		t.Fatalf("FocusNode: %v", err)
	}
	if err := tab.TypeText("hello world"); err != nil {
		t.Fatalf("TypeText: %v", err)
	}

	// Find the button and click it
	var button *DocNode
	for _, n := range doc.Nodes {
		if n.Role == "button" {
			button = n
			break
		}
	}
	if button == nil {
		t.Fatal("expected to find a button")
	}

	if err := tab.ClickNode(button.BackendNodeID); err != nil {
		t.Fatalf("ClickNode: %v", err)
	}

	t.Log("Click and type completed successfully")
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
TEXELBROWSE_INTEGRATION=1 go test ./apps/texelbrowse/ -run TestEngine_ClickAndType -v -timeout 30s
```
Expected: FAIL — methods not defined.

**Step 3: Add input dispatch methods to Tab**

Add to `apps/texelbrowse/engine.go`:

```go
import (
	// ... existing imports ...
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
)

// FocusNode focuses a DOM element by its BackendNodeID.
func (t *Tab) FocusNode(backendNodeID int64) error {
	return chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return dom.Focus().WithBackendNodeID(cdp.BackendNodeID(backendNodeID)).Do(ctx)
	}))
}

// ClickNode clicks the center of a DOM element by its BackendNodeID.
func (t *Tab) ClickNode(backendNodeID int64) error {
	return chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		boxModel, err := dom.GetBoxModel().
			WithBackendNodeID(cdp.BackendNodeID(backendNodeID)).
			Do(ctx)
		if err != nil {
			return fmt.Errorf("GetBoxModel: %w", err)
		}

		content := boxModel.Content
		cx := (content[0] + content[2] + content[4] + content[6]) / 4.0
		cy := (content[1] + content[3] + content[5] + content[7]) / 4.0

		if err := input.DispatchMouseEvent(input.MousePressed, cx, cy).
			WithButton(input.Left).
			WithClickCount(1).
			Do(ctx); err != nil {
			return err
		}
		return input.DispatchMouseEvent(input.MouseReleased, cx, cy).
			WithButton(input.Left).
			WithClickCount(1).
			Do(ctx)
	}))
}

// TypeText types text into the currently focused element.
func (t *Tab) TypeText(text string) error {
	return chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.InsertText(text).Do(ctx)
	}))
}

// SetValue sets the value of an input element and dispatches input/change events.
func (t *Tab) SetValue(backendNodeID int64, value string) error {
	return chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		// Focus the element first.
		if err := dom.Focus().WithBackendNodeID(cdp.BackendNodeID(backendNodeID)).Do(ctx); err != nil {
			return err
		}
		// Select all existing text and replace.
		if err := input.DispatchKeyEvent(input.KeyRawDown).
			WithKey("a").
			WithCode("KeyA").
			WithModifiers(2). // Ctrl
			Do(ctx); err != nil {
			return err
		}
		if err := input.DispatchKeyEvent(input.KeyUp).
			WithKey("a").
			WithCode("KeyA").
			Do(ctx); err != nil {
			return err
		}
		return input.InsertText(value).Do(ctx)
	}))
}

// PressKey sends a key press (e.g., "Enter", "Tab", "Escape").
func (t *Tab) PressKey(key string, code string, keyCode int) error {
	return chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := input.DispatchKeyEvent(input.KeyRawDown).
			WithKey(key).
			WithCode(code).
			WithWindowsVirtualKeyCode(int64(keyCode)).
			Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).
			WithKey(key).
			WithCode(code).
			WithWindowsVirtualKeyCode(int64(keyCode)).
			Do(ctx)
	}))
}
```

**Step 4: Run test to verify it passes**

Run:
```bash
TEXELBROWSE_INTEGRATION=1 go test ./apps/texelbrowse/ -run TestEngine_ClickAndType -v -timeout 30s
```
Expected: PASS.

**Step 5: Commit**

```bash
git add apps/texelbrowse/engine.go apps/texelbrowse/engine_test.go
git commit -m "Add CDP input dispatch: click, type, focus, set value"
```

---

### Task 6: New texelui widgets — Link and Checkbox

**Files:**
- Create: `texelui/widgets/link.go`
- Create: `texelui/widgets/link_test.go`
- Create: `texelui/widgets/checkbox.go`
- Create: `texelui/widgets/checkbox_test.go`

**Step 1: Write test for Link widget**

Create `texelui/widgets/link_test.go`:

```go
package widgets

import (
	"testing"

	"github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"
)

func TestLink_Render(t *testing.T) {
	link := NewLink("Click here")
	link.Resize(20, 1)

	buf := make([][]core.Cell, 1)
	buf[0] = make([]core.Cell, 20)
	p := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 20, H: 1})

	link.Draw(p)

	// Verify text is rendered
	text := ""
	for _, c := range buf[0] {
		if c.Ch != 0 {
			text += string(c.Ch)
		}
	}
	if text != "Click here" {
		t.Errorf("expected 'Click here', got %q", text)
	}

	// Verify underline style
	_, _, attr := buf[0][0].Style.Decompose()
	if attr&tcell.AttrUnderline == 0 {
		t.Error("expected link text to be underlined")
	}
}

func TestLink_HandleKey(t *testing.T) {
	clicked := false
	link := NewLink("Test")
	link.OnClick = func() { clicked = true }

	// Enter should trigger click
	ev := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	handled := link.HandleKey(ev)

	if !handled {
		t.Error("expected Enter to be handled")
	}
	if !clicked {
		t.Error("expected OnClick to be called")
	}
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
go test ./texelui/widgets/ -run TestLink -v
```
Expected: FAIL — `NewLink` not defined.

**Step 3: Implement Link widget**

Create `texelui/widgets/link.go`:

```go
package widgets

import (
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
	"github.com/gdamore/tcell/v2"
)

// Link is a clickable text widget styled with underline.
type Link struct {
	core.BaseWidget
	Text    string
	Style   tcell.Style
	OnClick func()
	inv     func(core.Rect)
}

func NewLink(text string) *Link {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.active")
	bg := tm.GetSemanticColor("bg.surface")

	l := &Link{
		Text:  text,
		Style: tcell.StyleDefault.Foreground(fg).Background(bg).Underline(true),
	}
	l.SetFocusable(true)
	l.Resize(len([]rune(text)), 1)
	return l
}

func (l *Link) Draw(p *core.Painter) {
	style := l.EffectiveStyle(l.Style)
	// Ensure underline is always on for links.
	style = style.Underline(true)

	x, y := l.Position()
	w, _ := l.Size()
	p.Fill(core.Rect{X: x, Y: y, W: w, H: 1}, ' ', style)

	runes := []rune(l.Text)
	for i, r := range runes {
		if i >= w {
			break
		}
		p.SetCell(x+i, y, r, style)
	}
}

func (l *Link) HandleKey(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyEnter {
		if l.OnClick != nil {
			l.OnClick()
		}
		return true
	}
	return false
}

func (l *Link) SetInvalidator(fn func(core.Rect)) { l.inv = fn }

func (l *Link) invalidate() {
	if l.inv != nil {
		l.inv(core.Rect{X: l.Rect.X, Y: l.Rect.Y, W: l.Rect.W, H: l.Rect.H})
	}
}
```

**Step 4: Run Link test**

Run:
```bash
go test ./texelui/widgets/ -run TestLink -v
```
Expected: PASS.

**Step 5: Write test for Checkbox widget**

Create `texelui/widgets/checkbox_test.go`:

```go
package widgets

import (
	"testing"

	"github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"
)

func TestCheckbox_Toggle(t *testing.T) {
	cb := NewCheckbox("Accept terms")
	if cb.Checked {
		t.Error("expected unchecked by default")
	}

	// Space should toggle
	ev := tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
	handled := cb.HandleKey(ev)

	if !handled {
		t.Error("expected Space to be handled")
	}
	if !cb.Checked {
		t.Error("expected checked after toggle")
	}

	// Toggle again
	cb.HandleKey(ev)
	if cb.Checked {
		t.Error("expected unchecked after second toggle")
	}
}

func TestCheckbox_Render(t *testing.T) {
	cb := NewCheckbox("Option A")
	cb.Resize(20, 1)

	buf := make([][]core.Cell, 1)
	buf[0] = make([]core.Cell, 20)
	p := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 20, H: 1})

	cb.Draw(p)

	// Should show "[ ] Option A"
	text := ""
	for _, c := range buf[0] {
		if c.Ch != 0 {
			text += string(c.Ch)
		}
	}
	if text != "[ ] Option A" {
		t.Errorf("unchecked render: expected '[ ] Option A', got %q", text)
	}

	// Check it and re-render
	cb.Checked = true
	p = core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 20, H: 1})
	cb.Draw(p)

	text = ""
	for _, c := range buf[0] {
		if c.Ch != 0 {
			text += string(c.Ch)
		}
	}
	if text != "[x] Option A" {
		t.Errorf("checked render: expected '[x] Option A', got %q", text)
	}
}
```

**Step 6: Run test to verify it fails**

Run:
```bash
go test ./texelui/widgets/ -run TestCheckbox -v
```
Expected: FAIL — `NewCheckbox` not defined.

**Step 7: Implement Checkbox widget**

Create `texelui/widgets/checkbox.go`:

```go
package widgets

import (
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
	"github.com/gdamore/tcell/v2"
)

// Checkbox is a togglable widget with a label.
type Checkbox struct {
	core.BaseWidget
	Label    string
	Checked  bool
	Style    tcell.Style
	OnChange func(checked bool)
	inv      func(core.Rect)
}

func NewCheckbox(label string) *Checkbox {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")

	cb := &Checkbox{
		Label: label,
		Style: tcell.StyleDefault.Foreground(fg).Background(bg),
	}
	cb.SetFocusable(true)
	// "[ ] " prefix = 4 chars + label
	cb.Resize(4+len([]rune(label)), 1)
	return cb
}

func (cb *Checkbox) Draw(p *core.Painter) {
	style := cb.EffectiveStyle(cb.Style)
	x, y := cb.Position()
	w, _ := cb.Size()

	p.Fill(core.Rect{X: x, Y: y, W: w, H: 1}, ' ', style)

	// Draw checkbox indicator.
	indicator := "[ ] "
	if cb.Checked {
		indicator = "[x] "
	}

	col := x
	for _, r := range indicator {
		if col >= x+w {
			break
		}
		p.SetCell(col, y, r, style)
		col++
	}

	// Draw label.
	for _, r := range cb.Label {
		if col >= x+w {
			break
		}
		p.SetCell(col, y, r, style)
		col++
	}
}

func (cb *Checkbox) HandleKey(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyRune && ev.Rune() == ' ' {
		cb.Checked = !cb.Checked
		if cb.OnChange != nil {
			cb.OnChange(cb.Checked)
		}
		cb.invalidate()
		return true
	}
	if ev.Key() == tcell.KeyEnter {
		cb.Checked = !cb.Checked
		if cb.OnChange != nil {
			cb.OnChange(cb.Checked)
		}
		cb.invalidate()
		return true
	}
	return false
}

func (cb *Checkbox) SetInvalidator(fn func(core.Rect)) { cb.inv = fn }

func (cb *Checkbox) invalidate() {
	if cb.inv != nil {
		cb.inv(core.Rect{X: cb.Rect.X, Y: cb.Rect.Y, W: cb.Rect.W, H: cb.Rect.H})
	}
}
```

**Step 8: Run Checkbox test**

Run:
```bash
go test ./texelui/widgets/ -run TestCheckbox -v
```
Expected: PASS.

**Step 9: Commit**

```bash
git add texelui/widgets/link.go texelui/widgets/link_test.go texelui/widgets/checkbox.go texelui/widgets/checkbox_test.go
git commit -m "Add Link and Checkbox widgets to texelui"
```

---

### Task 7: Document-to-widget mapper

**Files:**
- Create: `apps/texelbrowse/mapper.go`
- Create: `apps/texelbrowse/mapper_test.go`

**Step 1: Write test for widget mapping**

Create `apps/texelbrowse/mapper_test.go`:

```go
package texelbrowse

import (
	"testing"

	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
)

func TestMapper_HeadingToLabel(t *testing.T) {
	doc := &Document{
		Nodes: []*DocNode{
			{ID: "1", Role: "heading", Name: "Welcome", Level: 1},
		},
		ByID: map[string]*DocNode{},
	}
	doc.ByID["1"] = doc.Nodes[0]

	mapper := NewMapper(nil)
	widgetList := mapper.MapDocument(doc)

	if len(widgetList) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(widgetList))
	}

	label, ok := widgetList[0].(*widgets.Label)
	if !ok {
		t.Fatalf("expected *widgets.Label, got %T", widgetList[0])
	}
	if label.Text != "# Welcome" {
		t.Errorf("expected '# Welcome', got %q", label.Text)
	}
}

func TestMapper_LinkToLinkWidget(t *testing.T) {
	doc := &Document{
		Nodes: []*DocNode{
			{ID: "1", Role: "link", Name: "Go home", Interactive: true, BackendNodeID: 42},
		},
		ByID: map[string]*DocNode{},
	}
	doc.ByID["1"] = doc.Nodes[0]

	clicked := false
	mapper := NewMapper(func(nodeID int64) {
		clicked = true
		if nodeID != 42 {
			t.Errorf("expected nodeID 42, got %d", nodeID)
		}
	})
	widgetList := mapper.MapDocument(doc)

	if len(widgetList) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(widgetList))
	}

	link, ok := widgetList[0].(*widgets.Link)
	if !ok {
		t.Fatalf("expected *widgets.Link, got %T", widgetList[0])
	}
	if link.Text != "Go home" {
		t.Errorf("expected 'Go home', got %q", link.Text)
	}

	// Trigger the click callback.
	link.OnClick()
	if !clicked {
		t.Error("expected click callback to fire")
	}
}

func TestMapper_TextboxToInput(t *testing.T) {
	doc := &Document{
		Nodes: []*DocNode{
			{ID: "1", Role: "textbox", Name: "Email", Value: "user@example.com", Interactive: true, BackendNodeID: 99},
		},
		ByID: map[string]*DocNode{},
	}
	doc.ByID["1"] = doc.Nodes[0]

	mapper := NewMapper(nil)
	widgetList := mapper.MapDocument(doc)

	if len(widgetList) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(widgetList))
	}

	input, ok := widgetList[0].(*widgets.Input)
	if !ok {
		t.Fatalf("expected *widgets.Input, got %T", widgetList[0])
	}
	if input.Placeholder != "Email" {
		t.Errorf("expected placeholder 'Email', got %q", input.Placeholder)
	}
}

func TestMapper_MixedDocument(t *testing.T) {
	doc := &Document{
		Nodes: []*DocNode{
			{ID: "1", Role: "heading", Name: "Login", Level: 1},
			{ID: "2", Role: "StaticText", Name: "Enter your credentials"},
			{ID: "3", Role: "textbox", Name: "Username", Interactive: true, BackendNodeID: 10},
			{ID: "4", Role: "textbox", Name: "Password", Interactive: true, BackendNodeID: 11},
			{ID: "5", Role: "button", Name: "Sign in", Interactive: true, BackendNodeID: 12},
		},
		ByID: map[string]*DocNode{},
	}
	for _, n := range doc.Nodes {
		doc.ByID[n.ID] = n
	}

	mapper := NewMapper(nil)
	widgetList := mapper.MapDocument(doc)

	if len(widgetList) != 5 {
		t.Fatalf("expected 5 widgets, got %d", len(widgetList))
	}

	// Verify types in order.
	types := []string{}
	for _, w := range widgetList {
		switch w.(type) {
		case *widgets.Label:
			types = append(types, "Label")
		case *widgets.Link:
			types = append(types, "Link")
		case *widgets.Input:
			types = append(types, "Input")
		case *widgets.Button:
			types = append(types, "Button")
		case *widgets.Checkbox:
			types = append(types, "Checkbox")
		default:
			types = append(types, "Unknown")
		}
	}

	expected := []string{"Label", "Label", "Input", "Input", "Button"}
	for i, exp := range expected {
		if i >= len(types) || types[i] != exp {
			t.Errorf("widget %d: expected %s, got %s", i, exp, types[i])
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
go test ./apps/texelbrowse/ -run TestMapper -v
```
Expected: FAIL — `NewMapper` not defined.

**Step 3: Implement the mapper**

Create `apps/texelbrowse/mapper.go`:

```go
package texelbrowse

import (
	"fmt"
	"strings"

	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
)

// Mapper converts document nodes to texelui widgets.
type Mapper struct {
	onClickNode func(backendNodeID int64)
	onTypeNode  func(backendNodeID int64, text string)
}

// NewMapper creates a mapper with callbacks for interactive actions.
// onClickNode is called when a link or button widget is activated.
func NewMapper(onClickNode func(backendNodeID int64)) *Mapper {
	return &Mapper{
		onClickNode: onClickNode,
	}
}

// SetOnType sets the callback for when text is entered in an input widget.
func (m *Mapper) SetOnType(fn func(backendNodeID int64, text string)) {
	m.onTypeNode = fn
}

// MapDocument converts a Document into a flat list of widgets.
func (m *Mapper) MapDocument(doc *Document) []core.Widget {
	var result []core.Widget
	for _, node := range doc.Nodes {
		w := m.mapNode(node)
		if w != nil {
			result = append(result, w)
		}
	}
	return result
}

func (m *Mapper) mapNode(node *DocNode) core.Widget {
	switch node.Role {
	case "heading":
		return m.mapHeading(node)
	case "link":
		return m.mapLink(node)
	case "button":
		return m.mapButton(node)
	case "textbox", "searchbox":
		return m.mapInput(node)
	case "checkbox":
		return m.mapCheckbox(node)
	case "separator":
		return m.mapSeparator(node)
	case "StaticText", "paragraph", "text":
		return m.mapText(node)
	case "RootWebArea", "generic", "none", "group", "document":
		// Structural nodes — skip, children handled via flat list.
		return nil
	default:
		// Fallback: render as text if it has a name.
		if node.Name != "" {
			return m.mapText(node)
		}
		return nil
	}
}

func (m *Mapper) mapHeading(node *DocNode) core.Widget {
	prefix := strings.Repeat("#", node.Level) + " "
	label := widgets.NewLabel(prefix + node.Name)
	label.SetFocusable(false)
	return label
}

func (m *Mapper) mapText(node *DocNode) core.Widget {
	if node.Name == "" {
		return nil
	}
	label := widgets.NewLabel(node.Name)
	label.SetFocusable(false)
	return label
}

func (m *Mapper) mapLink(node *DocNode) core.Widget {
	link := widgets.NewLink(node.Name)
	backendID := node.BackendNodeID
	link.OnClick = func() {
		if m.onClickNode != nil {
			m.onClickNode(backendID)
		}
	}
	return link
}

func (m *Mapper) mapButton(node *DocNode) core.Widget {
	btn := widgets.NewButton(node.Name)
	backendID := node.BackendNodeID
	btn.OnClick = func() {
		if m.onClickNode != nil {
			m.onClickNode(backendID)
		}
	}
	return btn
}

func (m *Mapper) mapInput(node *DocNode) core.Widget {
	inp := widgets.NewInput()
	inp.Placeholder = node.Name
	if node.Value != "" {
		inp.Text = node.Value
		inp.CaretPos = len([]rune(node.Value))
	}
	backendID := node.BackendNodeID
	inp.OnSubmit = func(text string) {
		if m.onTypeNode != nil {
			m.onTypeNode(backendID, text)
		}
	}
	return inp
}

func (m *Mapper) mapCheckbox(node *DocNode) core.Widget {
	cb := widgets.NewCheckbox(node.Name)
	if node.Properties["checked"] == "true" {
		cb.Checked = true
	}
	backendID := node.BackendNodeID
	cb.OnChange = func(checked bool) {
		if m.onClickNode != nil {
			m.onClickNode(backendID)
		}
	}
	return cb
}

func (m *Mapper) mapSeparator(node *DocNode) core.Widget {
	_ = fmt.Sprintf // Suppress unused import if needed.
	sep := widgets.NewLabel("────────────────────────────────────────")
	sep.SetFocusable(false)
	return sep
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./apps/texelbrowse/ -run TestMapper -v
```
Expected: PASS.

**Step 5: Commit**

```bash
git add apps/texelbrowse/mapper.go apps/texelbrowse/mapper_test.go
git commit -m "Add AX-to-widget mapper for document rendering"
```

---

### Task 8: Adaptive layout manager

**Files:**
- Create: `apps/texelbrowse/layout.go`
- Create: `apps/texelbrowse/layout_test.go`

**Step 1: Write test for layout manager**

Create `apps/texelbrowse/layout_test.go`:

```go
package texelbrowse

import (
	"testing"

	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
)

func TestLayout_ReadingMode(t *testing.T) {
	layout := NewLayoutManager(80, 24)
	layout.SetMode(ModeReading)

	ws := []core.Widget{
		widgets.NewLabel("# Title"),
		widgets.NewLabel("Some paragraph text that should be word wrapped"),
		widgets.NewLink("Click here"),
	}

	layout.Arrange(ws)

	// Verify widgets are positioned vertically in sequence.
	for i, w := range ws {
		_, y := w.Position()
		if i > 0 {
			_, prevY := ws[i-1].Position()
			if y <= prevY {
				t.Errorf("widget %d (y=%d) should be below widget %d (y=%d)", i, y, i-1, prevY)
			}
		}
	}
}

func TestLayout_FormMode(t *testing.T) {
	layout := NewLayoutManager(80, 24)
	layout.SetMode(ModeForm)

	ws := []core.Widget{
		widgets.NewLabel("Sign in"),
		widgets.NewInput(),
		widgets.NewInput(),
		widgets.NewButton("Submit"),
	}

	layout.Arrange(ws)

	// In form mode, inputs should be wider.
	for _, w := range ws {
		if inp, ok := w.(*widgets.Input); ok {
			sw, _ := inp.Size()
			if sw < 40 {
				t.Errorf("expected input width >= 40 in form mode, got %d", sw)
			}
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
go test ./apps/texelbrowse/ -run TestLayout -v
```
Expected: FAIL — `NewLayoutManager` not defined.

**Step 3: Implement the layout manager**

Create `apps/texelbrowse/layout.go`:

```go
package texelbrowse

import (
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
)

// LayoutManager arranges widgets according to the current display mode.
type LayoutManager struct {
	width  int
	height int
	mode   DisplayMode
}

func NewLayoutManager(width, height int) *LayoutManager {
	return &LayoutManager{
		width:  width,
		height: height,
		mode:   ModeReading,
	}
}

func (lm *LayoutManager) SetMode(mode DisplayMode) {
	lm.mode = mode
}

func (lm *LayoutManager) Mode() DisplayMode {
	return lm.mode
}

func (lm *LayoutManager) Resize(width, height int) {
	lm.width = width
	lm.height = height
}

// Arrange positions and sizes all widgets in the content area.
// The content area excludes the URL bar (row 0-1) and status bar (last row).
func (lm *LayoutManager) Arrange(ws []core.Widget) {
	switch lm.mode {
	case ModeReading:
		lm.arrangeReading(ws)
	case ModeForm:
		lm.arrangeForm(ws)
	}
}

func (lm *LayoutManager) arrangeReading(ws []core.Widget) {
	y := 0
	contentWidth := lm.width - 2 // 1 char margin on each side

	for _, w := range ws {
		w.SetPosition(1, y)

		switch w.(type) {
		case *widgets.Input:
			w.Resize(contentWidth, 1)
		default:
			ww, _ := w.Size()
			if ww > contentWidth {
				w.Resize(contentWidth, 1)
			}
		}

		_, h := w.Size()
		y += h
	}
}

func (lm *LayoutManager) arrangeForm(ws []core.Widget) {
	y := 0
	contentWidth := lm.width - 4 // 2 char margin on each side
	inputWidth := contentWidth
	if inputWidth < 40 {
		inputWidth = 40
	}

	for _, w := range ws {
		w.SetPosition(2, y)

		switch w.(type) {
		case *widgets.Input:
			w.Resize(inputWidth, 1)
			y += 2 // Extra spacing between form fields
		case *widgets.Button:
			w.Resize(inputWidth/3, 1)
			y += 2
		case *widgets.Checkbox:
			w.Resize(contentWidth, 1)
			y += 1
		default:
			ww, _ := w.Size()
			if ww > contentWidth {
				w.Resize(contentWidth, 1)
			}
			y += 1
		}

		if y == 0 {
			_, h := w.Size()
			y += h
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./apps/texelbrowse/ -run TestLayout -v
```
Expected: PASS.

**Step 5: Commit**

```bash
git add apps/texelbrowse/layout.go apps/texelbrowse/layout_test.go
git commit -m "Add adaptive layout manager with reading and form modes"
```

---

### Task 9: Wire everything together — main BrowseApp

**Files:**
- Modify: `apps/texelbrowse/browse.go` (full wiring)
- Create: `apps/texelbrowse/commands.go` (URL bar, navigation commands)

**Step 1: Implement the URL bar and command handling**

Create `apps/texelbrowse/commands.go`:

```go
package texelbrowse

import (
	"strings"

	"github.com/framegrace/texelui/widgets"
)

// URLBar wraps a widgets.Input with URL-specific behavior.
type URLBar struct {
	*widgets.Input
	onNavigate func(url string)
}

func NewURLBar(onNavigate func(url string)) *URLBar {
	inp := widgets.NewInput()
	inp.Placeholder = "Enter URL..."
	inp.SetHelpText("Ctrl+L to focus, Enter to navigate")

	bar := &URLBar{
		Input:      inp,
		onNavigate: onNavigate,
	}

	inp.OnSubmit = func(text string) {
		url := normalizeURL(text)
		if bar.onNavigate != nil {
			bar.onNavigate(url)
		}
	}

	return bar
}

func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		if strings.Contains(raw, ".") {
			return "https://" + raw
		}
		// Treat as search query — for now, just prefix with https://
		return "https://" + raw
	}
	return raw
}
```

**Step 2: Wire the full BrowseApp**

Replace `apps/texelbrowse/browse.go` with:

```go
package texelbrowse

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/framegrace/texelui/adapter"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
	"github.com/gdamore/tcell/v2"
)

// BrowseApp is a semantic terminal browser.
type BrowseApp struct {
	*adapter.UIApp

	mu        sync.Mutex
	startURL  string
	engine    *Engine
	tab       *Tab
	doc       *Document
	mapper    *Mapper
	layout    *LayoutManager
	urlBar    *URLBar
	statusBar *widgets.StatusBar
	content   []core.Widget
	mode      DisplayMode
	modeForce bool // true if user manually set mode
	width     int
	height    int
	profileDir string
}

// New creates a new BrowseApp.
func New(startURL string) core.App {
	return NewWithProfile(startURL, defaultProfileDir())
}

// NewWithProfile creates a new BrowseApp with a specific profile directory.
func NewWithProfile(startURL, profileDir string) core.App {
	ui := core.NewUIManager()

	app := &BrowseApp{
		startURL:   startURL,
		profileDir: profileDir,
		width:      80,
		height:     24,
	}

	app.UIApp = adapter.NewUIApp("TexelBrowse", ui)

	// Status bar.
	app.statusBar = app.UIApp.StatusBar()
	if app.statusBar != nil {
		app.statusBar.SetLeft("texelbrowse")
		app.statusBar.SetCenter("No page loaded")
	}

	// URL bar.
	app.urlBar = NewURLBar(func(url string) {
		go app.navigate(url)
	})
	ui.AddWidget(app.urlBar.Input)

	// Mapper with click callback.
	app.mapper = NewMapper(func(backendNodeID int64) {
		go app.clickNode(backendNodeID)
	})
	app.mapper.SetOnType(func(backendNodeID int64, text string) {
		go app.typeInNode(backendNodeID, text)
	})

	// Layout.
	app.layout = NewLayoutManager(app.width, app.height)

	// Resize handler.
	app.SetOnResize(func(w, h int) {
		app.mu.Lock()
		app.width = w
		app.height = h
		app.layout.Resize(w, h)
		app.mu.Unlock()
		app.relayout()
	})

	return app
}

func (a *BrowseApp) Run() error {
	// Launch Chromium.
	engine, err := NewEngine(a.profileDir)
	if err != nil {
		a.setStatus("Engine error: " + err.Error())
		return a.UIApp.Run()
	}
	a.engine = engine

	tab, err := engine.NewTab()
	if err != nil {
		a.setStatus("Tab error: " + err.Error())
		return a.UIApp.Run()
	}
	a.tab = tab

	tab.OnNavigate = func(url, title string) {
		a.urlBar.Text = url
		if title != "" {
			a.UIApp.SetTitle("TexelBrowse - " + title)
		}
		go a.refreshDocument()
	}
	tab.OnLoading = func(loading bool) {
		if loading {
			a.setStatus("Loading...")
		} else {
			a.setStatus("")
		}
	}

	// Navigate to start URL if provided.
	if a.startURL != "" {
		go a.navigate(a.startURL)
	}

	return a.UIApp.Run()
}

func (a *BrowseApp) Stop() {
	if a.tab != nil {
		a.tab.Close()
	}
	if a.engine != nil {
		a.engine.Close()
	}
	a.UIApp.Stop()
}

func (a *BrowseApp) HandleKey(ev *tcell.EventKey) {
	// Global keybindings.
	switch {
	case ev.Key() == tcell.KeyCtrlL:
		a.UI().Focus(a.urlBar.Input)
		return
	case ev.Key() == tcell.KeyCtrlM:
		a.toggleMode()
		return
	case ev.Key() == tcell.KeyCtrlR:
		go a.reload()
		return
	}

	// Alt+Left = back, Alt+Right = forward.
	if ev.Modifiers()&tcell.ModAlt != 0 {
		switch ev.Key() {
		case tcell.KeyLeft:
			go a.back()
			return
		case tcell.KeyRight:
			go a.forward()
			return
		}
	}

	// Pass to UIApp (handles widget focus/tab/etc.).
	a.UIApp.HandleKey(ev)
}

func (a *BrowseApp) navigate(url string) {
	a.setStatus("Navigating to " + url + "...")
	a.urlBar.Text = url

	if a.tab == nil {
		a.setStatus("No browser tab")
		return
	}

	if err := a.tab.Navigate(url); err != nil {
		a.setStatus("Error: " + err.Error())
		return
	}

	a.refreshDocument()
}

func (a *BrowseApp) refreshDocument() {
	if a.tab == nil {
		return
	}

	doc, err := a.tab.FetchDocument()
	if err != nil {
		a.setStatus("AX tree error: " + err.Error())
		return
	}

	a.mu.Lock()
	a.doc = doc
	if !a.modeForce {
		a.mode = doc.SuggestedMode()
		a.layout.SetMode(a.mode)
	}
	a.mu.Unlock()

	// Update URL bar.
	url, title := a.tab.Location()
	a.urlBar.Text = url
	if title != "" {
		a.UIApp.SetTitle("TexelBrowse - " + title)
	}

	// Map to widgets and update UI.
	widgetList := a.mapper.MapDocument(doc)

	a.mu.Lock()
	a.content = widgetList
	a.mu.Unlock()

	a.relayout()

	interactiveCount := len(doc.InteractiveNodes())
	modeStr := "Reading"
	if a.mode == ModeForm {
		modeStr = "Form"
	}
	a.setStatus(fmt.Sprintf("%s mode | %d elements | %d interactive", modeStr, len(doc.Nodes), interactiveCount))
}

func (a *BrowseApp) relayout() {
	a.mu.Lock()
	ws := a.content
	a.mu.Unlock()

	// Clear old widgets (except URL bar).
	ui := a.UI()
	// Re-add URL bar + content widgets.
	// Note: a more efficient approach would be to diff, but for MVP clear+re-add works.
	ui.ClearWidgets()
	ui.AddWidget(a.urlBar.Input)
	a.urlBar.SetPosition(0, 0)
	a.urlBar.Resize(a.width, 1)

	// Offset content below URL bar.
	a.layout.Arrange(ws)
	for _, w := range ws {
		x, y := w.Position()
		w.SetPosition(x, y+1) // Shift down by 1 for URL bar
		ui.AddWidget(w)
	}
}

func (a *BrowseApp) clickNode(backendNodeID int64) {
	if a.tab == nil {
		return
	}
	if err := a.tab.ClickNode(backendNodeID); err != nil {
		a.setStatus("Click error: " + err.Error())
		return
	}
	// Re-fetch document after click (might cause navigation or DOM change).
	a.refreshDocument()
}

func (a *BrowseApp) typeInNode(backendNodeID int64, text string) {
	if a.tab == nil {
		return
	}
	if err := a.tab.SetValue(backendNodeID, text); err != nil {
		a.setStatus("Type error: " + err.Error())
	}
}

func (a *BrowseApp) back() {
	if a.tab != nil {
		a.tab.Back()
	}
}

func (a *BrowseApp) forward() {
	if a.tab != nil {
		a.tab.Forward()
	}
}

func (a *BrowseApp) reload() {
	if a.tab != nil {
		a.setStatus("Reloading...")
		a.tab.Reload()
		a.refreshDocument()
	}
}

func (a *BrowseApp) toggleMode() {
	a.mu.Lock()
	if a.mode == ModeReading {
		a.mode = ModeForm
	} else {
		a.mode = ModeReading
	}
	a.modeForce = true
	a.layout.SetMode(a.mode)
	a.mu.Unlock()
	a.relayout()
}

func (a *BrowseApp) setStatus(msg string) {
	if a.statusBar != nil {
		a.statusBar.SetCenter(msg)
	}
}

func (a *BrowseApp) SetTitle(title string) {
	a.UIApp.SetTitle(title)
}

func defaultProfileDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, ".texelation", "browser", "profiles", "default")
}
```

**Step 3: Verify compilation**

Run:
```bash
cd /home/marc/projects/texel/texelation && go build ./apps/texelbrowse/...
```
Expected: No errors. (May require adjusting imports based on exact UIManager API — `ClearWidgets` and `SetTitle` may need to be verified or adapted.)

**Step 4: Commit**

```bash
git add apps/texelbrowse/browse.go apps/texelbrowse/commands.go
git commit -m "Wire BrowseApp: URL bar, navigation, AX-to-widget pipeline"
```

---

### Task 10: Standalone binary and Makefile

**Files:**
- Create: `cmd/texelbrowse/main.go`
- Modify: `Makefile` (add texelbrowse to ALL_APPS and build-apps)

**Step 1: Create standalone binary**

Create `cmd/texelbrowse/main.go`:

```go
package main

import (
	"flag"
	"log"
	"strings"

	"github.com/framegrace/texelation/apps/texelbrowse"
	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/runtime"
)

func main() {
	flag.Parse()

	builder := func(args []string) (texelcore.App, error) {
		url := ""
		if len(args) > 0 {
			url = strings.Join(args, " ")
		}
		return texelbrowse.New(url), nil
	}

	if err := runtime.Run(builder, flag.Args()...); err != nil {
		log.Fatalf("texelbrowse: %v", err)
	}
}
```

**Step 2: Add to Makefile**

In `Makefile`, add `texelbrowse` to `ALL_APPS`:
```makefile
ALL_APPS := texelterm help texel-stress texelbrowse
```

Add build line to `build-apps` target:
```makefile
	$(GO_ENV) go build -o $(BIN_DIR)/texelbrowse ./cmd/texelbrowse
```

**Step 3: Verify build**

Run:
```bash
cd /home/marc/projects/texel/texelation && make build-apps
```
Expected: `bin/texelbrowse` is created.

**Step 4: Commit**

```bash
git add cmd/texelbrowse/main.go Makefile
git commit -m "Add standalone texelbrowse binary and Makefile target"
```

---

### Task 11: Register with texelation server

**Files:**
- Modify: `cmd/texel-server/main.go` (add blank import)

**Step 1: Check current server imports**

Read `cmd/texel-server/main.go` to find where app blank imports go.

**Step 2: Add the blank import**

Add to the import block in `cmd/texel-server/main.go`:

```go
_ "github.com/framegrace/texelation/apps/texelbrowse"
```

This triggers `texelbrowse.init()` which calls `registry.RegisterBuiltInProvider`.

**Step 3: Verify build**

Run:
```bash
cd /home/marc/projects/texel/texelation && go build ./cmd/texel-server/
```
Expected: No errors.

**Step 4: Commit**

```bash
git add cmd/texel-server/main.go
git commit -m "Register texelbrowse with texelation server"
```

---

### Task 12: Image widget in texelui (block art MVP)

**Files:**
- Create: `texelui/widgets/image.go`
- Create: `texelui/widgets/image_test.go`

**Step 1: Write test for Image widget**

Create `texelui/widgets/image_test.go`:

```go
package widgets

import (
	"image"
	"image/color"
	"image/png"
	"bytes"
	"testing"

	"github.com/framegrace/texelui/core"
)

func TestImage_FromBytes(t *testing.T) {
	// Create a small test image (4x4 red square).
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	imgWidget := NewImage(buf.Bytes(), "red square")
	imgWidget.Resize(4, 2) // 4 cols, 2 rows (2 pixels per row via half-blocks)

	cellBuf := make([][]core.Cell, 2)
	for y := range cellBuf {
		cellBuf[y] = make([]core.Cell, 4)
	}
	p := core.NewPainter(cellBuf, core.Rect{X: 0, Y: 0, W: 4, H: 2})

	imgWidget.Draw(p)

	// Verify cells are not empty (block art should produce characters).
	nonEmpty := 0
	for _, row := range cellBuf {
		for _, c := range row {
			if c.Ch != 0 && c.Ch != ' ' {
				nonEmpty++
			}
		}
	}
	if nonEmpty == 0 {
		t.Error("expected non-empty block art cells")
	}
}

func TestImage_AltTextFallback(t *testing.T) {
	// Invalid image data should fall back to alt text.
	imgWidget := NewImage([]byte("not an image"), "fallback text")
	imgWidget.Resize(20, 1)

	cellBuf := make([][]core.Cell, 1)
	cellBuf[0] = make([]core.Cell, 20)
	p := core.NewPainter(cellBuf, core.Rect{X: 0, Y: 0, W: 20, H: 1})

	imgWidget.Draw(p)

	text := ""
	for _, c := range cellBuf[0] {
		if c.Ch != 0 {
			text += string(c.Ch)
		}
	}
	expected := "[img: fallback text]"
	if text != expected {
		t.Errorf("expected %q, got %q", expected, text)
	}
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
go test ./texelui/widgets/ -run TestImage -v
```
Expected: FAIL — `NewImage` not defined.

**Step 3: Implement Image widget with block art**

Create `texelui/widgets/image.go`:

```go
package widgets

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
	"github.com/gdamore/tcell/v2"
	_ "golang.org/x/image/webp"
)

// Image renders an image using half-block characters for block art.
// Each cell represents two vertical pixels using the upper-half-block character (▀),
// with the foreground color for the top pixel and background for the bottom pixel.
type Image struct {
	core.BaseWidget
	imgData []byte
	altText string
	decoded image.Image
	valid   bool
	style   tcell.Style
	inv     func(core.Rect)
}

func NewImage(imgData []byte, altText string) *Image {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")

	img := &Image{
		imgData: imgData,
		altText: altText,
		style:   tcell.StyleDefault.Foreground(fg).Background(bg),
	}
	img.SetFocusable(false)

	// Try to decode the image.
	decoded, _, err := image.Decode(bytes.NewReader(imgData))
	if err == nil {
		img.decoded = decoded
		img.valid = true
	}

	return img
}

func (img *Image) Draw(p *core.Painter) {
	x, y := img.Position()
	w, h := img.Size()

	if !img.valid {
		// Fallback to alt text.
		text := fmt.Sprintf("[img: %s]", img.altText)
		runes := []rune(text)
		for i, r := range runes {
			if i >= w {
				break
			}
			p.SetCell(x+i, y, r, img.style)
		}
		return
	}

	// Scale image to fit widget area.
	// Each cell = 1 column width, 2 pixel rows (using half-block).
	imgBounds := img.decoded.Bounds()
	imgW := imgBounds.Dx()
	imgH := imgBounds.Dy()

	// Pixels available: w columns, h*2 pixel rows.
	pixW := w
	pixH := h * 2

	for cy := 0; cy < h; cy++ {
		for cx := 0; cx < w; cx++ {
			// Map cell position to image coordinates.
			topPixY := cy * 2
			botPixY := cy*2 + 1

			topColor := img.sampleColor(cx, topPixY, pixW, pixH, imgW, imgH)
			botColor := img.sampleColor(cx, botPixY, pixW, pixH, imgW, imgH)

			// Upper-half-block: fg = top pixel, bg = bottom pixel.
			style := tcell.StyleDefault.
				Foreground(tcell.NewRGBColor(int32(topColor.R), int32(topColor.G), int32(topColor.B))).
				Background(tcell.NewRGBColor(int32(botColor.R), int32(botColor.G), int32(botColor.B)))

			p.SetCell(x+cx, y+cy, '▀', style)
		}
	}
}

type rgb struct{ R, G, B uint8 }

func (img *Image) sampleColor(cx, py, pixW, pixH, imgW, imgH int) rgb {
	// Map pixel position to image coordinate with nearest-neighbor scaling.
	imgX := cx * imgW / pixW
	imgY := py * imgH / pixH

	if imgX >= imgW {
		imgX = imgW - 1
	}
	if imgY >= imgH {
		imgY = imgH - 1
	}

	bounds := img.decoded.Bounds()
	r, g, b, _ := img.decoded.At(bounds.Min.X+imgX, bounds.Min.Y+imgY).RGBA()
	return rgb{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}

func (img *Image) SetInvalidator(fn func(core.Rect)) { img.inv = fn }
```

Note: The `golang.org/x/image/webp` import may need to be added to `go.mod` if WebP support is desired. For MVP, remove that import if it causes issues and just support PNG/JPEG/GIF.

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./texelui/widgets/ -run TestImage -v
```
Expected: PASS. (May need to remove the webp import if not in go.mod.)

**Step 5: Commit**

```bash
git add texelui/widgets/image.go texelui/widgets/image_test.go
git commit -m "Add Image widget with block art rendering"
```

---

### Task 13: Integration test — end-to-end navigation

**Files:**
- Create: `apps/texelbrowse/integration_test.go`

**Step 1: Write end-to-end integration test**

Create `apps/texelbrowse/integration_test.go`:

```go
package texelbrowse

import (
	"os"
	"testing"
	"time"
)

func TestIntegration_NavigateAndRender(t *testing.T) {
	if os.Getenv("TEXELBROWSE_INTEGRATION") == "" {
		t.Skip("set TEXELBROWSE_INTEGRATION=1 to run browser tests")
	}

	profileDir := t.TempDir()
	engine, err := NewEngine(profileDir)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	tab, err := engine.NewTab()
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}

	// Navigate to example.com.
	if err := tab.Navigate("https://example.com"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	// Fetch document.
	doc, err := tab.FetchDocument()
	if err != nil {
		t.Fatalf("FetchDocument: %v", err)
	}

	t.Logf("Page: %s (%s)", doc.URL, doc.Title)
	t.Logf("Nodes: %d, Mode: %v", len(doc.Nodes), doc.SuggestedMode())

	// Map to widgets.
	mapper := NewMapper(nil)
	widgets := mapper.MapDocument(doc)
	t.Logf("Widgets: %d", len(widgets))

	if len(widgets) == 0 {
		t.Error("expected at least one widget from example.com")
	}

	// Layout.
	layout := NewLayoutManager(80, 24)
	layout.SetMode(doc.SuggestedMode())
	layout.Arrange(widgets)

	// Verify widgets have valid positions.
	for i, w := range widgets {
		x, y := w.Position()
		if x < 0 || y < 0 {
			t.Errorf("widget %d has negative position: (%d, %d)", i, x, y)
		}
	}

	// Navigate to a data URI with a form.
	formHTML := `data:text/html,<html><body>
		<h1>Login</h1>
		<form>
			<input type="text" aria-label="Username">
			<input type="password" aria-label="Password">
			<button type="submit">Sign In</button>
		</form>
	</body></html>`

	if err := tab.Navigate(formHTML); err != nil {
		t.Fatalf("Navigate form: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	doc, err = tab.FetchDocument()
	if err != nil {
		t.Fatalf("FetchDocument form: %v", err)
	}

	t.Logf("Form page: %d nodes, mode: %v", len(doc.Nodes), doc.SuggestedMode())

	// Should detect as form mode.
	if doc.SuggestedMode() != ModeForm {
		t.Logf("Warning: expected form mode but got reading mode (ratio may be off)")
	}
}
```

**Step 2: Run integration test**

Run:
```bash
TEXELBROWSE_INTEGRATION=1 go test ./apps/texelbrowse/ -run TestIntegration -v -timeout 60s
```
Expected: PASS.

**Step 3: Commit**

```bash
git add apps/texelbrowse/integration_test.go
git commit -m "Add end-to-end integration test for texelbrowse"
```

---

### Task 14: Manual smoke test

**Step 1: Build the standalone binary**

Run:
```bash
cd /home/marc/projects/texel/texelation && make build-apps
```

**Step 2: Run standalone texelbrowse**

Run:
```bash
./bin/texelbrowse https://example.com
```

**Step 3: Verify**

Check that:
- URL bar shows `https://example.com`
- Page content is rendered as text (heading, paragraphs, link)
- Tab cycles between link elements
- Enter on a link navigates
- Ctrl+L focuses URL bar
- Ctrl+M toggles mode
- Status bar shows mode and element count

**Step 4: Test OIDC flow (if available)**

Run:
```bash
./bin/texelbrowse https://accounts.google.com
```

Check that:
- Login form is rendered with Input widgets
- Can type email/password
- Submit button works
- Redirects are handled

**Step 5: Final commit**

If any fixes were needed during smoke testing, commit them:
```bash
git add -A
git commit -m "Fix issues found during smoke testing"
```

---

## Implementation Notes

### Adapting to UIManager API

The plan assumes certain UIManager methods exist:
- `ClearWidgets()` — If not present, you may need to implement a `RemoveWidget` approach or rebuild the UIManager.
- `SetTitle()` on UIApp — Verify this exists. The adapter has `title` field set in constructor; you may need to add a setter.
- `SetOnResize()` on UIApp — Exists in adapter per exploration results.

If any of these are missing, add them to texelui as small methods before proceeding.

### Testing Strategy

- **Unit tests** (Tasks 3, 6, 7, 8): Run without Chromium, use synthetic AX nodes and document models.
- **Integration tests** (Tasks 2, 4, 5, 13): Require `TEXELBROWSE_INTEGRATION=1` and Chromium installed. Guarded by env var skip.
- **Smoke test** (Task 14): Manual verification of the full pipeline.

### Dependency on texelui

Tasks 6 and 12 create new widgets in `texelui/`. These should be committed on a branch in the texelui repo first, then the texelation go.mod updated to point to the new version. Alternatively, if using `replace` directives for local development, this is handled automatically.
