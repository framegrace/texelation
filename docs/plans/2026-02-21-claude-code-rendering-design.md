# Claude Code Rendering Bug Investigation

**Date**: 2026-02-21

## Problem

Claude Code CLI (v2.1.50) renders incorrectly in texelterm. The welcome screen shows:
- Extra `│` characters in the bottom status bar's narrow right sections
- Possible missing outer box borders
- Quadrant block elements (`▌▌ ▌▌`) appearing as artifacts above the status bar

Other TUI apps (htop, vim) render correctly. Codex also works fine on primary screen.

## Context

Claude Code uses a unique rendering approach:
- **React + Ink** with a custom differential renderer (not Ink's default)
- Renders on the **primary screen** (not alt screen) for scrollback support
- Uses **DEC Private Mode 2026** (Synchronized Output) to batch frame updates
- Uses **cursor positioning** (CSI H) to write only changed cells between frames
- Box-drawing via **Unicode characters** (U+2500-U+257F), not DEC line-drawing mode
- Character widths via `string-width` npm package

Texelterm already supports mode 2026 and primary screen rendering (proven by Codex).

## Approach: Capture & Replay

Replicate the proven Codex debugging workflow.

### Step 1: Capture raw output

Run `script -q claude-code-capture.log -c "claude"` from `~`, then exit immediately to capture the welcome screen frames.

### Step 2: Create replay test

Build `TestClaudeCodeRenderPipelineTrace` (following `TestCodexRenderPipelineTrace` pattern):
- Load capture file
- Replay through VTerm parser at captured terminal dimensions
- Extract grid state after final synchronized update frame (ESC[?2026l boundary)

### Step 3: Analyze grid

Examine grid at problematic positions:
- Bottom status bar rows: locate extra `│` characters, identify occupied columns
- Right-side narrow sections: check what characters exist between box borders
- Outer box borders: verify presence at expected positions

### Step 4: Trace root cause

For each grid cell that differs from expected (reference terminal):
- Find the escape sequence responsible
- Identify parser behavior: missing sequence, wrong width, wrong position, or failed erase

### Step 5: Fix and test

Implement parser fix with regression test using the captured data.

## Likely Root Causes (Hypotheses)

1. **Missing escape sequence**: Claude Code's Ink renderer uses a sequence texelterm doesn't handle (e.g., OSC 8 hyperlinks, specific erase variant)
2. **Character width mismatch**: `string-width` (JS) and `go-runewidth` disagree on width of specific characters used in status bar icons
3. **Differential rendering edge case**: Overwriting cells on primary screen via CUP doesn't work correctly in some specific pattern Claude Code uses
4. **Capability query response**: Texelterm's response to DA/DECRQM queries causes Claude Code to choose a rendering path with unsupported features

## Deliverables

- `testdata/claude-code-session.txt` — captured raw terminal output
- `TestClaudeCodeRenderPipelineTrace` — replay and grid analysis test
- Parser fix(es) with regression tests
