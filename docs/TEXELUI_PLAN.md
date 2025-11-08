# TexelUI Plan

This document tracks the design and implementation plan for TexelUI — a reusable, text‑only UI library intended to run inside TexelApps (and other tcell‑based apps). Keep this file up to date as work progresses.

Last updated: 2025-11-08

## Goals
- Provide a clean widget kernel that can be embedded in any TexelApp/pane.
- Start with floating widgets; enable future layout managers without rewrites.
- Strong focus and input routing; themeable styling consistent with the rest of Texelation.
- Prefer composition over inheritance (idiomatic Go): simple, testable building blocks.

## Architecture
- Widget (interface)
  - Lifecycle: `Resize(w,h)`, `Draw(painter)`, `SetVisible/Enabled`, `Focus/Blur`
  - Input: `HandleKey`, `HandleMouse` (with capture), `HitTest(x,y)`
  - Sizing: `PreferredSize()` (optional, for layout managers)
- Container (interface)
  - Child management; coordinates layout and clipping; Z‑order for floating
- UIManager (root per UI tree)
  - Owns focus manager, event routing, redraw scheduling, theme propagation
- Style/Theme
  - fg/bg/bold/underline/reverse; integrated with project theme keys; widget‑level overrides
- Painter
  - Minimal draw API over a cell buffer with clipping (fills, text, borders)

## Event & Focus Model
- Mouse: Z‑ordered hit‑testing; optional mouse capture during drags until release/cancel
- Keyboard: goes to focused widget; bubble to parents if unhandled
- Focus manager: click‑to‑focus; Tab/Shift‑Tab traversal; programmatic `FocusNext/Prev/SetFocused`

## Rendering
- MVP may full‑redraw; plan for damage/dirty rectangles later
- Reuse buffers and styles to minimize allocations

## MVP Deliverables
1. Core (texelui/core)
   - Widget, Container, BaseWidget
   - UIManager (focus, routing, redraw), Painter, Style/Theme glue
2. Layout (texelui/layout)
   - Absolute (floating) layout; hooks for future managers
3. Widgets (texelui/widgets)
   - Pane: area fill with fg/bg; exposes client rect
   - Border (decorator): style + charset; computes inner client rect
   - TextArea: multiline, scrollable text editor (see scope below)
4. Adapter
   - Host a UIManager inside a TexelApp/pane (Resize/Render/Key/Mouse wiring)
5. Tests & Docs

## TextArea v1 Scope
- Editing: insert, backspace, delete, enter (newline)
- Navigation: arrows, Home/End, Ctrl+Left/Right (word), Ctrl+Home/End (doc)
- Selection: Shift+arrows; mouse drag selection; clipboard copy/cut/paste via existing hooks
- Scrolling: caret kept visible; mouse wheel vertical scroll
- Rendering: no reflow; visual wrap only in viewport; selection colors from theme
- Later: undo/redo, IME support, word‑wrap mode, soft tabs, syntax helpers

## Future Layout Managers (planned interfaces)
- Layout interface: `Measure(SizeConstraints) -> Size`, `Arrange(Rect)`
- Tiling layout backed by our current tree model (future)
- Flex/Grid managers

## Integration Strategy
- `TexelUIAdapter` mounts a UI tree within a TexelApp: forwards events, composes into pane buffer
- Theme keys: `ui.surface.*`, `ui.border.*`, `ui.text.*` (defaults from global theme; override per widget)

## Testing Strategy
- Core: hit‑testing, focus traversal, clipping, painting assertions on buffers
- TextArea: editing/navigation/selection invariants; viewport scrolling; clipboard round‑trip (when available)
- Adapter: render pipeline under fake screen driver

## Phased Execution
1. Scaffold core package and interfaces
2. Implement UIManager + Painter + Style glue
3. Add Pane widget
4. Add Border decorator
5. Implement TextArea (MVP)
6. Build TexelUIAdapter and sample app card
7. Add tests and quickstart docs
8. Iterate on performance (damage tracking), then layout managers

## Status / Progress
- [x] Planning document created (this file)
- [x] Core package scaffolded
- [x] UIManager + Painter + Style glue
- [x] Pane widget
- [x] Border decorator
- [x] TextArea (MVP)
- [x] Adapter integration into a sample TexelApp
- [x] Minimal tests for render pipeline
- [x] Dirty-region rendering (initial, coarse merge of regions)
- [x] Layout manager interface (Absolute)
- [x] Focus traversal (Tab/Shift-Tab), click-to-focus hit-testing
- [x] Mouse handling with capture for drags (basic)
- [x] Clipboard integration for TextArea (local copy/cut/paste)
- [x] Damage tracking improvements (rect merging; multi-clip redraw)
- [x] Quickstart doc for demo and embedding
- [ ] Per-widget invalidation API (plumb widget-driven dirty regions)
- [ ] Benchmarks for redraw cost (typing and selection)
- [ ] Cursor blink timer and IME hooks

Maintenance note: Update this checklist and sections whenever work lands. Commit this file alongside related code changes.
