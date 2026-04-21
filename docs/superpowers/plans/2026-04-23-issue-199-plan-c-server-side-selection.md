# Issue #199 — Plan C: Server-Side Selection / Copy (STUB)

**Status:** Stub — write the full TDD plan after Plan A lands.
**Depends on:** Plan A merged (needs globalIdx-based addressing).
**Can ship independently of Plan B.**
**Spec reference:** Sub-problem 1 + spec sequencing step 6.

## Goal

Selection works across the entire scrollback — including cold pages and rows never cached on the client. Mosh-style silent wrong-byte grabs become structurally impossible.

## Scope

- Client owns live selection state `{anchor, head, mode}` in the current pane's coordinate space.
  - Main screen: `(globalIdx, col)`.
  - Alt screen: `(screenRow, col)`.
- New server RPCs:
  - `MsgResolveBoundary { paneID, pos, direction, mode } → MsgResolveBoundaryResponse { resolved | unsupported }`.
  - `MsgCaptureSelection { paneID, coordSpace, anchor, head, mode, formats } → MsgCaptureResult { entryID, formats }`.
- Modes: `word`, `line`, `paragraph`, `logical-line`, `prompt`, `prompt-output`.
  - Prompt modes use OSC 133 anchors; alt-screen returns `unsupported`, client greys out binding.
- `CaptureSelection` walks the range (faulting cold pages), soft-joins wrapped rows, CRLF on hard breaks, trims trailing blanks.
- Formats supported day one: `plain`, `ansi`.
- Copy stack: server-scoped in-memory `[]CopyEntry{entryID, timestamp, meta}` seeded by `CaptureSelection`. Protocol surface area for stack operations (`ListStack`, `GetStackEntry`, `TransformStackEntry`, `DeleteStackEntry`) deferred — scaffolded via stable `entryID`.
- Mid-drag alt-screen transition cancels selection.

## Out of scope

- Copy stack UI / transforms (modal colorize-reformat card) — separate follow-up.
- Multi-entry clipboard integration — separate follow-up.

## Files touched (estimated)

- `protocol/messages.go` + new test files for the three messages.
- `internal/runtime/server/selection_handler.go` + tests — resolves modes, walks ranges, faults pages via `PageStore`.
- `internal/runtime/server/copy_stack.go` + tests — in-memory entries.
- `apps/texelterm/parser/sparse/store.go` — accessor for OSC 133 anchor map (if not already exposed).
- `internal/runtime/client/selection.go` — migrate from screen-row to `(globalIdx, col)` / `(screenRow, col)`.
- `internal/runtime/client/input.go` — mouse/keyboard → `ResolveBoundary` + `CaptureSelection` wiring.
- Integration tests: cross-scrollback select + copy returning correct bytes.

## Test checklist

- `ResolveBoundary` word/line/paragraph from sparse store.
- `ResolveBoundary` prompt/prompt-output against OSC 133 anchors.
- `ResolveBoundary` returns `unsupported` on alt-screen for prompt modes.
- `CaptureSelection` walks wrapped rows with soft-join, hard break CRLF, trailing-blank trim.
- `CaptureSelection` loads cold pages when selection straddles on-disk data.
- `CaptureSelection` returns both `plain` and `ansi` formats.
- Mid-drag alt-screen transition cancels selection on the client.
- `entryID` is stable and addressable (list before-after capture).
