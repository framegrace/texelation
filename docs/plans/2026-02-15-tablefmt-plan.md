# Table Formatter Plugin Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract table formatting from txfmt into a standalone tablefmt transformer plugin that detects and box-draws tabular content (Markdown, space-aligned, pipe-separated, CSV/TSV) with confidence gating and per-table buffering.

**Architecture:** New `apps/texelterm/tablefmt/` package with a state machine (SCANNING → BUFFERING → FLUSHING). Uses a new `LineSuppressor` interface to buffer lines silently, then emits formatted output on flush. Runs before txfmt in the pipeline. Each table type has its own detector with independent confidence scoring.

**Tech Stack:** Go 1.24.3, `parser.LogicalLine`/`parser.Cell` types, `transformer.Register` plugin system, box-drawing Unicode characters.

---

### Task 1: Add LineSuppressor Interface and Pipeline Return Value

Modify the transformer pipeline to support line suppression. This is the foundation that tablefmt needs.

**Files:**
- Modify: `apps/texelterm/transformer/transformer.go:18-88`
- Modify: `apps/texelterm/transformer/transformer_test.go:82-99`
- Modify: `apps/texelterm/parser/vterm.go:86`
- Modify: `apps/texelterm/parser/vterm_memory_buffer.go:607-614`
- Modify: `apps/texelterm/term.go:1385`

**Step 1: Write failing test for LineSuppressor in pipeline**

Add to `apps/texelterm/transformer/transformer_test.go`:

```go
// suppressingTransformer suppresses lines with even lineIdx.
type suppressingTransformer struct {
	stubTransformer
	lastSuppressed int64
}

func (s *suppressingTransformer) HandleLine(lineIdx int64, line *parser.LogicalLine, isCommand bool) {
	s.stubTransformer.HandleLine(lineIdx, line, isCommand)
	s.lastSuppressed = lineIdx
}

func (s *suppressingTransformer) ShouldSuppress(lineIdx int64) bool {
	return lineIdx%2 == 0 // suppress even lines
}

func TestPipelineSuppression(t *testing.T) {
	sup := &suppressingTransformer{}
	after := &stubTransformer{id: "after"}
	p := &Pipeline{transformers: []Transformer{sup, after}}

	line := &parser.LogicalLine{}

	// Even lineIdx → suppressed, "after" should NOT see it
	suppressed := p.HandleLine(0, line, true)
	if !suppressed {
		t.Error("expected HandleLine to return true (suppressed) for even lineIdx")
	}
	if after.handleCalls != 0 {
		t.Errorf("expected 'after' to not be called for suppressed line, got %d calls", after.handleCalls)
	}

	// Odd lineIdx → not suppressed, "after" should see it
	suppressed = p.HandleLine(1, line, true)
	if suppressed {
		t.Error("expected HandleLine to return false (not suppressed) for odd lineIdx")
	}
	if after.handleCalls != 1 {
		t.Errorf("expected 'after' to be called once, got %d calls", after.handleCalls)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/transformer/ -run TestPipelineSuppression -v`
Expected: Compilation error — `HandleLine` doesn't return `bool`, `ShouldSuppress` not defined.

**Step 3: Implement LineSuppressor and Pipeline changes**

In `apps/texelterm/transformer/transformer.go`:

1. Add the `LineSuppressor` interface after `LineInserter` (after line 30):

```go
// LineSuppressor is an optional interface that transformers can implement
// to consume a line, preventing further pipeline processing and scrollback
// persistence. Used by buffering transformers like tablefmt.
type LineSuppressor interface {
	ShouldSuppress(lineIdx int64) bool
}
```

2. Change `HandleLine` signature and body (lines 84-88):

```go
// HandleLine dispatches to each transformer in order.
// Returns true if the line was suppressed (should not be persisted).
func (p *Pipeline) HandleLine(lineIdx int64, line *parser.LogicalLine, isCommand bool) bool {
	for _, t := range p.transformers {
		t.HandleLine(lineIdx, line, isCommand)
		if sup, ok := t.(LineSuppressor); ok && sup.ShouldSuppress(lineIdx) {
			return true
		}
	}
	return false
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/transformer/ -run TestPipelineSuppression -v`
Expected: PASS

**Step 5: Fix compilation in callers**

Update `apps/texelterm/parser/vterm.go` line 86 — change `OnLineCommit` field type:

```go
OnLineCommit func(lineIdx int64, line *LogicalLine, isCommand bool) bool
```

Update `apps/texelterm/parser/vterm.go` line 1256 — `WithLineCommitHandler` parameter type:

```go
func WithLineCommitHandler(handler func(int64, *LogicalLine, bool) bool) Option {
```

Update `apps/texelterm/parser/vterm_memory_buffer.go` lines 607-614 — handle the return value:

```go
if v.OnLineCommit != nil {
	v.commitInsertOffset = 0
	if line := v.memBufState.memBuf.GetLine(currentGlobal); line != nil {
		suppressed := v.OnLineCommit(currentGlobal, line, v.CommandActive)
		if suppressed {
			// Line was consumed by a transformer (e.g. tablefmt buffering).
			// Remove it from the memory buffer so it doesn't enter scrollback.
			v.memBufState.memBuf.DeleteLine(currentGlobal)
			// Don't adjust currentGlobal for inserts since the line is removed.
			return
		}
	}
	// Adjust for any lines inserted by the callback via RequestLineInsert.
	currentGlobal += v.commitInsertOffset
}
```

Update `apps/texelterm/term.go` line 1385 — assignment is fine since `Pipeline.HandleLine` now returns bool, matching the new callback type.

**Step 6: Check if MemoryBuffer has DeleteLine, or if we need a different approach**

We need to check if `MemoryBuffer.DeleteLine` exists. If not, an alternative approach: instead of deleting the line, we can clear its cells. The line stays in the buffer but is empty — effectively invisible. This avoids needing a new MemoryBuffer API.

Actually, the simpler approach: when a line is suppressed, we don't need to delete it from the buffer. We can just **not persist it** and **not adjust** currentGlobal. The line exists momentarily in the memBuf but gets overwritten on the next lineFeed since liveEdgeBase hasn't advanced. Let's verify this by looking at the `memoryBufferLineFeed` flow more carefully.

The cleanest approach: when `OnLineCommit` returns true (suppressed), skip the persistence notification (the `NotifyWriteWithMeta` call) and the `currentGlobal` adjustment. The line still exists in the memBuf grid at that position, but it won't be flushed to disk and will be overwritten when the next line arrives. This is the minimal change:

```go
if v.OnLineCommit != nil {
	v.commitInsertOffset = 0
	if line := v.memBufState.memBuf.GetLine(currentGlobal); line != nil {
		if v.OnLineCommit(currentGlobal, line, v.CommandActive) {
			// Line suppressed by transformer — don't persist or advance.
			return
		}
	}
	currentGlobal += v.commitInsertOffset
}
```

Wait — but `memoryBufferLineFeed` has already advanced `liveEdgeBase` by this point. We need to understand the full flow. Let me document what happens:

The actual approach depends on whether liveEdgeBase has already advanced when OnLineCommit fires. Read the code carefully and adapt. The key contract: if suppressed, the line should not enter scrollback. The simplest way: clear the line's cells after suppression, so even if it's persisted, it's an empty line. Then the transformer will re-emit the content via insertFunc later.

For the plan: implement the suppression in a way that the suppressed line's cells are cleared after the callback returns true. This is safe because tablefmt already cloned the line data in its buffer.

**Step 7: Run full test suite to verify no regressions**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/... -count=1`
Expected: All pass (txfmt's `HandleLine` doesn't return bool — needs updating too).

**Step 8: Update txfmt HandleLine to match new interface**

The `Transformer` interface's `HandleLine` does NOT return bool — only `Pipeline.HandleLine` returns bool. The `Transformer.HandleLine` signature stays the same. The suppression is via the optional `LineSuppressor` interface. So txfmt doesn't need changes here.

Verify: `go build ./apps/texelterm/...`

**Step 9: Commit**

```bash
git add apps/texelterm/transformer/transformer.go apps/texelterm/transformer/transformer_test.go apps/texelterm/parser/vterm.go apps/texelterm/parser/vterm_memory_buffer.go apps/texelterm/term.go
git commit -m "Add LineSuppressor interface to transformer pipeline"
```

---

### Task 2: Create tablefmt Package Skeleton with Registration

Create the package, struct, and register it. No detection logic yet — just the state machine skeleton that passes lines through.

**Files:**
- Create: `apps/texelterm/tablefmt/tablefmt.go`
- Create: `apps/texelterm/tablefmt/tablefmt_test.go`
- Modify: `apps/texelterm/term.go:31-32` (add import)

**Step 1: Write failing test for registration**

Create `apps/texelterm/tablefmt/tablefmt_test.go`:

```go
package tablefmt

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/transformer"
)

func TestRegistration(t *testing.T) {
	_, ok := transformer.Lookup("tablefmt")
	if !ok {
		t.Fatal("tablefmt should be registered via init()")
	}
}

func TestPassThrough_Scanning(t *testing.T) {
	// In SCANNING state with non-table content, lines pass through untouched.
	f := New(1000)
	f.NotifyPromptStart()

	line := makeCells("hello world this is plain text")
	f.HandleLine(0, line, true)

	// Should NOT be suppressed (not table-like)
	if f.ShouldSuppress(0) {
		t.Error("expected plain text to not be suppressed")
	}
}

func makeCells(s string) *parser.LogicalLine {
	cells := make([]parser.Cell, len([]rune(s)))
	for i, r := range []rune(s) {
		cells[i] = parser.Cell{Rune: r, FG: parser.DefaultFG, BG: parser.DefaultBG}
	}
	return &parser.LogicalLine{Cells: cells}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run TestRegistration -v`
Expected: Compilation error — package doesn't exist.

**Step 3: Create the tablefmt package**

Create `apps/texelterm/tablefmt/tablefmt.go`:

```go
package tablefmt

import (
	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/transformer"
)

func init() {
	transformer.Register("tablefmt", func(cfg transformer.Config) (transformer.Transformer, error) {
		maxRows := 1000
		if v, ok := cfg["max_buffer_rows"].(float64); ok && v > 0 {
			maxRows = int(v)
		}
		return New(maxRows), nil
	})
}

type state int

const (
	stateScanning  state = iota
	stateBuffering
)

// bufferedLine stores a cloned line and its global index for deferred processing.
type bufferedLine struct {
	lineIdx int64
	line    *parser.LogicalLine
}

// TableFormatter detects and renders tabular content with box-drawing borders.
type TableFormatter struct {
	state              state
	buffer             []*bufferedLine
	maxBufferRows      int
	lastSuppressedIdx  int64
	suppressed         bool
	wasCommand         bool
	hasShellIntegration bool
	insertFunc         func(beforeIdx int64, cells []parser.Cell)
}

// New creates a new TableFormatter with the given buffer row limit.
func New(maxBufferRows int) *TableFormatter {
	return &TableFormatter{
		maxBufferRows: maxBufferRows,
	}
}

// SetInsertFunc implements transformer.LineInserter.
func (tf *TableFormatter) SetInsertFunc(fn func(beforeIdx int64, cells []parser.Cell)) {
	tf.insertFunc = fn
}

// NotifyPromptStart records that shell integration is active.
func (tf *TableFormatter) NotifyPromptStart() {
	tf.hasShellIntegration = true
}

// HandleLine processes each committed line through the state machine.
func (tf *TableFormatter) HandleLine(lineIdx int64, line *parser.LogicalLine, isCommand bool) {
	tf.suppressed = false

	effectiveIsCommand := isCommand || !tf.hasShellIntegration

	// Flush buffer on command→prompt transition.
	if tf.wasCommand && !effectiveIsCommand {
		tf.flush()
	}
	tf.wasCommand = effectiveIsCommand

	if !effectiveIsCommand {
		return
	}

	switch tf.state {
	case stateScanning:
		// TODO: test line against detectors, transition to BUFFERING if match
		// For now: pass through everything
	case stateBuffering:
		// TODO: accumulate or flush
	}
}

// ShouldSuppress implements transformer.LineSuppressor.
func (tf *TableFormatter) ShouldSuppress(lineIdx int64) bool {
	return tf.suppressed
}

// flush emits all buffered lines. For now, just clears the buffer.
func (tf *TableFormatter) flush() {
	// TODO: evaluate confidence, render or pass through
	tf.buffer = tf.buffer[:0]
	tf.state = stateScanning
}
```

**Step 4: Add import to term.go**

In `apps/texelterm/term.go`, add tablefmt import alongside the txfmt import (around line 32):

```go
	// Import txfmt for init() side-effect registration.
	_ "github.com/framegrace/texelation/apps/texelterm/txfmt"
	// Import tablefmt for init() side-effect registration.
	_ "github.com/framegrace/texelation/apps/texelterm/tablefmt"
```

**Step 5: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add apps/texelterm/tablefmt/ apps/texelterm/term.go
git commit -m "Add tablefmt package skeleton with registration"
```

---

### Task 3: Markdown Table Detector

Implement the first and highest-priority detector: Markdown tables (GFM).

**Files:**
- Create: `apps/texelterm/tablefmt/detect.go`
- Create: `apps/texelterm/tablefmt/detect_test.go`

**Step 1: Write failing tests for MD detection**

Create `apps/texelterm/tablefmt/detect_test.go`:

```go
package tablefmt

import "testing"

func TestMarkdownDetector_BasicTable(t *testing.T) {
	d := &markdownDetector{}
	lines := []string{
		"| Name  | Age | City     |",
		"| ----- | --- | -------- |",
		"| Alice | 30  | New York |",
		"| Bob   | 25  | London   |",
	}
	score := d.Score(lines)
	if score < 0.95 {
		t.Errorf("expected score >= 0.95 for basic MD table, got %f", score)
	}
}

func TestMarkdownDetector_GFMAlignment(t *testing.T) {
	d := &markdownDetector{}
	lines := []string{
		"| Left | Center | Right |",
		"|:-----|:------:|------:|",
		"| a    |   b    |     c |",
	}
	score := d.Score(lines)
	if score < 0.95 {
		t.Errorf("expected score >= 0.95 for GFM aligned table, got %f", score)
	}
}

func TestMarkdownDetector_NotATable(t *testing.T) {
	d := &markdownDetector{}
	lines := []string{
		"This is just some text",
		"with no table structure",
		"at all in the content",
	}
	score := d.Score(lines)
	if score > 0 {
		t.Errorf("expected score 0 for non-table, got %f", score)
	}
}

func TestMarkdownDetector_NoSeparatorRow(t *testing.T) {
	d := &markdownDetector{}
	lines := []string{
		"| Name  | Age |",
		"| Alice | 30  |",
		"| Bob   | 25  |",
	}
	// Without separator row, this is pipe-separated, not MD
	score := d.Score(lines)
	if score >= 0.95 {
		t.Errorf("expected score < 0.95 without separator row, got %f", score)
	}
}

func TestMarkdownDetector_Compatible(t *testing.T) {
	d := &markdownDetector{}
	if !d.Compatible("| foo | bar |") {
		t.Error("expected pipe-containing line to be compatible")
	}
	if d.Compatible("plain text without pipes") {
		t.Error("expected line without pipes to be incompatible")
	}
	if !d.Compatible("") {
		t.Error("expected blank line to be compatible")
	}
}

func TestMarkdownDetector_Parse(t *testing.T) {
	d := &markdownDetector{}
	lines := []string{
		"| Name  | Age | City     |",
		"|:------|----:|:--------:|",
		"| Alice | 30  | New York |",
		"| Bob   | 25  | London   |",
	}
	ts := d.Parse(lines)
	if ts == nil {
		t.Fatal("expected non-nil tableStructure")
	}
	if len(ts.columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(ts.columns))
	}
	// Check alignment from GFM separator
	if ts.columns[0].align != alignLeft {
		t.Errorf("col 0: expected left align, got %d", ts.columns[0].align)
	}
	if ts.columns[1].align != alignRight {
		t.Errorf("col 1: expected right align, got %d", ts.columns[1].align)
	}
	if ts.columns[2].align != alignCenter {
		t.Errorf("col 2: expected center align, got %d", ts.columns[2].align)
	}
	// Check header row
	if ts.headerRow != 0 {
		t.Errorf("expected header at row 0, got %d", ts.headerRow)
	}
	// Check data rows (separator row should be excluded)
	if len(ts.rows) != 3 { // header + 2 data rows
		t.Errorf("expected 3 rows (header + 2 data), got %d", len(ts.rows))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run TestMarkdownDetector -v`
Expected: Compilation error — types don't exist.

**Step 3: Implement detector types and markdown detector**

Create `apps/texelterm/tablefmt/detect.go`:

```go
package tablefmt

import (
	"regexp"
	"strings"
)

// alignment represents column alignment.
type alignment int

const (
	alignLeft   alignment = iota
	alignRight
	alignCenter
)

// columnInfo describes a detected column.
type columnInfo struct {
	align alignment
}

// tableStructure is the parsed result of a detected table.
type tableStructure struct {
	columns   []columnInfo
	headerRow int      // index of header row in rows (-1 if none)
	rows      [][]string // cell values per row (includes header)
	tableType tableType
}

// tableType identifies the detected table format.
type tableType int

const (
	tableNone         tableType = iota
	tableMarkdown
	tablePipeSeparated
	tableSpaceAligned
	tableCSV
)

// tableDetector scores and parses lines for a specific table format.
type tableDetector interface {
	Score(lines []string) float64
	Parse(lines []string) *tableStructure
	Compatible(line string) bool
}

// --- Markdown detector ---

var reMDSeparator = regexp.MustCompile(`^\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$`)

type markdownDetector struct{}

func (d *markdownDetector) Score(lines []string) float64 {
	if len(lines) < 2 {
		return 0
	}
	// Need at least one line with | and a separator row.
	hasPipes := false
	hasSeparator := false
	for _, ln := range lines {
		if strings.Contains(ln, "|") {
			hasPipes = true
		}
		if reMDSeparator.MatchString(strings.TrimSpace(ln)) {
			hasSeparator = true
		}
	}
	if !hasPipes || !hasSeparator {
		return 0
	}
	// Verify consistent column count across pipe-containing lines.
	colCounts := map[int]int{}
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if reMDSeparator.MatchString(trimmed) {
			continue // skip separator for consistency check
		}
		if strings.Contains(trimmed, "|") {
			cols := parseMDRow(trimmed)
			colCounts[len(cols)]++
		}
	}
	// Find most common column count.
	maxCount, totalPipeLines := 0, 0
	for _, c := range colCounts {
		if c > maxCount {
			maxCount = c
		}
		totalPipeLines += c
	}
	if totalPipeLines == 0 {
		return 0
	}
	consistency := float64(maxCount) / float64(totalPipeLines)
	if consistency < 0.7 {
		return 0
	}
	return 0.95 + (consistency-0.7)*0.05/0.3 // 0.95 to 1.0
}

func (d *markdownDetector) Compatible(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || strings.Contains(trimmed, "|")
}

func (d *markdownDetector) Parse(lines []string) *tableStructure {
	// Find separator row to determine column count and alignment.
	sepIdx := -1
	for i, ln := range lines {
		if reMDSeparator.MatchString(strings.TrimSpace(ln)) {
			sepIdx = i
			break
		}
	}
	if sepIdx < 0 {
		return nil
	}

	// Parse alignment from separator.
	sepCols := parseMDRow(strings.TrimSpace(lines[sepIdx]))
	columns := make([]columnInfo, len(sepCols))
	for i, cell := range sepCols {
		cell = strings.TrimSpace(cell)
		left := strings.HasPrefix(cell, ":")
		right := strings.HasSuffix(cell, ":")
		switch {
		case left && right:
			columns[i].align = alignCenter
		case right:
			columns[i].align = alignRight
		default:
			columns[i].align = alignLeft
		}
	}

	// Collect rows (skip separator).
	var rows [][]string
	headerRow := -1
	for i, ln := range lines {
		if i == sepIdx {
			continue
		}
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		cells := parseMDRow(trimmed)
		// Normalize to expected column count.
		for len(cells) < len(columns) {
			cells = append(cells, "")
		}
		if len(cells) > len(columns) {
			cells = cells[:len(columns)]
		}
		if i < sepIdx && headerRow < 0 {
			headerRow = len(rows)
		}
		rows = append(rows, cells)
	}

	return &tableStructure{
		columns:   columns,
		headerRow: headerRow,
		rows:      rows,
		tableType: tableMarkdown,
	}
}

// parseMDRow splits a markdown table row by pipes, trimming outer pipes.
func parseMDRow(row string) []string {
	row = strings.TrimSpace(row)
	// Strip leading/trailing pipes.
	if strings.HasPrefix(row, "|") {
		row = row[1:]
	}
	if strings.HasSuffix(row, "|") {
		row = row[:len(row)-1]
	}
	parts := strings.Split(row, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}
```

**Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run TestMarkdownDetector -v`
Expected: PASS

**Step 5: Commit**

```bash
git add apps/texelterm/tablefmt/detect.go apps/texelterm/tablefmt/detect_test.go
git commit -m "Add markdown table detector with GFM alignment support"
```

---

### Task 4: Space-Aligned, Pipe-Separated, and CSV Detectors

Add the remaining three detectors.

**Files:**
- Modify: `apps/texelterm/tablefmt/detect.go`
- Modify: `apps/texelterm/tablefmt/detect_test.go`

**Step 1: Write failing tests for all three detectors**

Append to `apps/texelterm/tablefmt/detect_test.go`:

```go
// --- Space-aligned detector tests ---

func TestSpaceAlignedDetector_PsAux(t *testing.T) {
	d := &spaceAlignedDetector{}
	lines := []string{
		"  PID TTY      STAT   TIME COMMAND",
		"    1 ?        Ss     0:01 /sbin/init",
		"   42 ?        S      0:00 /usr/lib/systemd",
		"  123 tty1     Ss+    0:00 /sbin/agetty",
		" 1234 pts/0    Ss     0:00 bash",
	}
	score := d.Score(lines)
	if score < 0.6 {
		t.Errorf("expected score >= 0.6 for ps output, got %f", score)
	}
}

func TestSpaceAlignedDetector_KubectlGet(t *testing.T) {
	d := &spaceAlignedDetector{}
	lines := []string{
		"NAME                 STATUS    AGE     VERSION",
		"nginx-pod            Running   5d      1.21.0",
		"redis-pod            Running   3d      7.0.5",
		"postgres-pod         Running   10d     15.2",
	}
	score := d.Score(lines)
	if score < 0.6 {
		t.Errorf("expected score >= 0.6 for kubectl output, got %f", score)
	}
}

func TestSpaceAlignedDetector_NotATable(t *testing.T) {
	d := &spaceAlignedDetector{}
	lines := []string{
		"This is prose text",
		"Another sentence here",
		"Nothing columnar about this",
	}
	score := d.Score(lines)
	if score >= 0.6 {
		t.Errorf("expected score < 0.6 for prose, got %f", score)
	}
}

func TestSpaceAlignedDetector_Parse(t *testing.T) {
	d := &spaceAlignedDetector{}
	lines := []string{
		"NAME         STATUS    AGE",
		"nginx        Running   5d",
		"redis        Running   3d",
	}
	ts := d.Parse(lines)
	if ts == nil {
		t.Fatal("expected non-nil tableStructure")
	}
	if len(ts.columns) < 2 {
		t.Errorf("expected >= 2 columns, got %d", len(ts.columns))
	}
	if len(ts.rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(ts.rows))
	}
}

// --- Pipe-separated detector tests ---

func TestPipeDetector_BasicPipes(t *testing.T) {
	d := &pipeDetector{}
	lines := []string{
		"Name | Age | City",
		"Alice | 30 | New York",
		"Bob | 25 | London",
	}
	score := d.Score(lines)
	if score < 0.7 {
		t.Errorf("expected score >= 0.7 for pipe-separated, got %f", score)
	}
}

func TestPipeDetector_NotPipes(t *testing.T) {
	d := &pipeDetector{}
	lines := []string{
		"just some | in text",
		"but not columnar content here",
		"no consistent pipes at all",
	}
	score := d.Score(lines)
	if score >= 0.7 {
		t.Errorf("expected score < 0.7, got %f", score)
	}
}

func TestPipeDetector_SkipsMDTable(t *testing.T) {
	d := &pipeDetector{}
	lines := []string{
		"| Name  | Age |",
		"| ----- | --- |",
		"| Alice | 30  |",
	}
	// Should score low because MD detector handles these
	score := d.Score(lines)
	if score >= 0.7 {
		t.Errorf("pipe detector should not claim MD tables, got score %f", score)
	}
}

// --- CSV/TSV detector tests ---

func TestCSVDetector_CommaSeparated(t *testing.T) {
	d := &csvDetector{}
	lines := []string{
		"Name,Age,City",
		"Alice,30,New York",
		"Bob,25,London",
		"Carol,35,Paris",
	}
	score := d.Score(lines)
	if score < 0.5 {
		t.Errorf("expected score >= 0.5 for CSV, got %f", score)
	}
}

func TestCSVDetector_TabSeparated(t *testing.T) {
	d := &csvDetector{}
	lines := []string{
		"Name\tAge\tCity",
		"Alice\t30\tNew York",
		"Bob\t25\tLondon",
	}
	score := d.Score(lines)
	if score < 0.5 {
		t.Errorf("expected score >= 0.5 for TSV, got %f", score)
	}
}

func TestCSVDetector_NotCSV(t *testing.T) {
	d := &csvDetector{}
	lines := []string{
		"Hello world",
		"No commas or tabs here",
		"Just plain text",
	}
	score := d.Score(lines)
	if score >= 0.5 {
		t.Errorf("expected score < 0.5 for prose, got %f", score)
	}
}

func TestCSVDetector_QuotedFields(t *testing.T) {
	d := &csvDetector{}
	lines := []string{
		`Name,Description,City`,
		`Alice,"Has a, comma",New York`,
		`Bob,"Simple",London`,
	}
	ts := d.Parse(lines)
	if ts == nil {
		t.Fatal("expected non-nil tableStructure")
	}
	// Row 1, col 1 should be the full quoted content without quotes
	if len(ts.rows) >= 2 && len(ts.rows[1]) >= 2 {
		if ts.rows[1][1] != "Has a, comma" {
			t.Errorf("expected quoted field 'Has a, comma', got %q", ts.rows[1][1])
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run "TestSpaceAligned|TestPipe|TestCSV" -v`
Expected: Compilation error — types don't exist.

**Step 3: Implement the three detectors**

Add to `apps/texelterm/tablefmt/detect.go`: space-aligned detector (extract and improve from txfmt's `detectColumnsFromHeader`/`detectColumnsFromGaps`), pipe detector (similar to MD but no separator requirement, defers to MD if separator present), CSV detector (comma/tab with quoted field support).

Implementation guidance:

**Space-aligned**: Re-use the gap-detection algorithm from `txfmt.detectColumnsFromGaps` and `txfmt.detectColumnsFromHeader`. Score based on number of strong column boundaries relative to lines. `Compatible` checks if line has content at expected column positions.

**Pipe-separated**: Count `|` per line. Consistent count across 70%+ of lines = high score. If any line matches MD separator regex, return 0 (defer to MD detector). `Parse` splits on `|`.

**CSV**: Detect delimiter (comma or tab) by counting occurrences. Consistent count per line = high score. `Parse` respects RFC 4180 quoting. `Compatible` checks delimiter count within ±1 of expected.

**Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run "TestSpaceAligned|TestPipe|TestCSV" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add apps/texelterm/tablefmt/detect.go apps/texelterm/tablefmt/detect_test.go
git commit -m "Add space-aligned, pipe-separated, and CSV/TSV detectors"
```

---

### Task 5: State Machine — Detection and Buffering

Wire the detectors into the state machine so lines are scored and buffered.

**Files:**
- Modify: `apps/texelterm/tablefmt/tablefmt.go`
- Modify: `apps/texelterm/tablefmt/tablefmt_test.go`

**Step 1: Write failing tests for state transitions**

Add to `apps/texelterm/tablefmt/tablefmt_test.go`:

```go
func TestBuffering_MDTable(t *testing.T) {
	tf := New(1000)
	tf.NotifyPromptStart()

	lines := []string{
		"| Name  | Age |",
		"| ----- | --- |",
		"| Alice | 30  |",
		"| Bob   | 25  |",
	}
	for i, s := range lines {
		line := makeCells(s)
		tf.HandleLine(int64(i), line, true)
		if !tf.ShouldSuppress(int64(i)) {
			t.Errorf("line %d: expected suppression during buffering", i)
		}
	}
	if tf.state != stateBuffering {
		t.Errorf("expected stateBuffering, got %d", tf.state)
	}
	if len(tf.buffer) != 4 {
		t.Errorf("expected 4 buffered lines, got %d", len(tf.buffer))
	}
}

func TestBuffering_FlushOnNonTableLine(t *testing.T) {
	tf := New(1000)
	tf.NotifyPromptStart()

	var inserted int
	tf.SetInsertFunc(func(_ int64, _ []parser.Cell) { inserted++ })

	lines := []string{
		"| Name  | Age |",
		"| ----- | --- |",
		"| Alice | 30  |",
	}
	for i, s := range lines {
		tf.HandleLine(int64(i), makeCells(s), true)
	}
	// Non-table line triggers flush
	tf.HandleLine(3, makeCells("This is not a table line"), true)

	if tf.state != stateScanning {
		t.Errorf("expected stateScanning after flush, got %d", tf.state)
	}
	if inserted == 0 {
		t.Error("expected insertFunc to be called during flush")
	}
}

func TestBuffering_FlushOnPrompt(t *testing.T) {
	tf := New(1000)
	tf.NotifyPromptStart()

	var inserted int
	tf.SetInsertFunc(func(_ int64, _ []parser.Cell) { inserted++ })

	lines := []string{
		"| Name  | Age |",
		"| ----- | --- |",
		"| Alice | 30  |",
	}
	for i, s := range lines {
		tf.HandleLine(int64(i), makeCells(s), true)
	}
	// Prompt triggers flush
	tf.HandleLine(3, makeCells("$ "), false)

	if tf.state != stateScanning {
		t.Errorf("expected stateScanning after prompt flush, got %d", tf.state)
	}
}

func TestBuffering_LimitExceeded(t *testing.T) {
	tf := New(3) // Very low limit for testing
	tf.NotifyPromptStart()

	var insertedLines [][]parser.Cell
	tf.SetInsertFunc(func(_ int64, cells []parser.Cell) {
		insertedLines = append(insertedLines, cells)
	})

	lines := []string{
		"| Name  | Age |",
		"| ----- | --- |",
		"| Alice | 30  |",
		"| Bob   | 25  |", // This is the 4th line → exceeds limit of 3
	}
	for i, s := range lines {
		tf.HandleLine(int64(i), makeCells(s), true)
	}

	// After exceeding limit: buffer should have been flushed raw
	if tf.state != stateScanning {
		t.Errorf("expected stateScanning after limit exceeded, got %d", tf.state)
	}
	// Inserted lines should be the raw originals (no box-drawing)
	for _, cells := range insertedLines {
		for _, c := range cells {
			if c.Rune == '╭' || c.Rune == '│' || c.Rune == '╰' {
				t.Error("expected raw emission, got box-drawing characters")
			}
		}
	}
}

func TestScanningPassThrough(t *testing.T) {
	tf := New(1000)
	tf.NotifyPromptStart()

	line := makeCells("just plain text no table here")
	tf.HandleLine(0, line, true)

	if tf.ShouldSuppress(0) {
		t.Error("non-table lines should not be suppressed")
	}
	if tf.state != stateScanning {
		t.Errorf("should remain in scanning, got %d", tf.state)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run "TestBuffering|TestScanningPassThrough" -v`
Expected: FAIL — state machine doesn't detect/buffer yet.

**Step 3: Implement state machine with detection**

Update `apps/texelterm/tablefmt/tablefmt.go`. The `HandleLine` method should:

1. In SCANNING: extract plain text, run all detectors on `[line]`. If any detector scores > 0, transition to BUFFERING, clone and buffer the line, set `tf.suppressed = true`.
2. In BUFFERING: check if line is compatible with the active detector. If yes: clone, buffer, suppress. If no or buffer limit hit: flush, then process the current line in SCANNING state.
3. `flush()`: re-score the full buffer with all detectors. Pick the winner. If above threshold, render (Task 6). If below threshold or limit exceeded, emit raw lines via insertFunc.

Key details:
- Store all `tableDetector` instances in the struct: `detectors []tableDetector`
- Track `activeDetector` (the one that first triggered buffering)
- Use `line.Clone()` before buffering
- `extractPlainText` helper (same as txfmt's)

**Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run "TestBuffering|TestScanningPassThrough" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add apps/texelterm/tablefmt/tablefmt.go apps/texelterm/tablefmt/tablefmt_test.go
git commit -m "Wire detectors into state machine with buffering and suppression"
```

---

### Task 6: Box-Drawing Renderer

Implement the renderer that turns a `tableStructure` into formatted `parser.Cell` slices with box-drawing borders.

**Files:**
- Create: `apps/texelterm/tablefmt/render.go`
- Create: `apps/texelterm/tablefmt/render_test.go`

**Step 1: Write failing tests for rendering**

Create `apps/texelterm/tablefmt/render_test.go`:

```go
package tablefmt

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func cellsToString(cells []parser.Cell) string {
	rs := make([]rune, len(cells))
	for i, c := range cells {
		rs[i] = c.Rune
	}
	return string(rs)
}

func TestRenderTable_BasicMD(t *testing.T) {
	ts := &tableStructure{
		columns: []columnInfo{
			{align: alignLeft},
			{align: alignLeft},
		},
		headerRow: 0,
		rows: [][]string{
			{"Name", "City"},
			{"Alice", "New York"},
			{"Bob", "London"},
		},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	// Expected output:
	// ╭───────┬──────────╮  (top border)
	// │ Name  │ City     │  (header)
	// ├───────┼──────────┤  (header separator)
	// │ Alice │ New York │  (data)
	// │ Bob   │ London   │  (data)
	// ╰───────┴──────────╯  (bottom border)
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines (border+header+sep+2data+border), got %d", len(lines))
	}

	// Top border: starts with ╭, ends with ╮
	top := cellsToString(lines[0])
	if []rune(top)[0] != '╭' {
		t.Errorf("top border should start with ╭, got %c", []rune(top)[0])
	}
	topRunes := []rune(top)
	if topRunes[len(topRunes)-1] != '╮' {
		t.Errorf("top border should end with ╮, got %c", topRunes[len(topRunes)-1])
	}

	// Bottom border: starts with ╰, ends with ╯
	bot := cellsToString(lines[len(lines)-1])
	botRunes := []rune(bot)
	if botRunes[0] != '╰' {
		t.Errorf("bottom border should start with ╰, got %c", botRunes[0])
	}
	if botRunes[len(botRunes)-1] != '╯' {
		t.Errorf("bottom border should end with ╯, got %c", botRunes[len(botRunes)-1])
	}

	// Header row has │ delimiters
	header := cellsToString(lines[1])
	headerRunes := []rune(header)
	if headerRunes[0] != '│' {
		t.Errorf("header should start with │, got %c", headerRunes[0])
	}

	// Header separator: ├ ... ┤
	sep := cellsToString(lines[2])
	sepRunes := []rune(sep)
	if sepRunes[0] != '├' {
		t.Errorf("separator should start with ├, got %c", sepRunes[0])
	}
}

func TestRenderTable_RightAlignment(t *testing.T) {
	ts := &tableStructure{
		columns: []columnInfo{
			{align: alignLeft},
			{align: alignRight},
		},
		headerRow: 0,
		rows: [][]string{
			{"Item", "Price"},
			{"Apple", "1.50"},
			{"Banana", "20.00"},
		},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	// Check that right-aligned values are right-justified.
	// Data row for "1.50" should have leading spaces.
	dataRow := cellsToString(lines[3]) // first data row (after top, header, sep)
	t.Logf("data row: %q", dataRow)
	// "1.50" should be right-aligned in its column
	// The exact position depends on column width (max of "Price", "1.50", "20.00" = 5)
	// " 1.50" (5 chars, right-aligned)
}

func TestRenderTable_FixedWidth(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}, {align: alignLeft}},
		headerRow: -1,
		rows:      [][]string{{"a", "b"}, {"c", "d"}},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	for i, line := range lines {
		if len(line) == 0 {
			t.Errorf("line %d: empty", i)
		}
		// All lines should be the same width
		if i > 0 && len(line) != len(lines[0]) {
			t.Errorf("line %d: width %d != line 0 width %d", i, len(line), len(lines[0]))
		}
	}
}

func TestRenderTable_NoHeader(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}, {align: alignLeft}},
		headerRow: -1, // no header
		rows:      [][]string{{"a", "b"}, {"c", "d"}},
		tableType: tableSpaceAligned,
	}
	lines := renderTable(ts)
	// Should be: top border + 2 data rows + bottom border = 4 lines (no separator)
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines (no header sep), got %d", len(lines))
	}
}

func TestRenderTable_DimBorders(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}},
		headerRow: -1,
		rows:      [][]string{{"data"}},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	// All border characters should have AttrDim
	for _, cell := range lines[0] { // top border
		if cell.Attr&parser.AttrDim == 0 {
			t.Errorf("border char %c should have dim attribute", cell.Rune)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run TestRenderTable -v`
Expected: Compilation error — `renderTable` doesn't exist.

**Step 3: Implement the renderer**

Create `apps/texelterm/tablefmt/render.go`:

```go
package tablefmt

import (
	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// renderTable transforms a tableStructure into formatted cell lines with box-drawing borders.
// Returns a slice of cell slices, each representing one output line.
// All lines have the same width. Border characters have AttrDim.
func renderTable(ts *tableStructure) [][]parser.Cell {
	if ts == nil || len(ts.rows) == 0 || len(ts.columns) == 0 {
		return nil
	}

	// 1. Compute column widths (max content width per column).
	colWidths := make([]int, len(ts.columns))
	for _, row := range ts.rows {
		for ci, cell := range row {
			if ci < len(colWidths) {
				w := len([]rune(cell))
				if w > colWidths[ci] {
					colWidths[ci] = w
				}
			}
		}
	}

	// 2. Total line width: │ + (padding + content + padding + │) per column
	// = 1 + sum(1 + colWidth + 1 + 1) = 1 + sum(colWidth + 3)
	// Actually: │ col │ col │ ... │  →  ncols * (1+colWidth+1) + 1
	// Simplified: for each col: " content " then │. Plus leading │.
	totalWidth := 1 // leading │
	for _, w := range colWidths {
		totalWidth += 1 + w + 1 + 1 // space + content + space + │
	}

	var result [][]parser.Cell

	// 3. Top border: ╭───┬───╮
	result = append(result, makeHBorder(colWidths, '╭', '┬', '╮', '─'))

	// 4. Data rows with optional header separator.
	for ri, row := range ts.rows {
		result = append(result, makeDataRow(row, colWidths, ts.columns))
		if ri == ts.headerRow {
			result = append(result, makeHBorder(colWidths, '├', '┼', '┤', '─'))
		}
	}

	// 5. Bottom border: ╰───┴───╯
	result = append(result, makeHBorder(colWidths, '╰', '┴', '╯', '─'))

	return result
}

// makeHBorder creates a horizontal border line.
func makeHBorder(colWidths []int, left, junction, right, fill rune) []parser.Cell {
	var cells []parser.Cell
	bc := func(r rune) parser.Cell {
		return parser.Cell{Rune: r, FG: parser.DefaultFG, BG: parser.DefaultBG, Attr: parser.AttrDim}
	}
	cells = append(cells, bc(left))
	for ci, w := range colWidths {
		for i := 0; i < w+2; i++ { // +2 for padding spaces
			cells = append(cells, bc(fill))
		}
		if ci < len(colWidths)-1 {
			cells = append(cells, bc(junction))
		}
	}
	cells = append(cells, bc(right))
	return cells
}

// makeDataRow creates a data row: │ val │ val │
func makeDataRow(row []string, colWidths []int, columns []columnInfo) []parser.Cell {
	var cells []parser.Cell
	bc := func(r rune) parser.Cell {
		return parser.Cell{Rune: r, FG: parser.DefaultFG, BG: parser.DefaultBG, Attr: parser.AttrDim}
	}
	dc := func(r rune) parser.Cell {
		return parser.Cell{Rune: r, FG: parser.DefaultFG, BG: parser.DefaultBG}
	}

	cells = append(cells, bc('│'))
	for ci, w := range colWidths {
		val := ""
		if ci < len(row) {
			val = row[ci]
		}
		valRunes := []rune(val)
		padTotal := w - len(valRunes)
		if padTotal < 0 {
			padTotal = 0
			valRunes = valRunes[:w]
		}

		align := alignLeft
		if ci < len(columns) {
			align = columns[ci].align
		}

		var leftPad, rightPad int
		switch align {
		case alignRight:
			leftPad = padTotal
			rightPad = 0
		case alignCenter:
			leftPad = padTotal / 2
			rightPad = padTotal - leftPad
		default: // alignLeft
			leftPad = 0
			rightPad = padTotal
		}

		cells = append(cells, dc(' ')) // left padding
		for i := 0; i < leftPad; i++ {
			cells = append(cells, dc(' '))
		}
		for _, r := range valRunes {
			cells = append(cells, dc(r))
		}
		for i := 0; i < rightPad; i++ {
			cells = append(cells, dc(' '))
		}
		cells = append(cells, dc(' ')) // right padding

		if ci < len(colWidths)-1 {
			cells = append(cells, bc('│'))
		}
	}
	cells = append(cells, bc('│'))
	return cells
}
```

**Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run TestRenderTable -v`
Expected: PASS

**Step 5: Commit**

```bash
git add apps/texelterm/tablefmt/render.go apps/texelterm/tablefmt/render_test.go
git commit -m "Add box-drawing table renderer with alignment support"
```

---

### Task 7: Column-Type Classification and Colorization

Add per-column type detection and color application.

**Files:**
- Create: `apps/texelterm/tablefmt/classify.go`
- Create: `apps/texelterm/tablefmt/classify_test.go`
- Modify: `apps/texelterm/tablefmt/render.go`

**Step 1: Write failing tests for classification**

Create `apps/texelterm/tablefmt/classify_test.go`:

```go
package tablefmt

import "testing"

func TestClassifyColumn_Number(t *testing.T) {
	values := []string{"42", "100", "7", "1,234"}
	ct := classifyValues(values)
	if ct != colNumber {
		t.Errorf("expected colNumber, got %d", ct)
	}
}

func TestClassifyColumn_DateTime(t *testing.T) {
	values := []string{"5d", "3d", "10d", "2d"}
	ct := classifyValues(values)
	if ct != colDateTime {
		t.Errorf("expected colDateTime, got %d", ct)
	}
}

func TestClassifyColumn_Path(t *testing.T) {
	values := []string{"/etc/nginx.conf", "/etc/redis.conf", "/var/lib/pg"}
	ct := classifyValues(values)
	if ct != colPath {
		t.Errorf("expected colPath, got %d", ct)
	}
}

func TestClassifyColumn_Text(t *testing.T) {
	values := []string{"Running", "Pending", "Running"}
	ct := classifyValues(values)
	if ct != colText {
		t.Errorf("expected colText, got %d", ct)
	}
}

func TestClassifyColumn_MixedDefaultsToText(t *testing.T) {
	values := []string{"hello", "42", "/path", "10d"}
	ct := classifyValues(values)
	if ct != colText {
		t.Errorf("expected colText for mixed content, got %d", ct)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run TestClassifyColumn -v`
Expected: Compilation error.

**Step 3: Implement classification**

Create `apps/texelterm/tablefmt/classify.go`:

```go
package tablefmt

import (
	"regexp"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// columnType classifies a table column for per-column colorization.
type columnType int

const (
	colText     columnType = iota // default FG
	colNumber                     // yellow
	colDateTime                   // cyan
	colPath                       // green
)

var (
	reColNumber   = regexp.MustCompile(`^-?[0-9][0-9,.]*%?$`)
	reColDateTime = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}|\d{1,3}[dhms]|<?\d+[dhms](\d+[dhms])*>?|\d{2}:\d{2}(:\d{2})?|\d{1,2}[A-Z][a-z]{2}\d{2,4}|\d+\.\d+[dhms])$`)
	reColPath     = regexp.MustCompile(`[/\\]|^\.\w+$|^[\w.-]+\.\w{1,5}$`)
)

// classifyValues determines the column type from a slice of cell values.
// "All column, same type" — majority of non-empty values determines type.
func classifyValues(values []string) columnType {
	numCount, dateCount, pathCount, total := 0, 0, 0, 0
	for _, val := range values {
		if val == "" || val == "-" || val == "<none>" {
			continue
		}
		total++
		if reColNumber.MatchString(val) {
			numCount++
		} else if reColDateTime.MatchString(val) {
			dateCount++
		} else if reColPath.MatchString(val) {
			pathCount++
		}
	}
	if total == 0 {
		return colText
	}
	if numCount*100/total >= 60 {
		return colNumber
	}
	if dateCount*100/total >= 60 {
		return colDateTime
	}
	if pathCount*100/total >= 40 {
		return colPath
	}
	return colText
}

// classifyAndColorize determines column types and applies colors to rendered data cells.
// headerRow is the index in rows of the header (-1 if none). Header gets bold cyan.
// Colors: Number=yellow, DateTime=cyan, Path=green, Text=default.
func classifyAndColorize(ts *tableStructure, renderedLines [][]parser.Cell) {
	if ts == nil || len(ts.columns) == 0 || len(ts.rows) == 0 {
		return
	}

	// Classify each column from data rows (skip header).
	colTypes := make([]columnType, len(ts.columns))
	for ci := range ts.columns {
		var values []string
		for ri, row := range ts.rows {
			if ri == ts.headerRow {
				continue
			}
			if ci < len(row) {
				values = append(values, row[ci])
			}
		}
		colTypes[ci] = classifyValues(values)
	}

	// Apply colors to rendered lines.
	// renderedLines layout: [topBorder, rows...(with header sep interspersed), bottomBorder]
	// We need to map each rendered data row back to its ts.rows index.
	dataLineStart := 1 // skip top border
	dataRowIdx := 0
	for i := dataLineStart; i < len(renderedLines)-1; i++ { // skip bottom border
		cells := renderedLines[i]
		// Check if this is a border line (all cells are border chars).
		if len(cells) > 0 && isBorderChar(cells[0].Rune) {
			continue
		}
		if dataRowIdx >= len(ts.rows) {
			break
		}
		isHeader := dataRowIdx == ts.headerRow
		colorizeRenderedRow(cells, ts.columns, colTypes, isHeader)
		dataRowIdx++
	}
}

var (
	colorYellow = parser.Color{Mode: parser.ColorModeStandard, Value: 3}
	colorCyan   = parser.Color{Mode: parser.ColorModeStandard, Value: 6}
	colorGreen  = parser.Color{Mode: parser.ColorModeStandard, Value: 2}
)

// colorizeRenderedRow applies color to content cells within a rendered data row.
func colorizeRenderedRow(cells []parser.Cell, columns []columnInfo, colTypes []columnType, isHeader bool) {
	if isHeader {
		// Header: bold cyan on all non-border, non-space content.
		for i := range cells {
			c := &cells[i]
			if c.Attr&parser.AttrDim != 0 { // border char
				continue
			}
			if c.Rune == ' ' {
				continue
			}
			c.FG = colorCyan
			c.Attr |= parser.AttrBold
		}
		return
	}

	// Color by column type. We need to find column boundaries in the rendered cells.
	// Rendered row: │ val │ val │ ... │
	// Walk through cells, tracking which column we're in.
	colIdx := -1
	for i := range cells {
		c := &cells[i]
		if c.Attr&parser.AttrDim != 0 && c.Rune == '│' {
			colIdx++
			continue
		}
		if colIdx < 0 || colIdx >= len(colTypes) {
			continue
		}
		if c.Rune == ' ' {
			continue
		}
		ct := colTypes[colIdx]
		switch ct {
		case colNumber:
			c.FG = colorYellow
		case colDateTime:
			c.FG = colorCyan
		case colPath:
			c.FG = colorGreen
		}
	}
}

func isBorderChar(r rune) bool {
	switch r {
	case '╭', '╮', '╰', '╯', '├', '┤', '┬', '┴', '┼', '─':
		return true
	}
	return false
}
```

**Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run TestClassifyColumn -v`
Expected: PASS

**Step 5: Wire classification into render pipeline**

In `render.go`, add a call to `classifyAndColorize(ts, result)` before returning from `renderTable`.

**Step 6: Commit**

```bash
git add apps/texelterm/tablefmt/classify.go apps/texelterm/tablefmt/classify_test.go apps/texelterm/tablefmt/render.go
git commit -m "Add column-type classification and colorization"
```

---

### Task 8: Wire Flush to Renderer

Connect the state machine's `flush()` to the renderer so buffered tables actually get emitted as formatted output.

**Files:**
- Modify: `apps/texelterm/tablefmt/tablefmt.go`
- Modify: `apps/texelterm/tablefmt/tablefmt_test.go`

**Step 1: Write failing end-to-end test**

Add to `apps/texelterm/tablefmt/tablefmt_test.go`:

```go
func TestEndToEnd_MDTable(t *testing.T) {
	tf := New(1000)
	tf.NotifyPromptStart()

	var insertedLines [][]parser.Cell
	tf.SetInsertFunc(func(_ int64, cells []parser.Cell) {
		insertedLines = append(insertedLines, cells)
	})

	mdLines := []string{
		"| Name  | Age |",
		"| ----- | --- |",
		"| Alice | 30  |",
		"| Bob   | 25  |",
	}
	for i, s := range mdLines {
		tf.HandleLine(int64(i), makeCells(s), true)
	}
	// Trigger flush with prompt
	tf.HandleLine(4, makeCells("$ "), false)

	// Should have emitted formatted table lines
	if len(insertedLines) == 0 {
		t.Fatal("expected formatted lines to be emitted")
	}

	// First emitted line should be top border with ╭
	firstRunes := []rune(cellsToString(insertedLines[0]))
	if firstRunes[0] != '╭' {
		t.Errorf("expected top border ╭, got %c", firstRunes[0])
	}

	// Last emitted line should be bottom border with ╰
	lastRunes := []rune(cellsToString(insertedLines[len(insertedLines)-1]))
	if lastRunes[0] != '╰' {
		t.Errorf("expected bottom border ╰, got %c", lastRunes[0])
	}

	// All lines should have FixedWidth (check via cell count — all same width)
	width := len(insertedLines[0])
	for i, line := range insertedLines {
		if len(line) != width {
			t.Errorf("line %d: width %d != expected %d", i, len(line), width)
		}
	}
}

func TestEndToEnd_SpaceAligned(t *testing.T) {
	tf := New(1000)
	tf.NotifyPromptStart()

	var insertedLines [][]parser.Cell
	tf.SetInsertFunc(func(_ int64, cells []parser.Cell) {
		insertedLines = append(insertedLines, cells)
	})

	lines := []string{
		"NAME                 STATUS    AGE     VERSION",
		"nginx-pod            Running   5d      1.21.0",
		"redis-pod            Running   3d      7.0.5",
		"postgres-pod         Running   10d     15.2",
		"memcached-pod        Running   2d      1.6.17",
	}
	for i, s := range lines {
		tf.HandleLine(int64(i), makeCells(s), true)
	}
	tf.HandleLine(5, makeCells("$ "), false)

	if len(insertedLines) == 0 {
		t.Fatal("expected formatted lines to be emitted")
	}

	firstRunes := []rune(cellsToString(insertedLines[0]))
	if firstRunes[0] != '╭' {
		t.Errorf("expected top border ╭, got %c", firstRunes[0])
	}
}

func TestEndToEnd_MultipleTables(t *testing.T) {
	// Simulate ls -lR: text header, then table, then text header, then table
	tf := New(1000)
	tf.NotifyPromptStart()

	var insertedLines [][]parser.Cell
	tf.SetInsertFunc(func(_ int64, cells []parser.Cell) {
		insertedLines = append(insertedLines, cells)
	})

	idx := int64(0)
	emit := func(s string) {
		tf.HandleLine(idx, makeCells(s), true)
		idx++
	}

	// First table
	emit("NAME         STATUS    AGE")
	emit("nginx        Running   5d")
	emit("redis        Running   3d")
	// Non-table line → flushes first table
	emit("")
	emit("./subdir:")
	// Second table
	emit("NAME         STATUS    AGE")
	emit("postgres     Running   10d")
	emit("memcached    Running   2d")
	// Prompt → flushes second table
	tf.HandleLine(idx, makeCells("$ "), false)

	// Should have emitted two independent tables
	topBorders := 0
	for _, line := range insertedLines {
		runes := []rune(cellsToString(line))
		if len(runes) > 0 && runes[0] == '╭' {
			topBorders++
		}
	}
	if topBorders < 2 {
		t.Errorf("expected >= 2 top borders for 2 tables, got %d", topBorders)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run TestEndToEnd -v`
Expected: FAIL — flush doesn't render yet.

**Step 3: Implement flush → render → emit**

Update `flush()` in `tablefmt.go`:

1. Collect plain text from buffer.
2. Score with all detectors. Pick the best above its threshold.
3. If high confidence: call `Parse()`, then `renderTable()`, then `classifyAndColorize()`.
4. Emit all rendered lines via `insertFunc` at the first buffered line's index.
5. Set `FixedWidth` on each emitted `LogicalLine`.
6. If low confidence or limit exceeded: emit raw buffer lines via `insertFunc`.
7. Clear buffer, return to SCANNING.

The emitted lines should be wrapped in `LogicalLine` with `FixedWidth` set. But `insertFunc` takes `[]parser.Cell`, not `*LogicalLine`. The `FixedWidth` needs to be set differently — looking at the existing code, `insertFunc` in VTerm creates a `LogicalLine` internally. We should set `FixedWidth` after insertion, or modify `insertFunc` to accept it.

Actually, looking at `RequestLineInsert` (vterm.go:1265-1274), it creates a bare LogicalLine and sets `line.Cells = cells`. We'd need to also set `FixedWidth`. The simplest approach: add a wrapper in tablefmt that, after calling `insertFunc`, retrieves the line and sets FixedWidth. But we don't have access to the line after insertion.

Alternative: extend `insertFunc` signature to include FixedWidth. But that's a bigger change.

Simplest: the table formatter emits lines with a special marker (e.g., a trailing cell with a specific attribute) that the VTerm recognizes and sets FixedWidth. Actually, that's hacky.

Better approach: change `insertFunc` to return `*LogicalLine` so the caller can set FixedWidth. Or: add a new callback `SetLineFixedWidth(lineIdx int64, width int)`. Or: just accept that inserted lines need FixedWidth set, and add a `FixedWidthInserter` optional interface.

The pragmatic solution: extend `RequestLineInsert` to accept an optional FixedWidth parameter. Since Go doesn't have overloading, add a new method `RequestLineInsertFixed(beforeIdx int64, cells []Cell, fixedWidth int)`. The `insertFunc` type becomes `func(beforeIdx int64, cells []parser.Cell, fixedWidth int)`. Update `LineInserter` accordingly.

Actually the simplest: just change the insertFunc signature everywhere to include fixedWidth. It's used in 3 places (transformer.go, term.go, txfmt.go). Quick and clean.

**Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run TestEndToEnd -v`
Expected: PASS

**Step 5: Commit**

```bash
git add apps/texelterm/tablefmt/tablefmt.go apps/texelterm/tablefmt/tablefmt_test.go
git commit -m "Wire flush to renderer for end-to-end table formatting"
```

---

### Task 9: Remove Table Code from txfmt

Extract all table-related code from txfmt now that tablefmt handles it.

**Files:**
- Modify: `apps/texelterm/txfmt/txfmt.go`
- Modify: `apps/texelterm/txfmt/txfmt_test.go`

**Step 1: Remove from txfmt.go**

Remove the following from `apps/texelterm/txfmt/txfmt.go`:

1. `modeTable` constant (line 40)
2. `columnType` type and constants (lines 46-53)
3. `tableColumn` struct (lines 56-60)
4. `borderPosition` type and constants (lines 62-69)
5. Table-related regexes: `reColNumber`, `reColDateTime`, `reColPath`, `reNonTableLine` (lines 96-101)
6. Table-related fields in `Formatter` struct: `tableLineCount`, `tableColumns`, `tableWidth`, `tableBordersActive` (lines 110-113)
7. Table border insertion in `HandleLine` command→prompt transition (lines 160-163)
8. Table state reset in `HandleLine` (lines 165-168)
9. `modeTable` case in `colorize()` (lines 401-406)
10. `modeTable` handling in `recolorizeBacklog` (lines 237-239)
11. `FixedWidth` setting for `modeTable` (lines 199-201 and 258-259)
12. `scoreTable()` function (lines 615-652)
13. `modeTable` scoring in `score()` (line 589)
14. `detectTableColumns`, `refineColumnEnds`, `detectColumnsFromHeader`, `detectColumnsFromGaps` (lines 654-925)
15. `classifyColumn`, `classifyAllColumns` (lines 927-976)
16. `colorizeTableCellsWithColumns`, `addTableSideBorders`, `makeBorderLine` (lines 978-1091)
17. `recolorizeTableBacklog` method (lines 267-315)

**Step 2: Remove table tests from txfmt_test.go**

Remove tests that reference table-specific functions:
- `TestDetector_Table`
- `TestColorize_Table_Header`
- `TestDetectTableColumns`
- `TestClassifyColumn_Number`, `TestClassifyColumn_DateTime`, `TestClassifyColumn_Path`, `TestClassifyColumn_Text`
- `TestColorizeTableCellsWithColumns_NumberColumn`, `TestColorizeTableCellsWithColumns_NoNumberInText`
- `TestAddTableSideBorders`
- `TestMakeBorderLine`
- `TestTablePipeline_FullBacklog`
- `TestSideBordersOnContentLines`
- `TestBottomBorderOnPrompt`
- `TestDockerPsColumns`
- `TestPsAxColumns`

Keep: `cellsToString` helper (used by remaining tests — or remove if unused).

**Step 3: Run txfmt tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/txfmt/ -v`
Expected: PASS (all non-table tests still pass)

**Step 4: Run full test suite**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/... -count=1`
Expected: All pass

**Step 5: Commit**

```bash
git add apps/texelterm/txfmt/txfmt.go apps/texelterm/txfmt/txfmt_test.go
git commit -m "Remove table formatting code from txfmt (moved to tablefmt)"
```

---

### Task 10: Integration Testing — Full Pipeline

Test the complete pipeline with tablefmt before txfmt, ensuring they compose correctly.

**Files:**
- Modify: `apps/texelterm/transformer/txfmt_integration_test.go` (rename to `pipeline_integration_test.go` or add tests)

**Step 1: Write integration tests**

Add to a new file `apps/texelterm/transformer/tablefmt_integration_test.go`:

```go
package transformer_test

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/transformer"
	"github.com/framegrace/texelation/config"

	_ "github.com/framegrace/texelation/apps/texelterm/tablefmt"
	_ "github.com/framegrace/texelation/apps/texelterm/txfmt"
)

func TestTablefmtRegistered(t *testing.T) {
	_, ok := transformer.Lookup("tablefmt")
	if !ok {
		t.Fatal("tablefmt should be registered via init()")
	}
}

func TestPipelineOrder_TablefmtBeforeTxfmt(t *testing.T) {
	cfg := config.Config{
		"transformers": map[string]interface{}{
			"enabled": true,
			"pipeline": []interface{}{
				map[string]interface{}{"id": "tablefmt", "enabled": true, "max_buffer_rows": float64(1000)},
				map[string]interface{}{"id": "txfmt", "enabled": true},
			},
		},
	}
	p := transformer.BuildPipeline(cfg)
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}
}

func TestPipeline_MDTableFormatted(t *testing.T) {
	cfg := config.Config{
		"transformers": map[string]interface{}{
			"enabled": true,
			"pipeline": []interface{}{
				map[string]interface{}{"id": "tablefmt", "enabled": true, "max_buffer_rows": float64(1000)},
				map[string]interface{}{"id": "txfmt", "enabled": true},
			},
		},
	}
	p := transformer.BuildPipeline(cfg)
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}

	var insertedLines [][]parser.Cell
	p.SetInsertFunc(func(_ int64, cells []parser.Cell) {
		insertedLines = append(insertedLines, cells)
	})
	p.NotifyPromptStart()

	makeCells := func(s string) *parser.LogicalLine {
		cells := make([]parser.Cell, len([]rune(s)))
		for i, r := range []rune(s) {
			cells[i] = parser.Cell{Rune: r, FG: parser.DefaultFG, BG: parser.DefaultBG}
		}
		return &parser.LogicalLine{Cells: cells}
	}

	mdLines := []string{
		"| Name  | Age |",
		"| ----- | --- |",
		"| Alice | 30  |",
	}
	for i, s := range mdLines {
		p.HandleLine(int64(i), makeCells(s), true)
	}
	// Prompt triggers flush
	p.HandleLine(3, makeCells("$ "), false)

	if len(insertedLines) == 0 {
		t.Fatal("expected formatted table lines")
	}

	// Check first line is top border
	runes := make([]rune, len(insertedLines[0]))
	for i, c := range insertedLines[0] {
		runes[i] = c.Rune
	}
	if runes[0] != '╭' {
		t.Errorf("expected ╭ top border, got %c", runes[0])
	}
}

func TestPipeline_NonTablePassesThrough(t *testing.T) {
	cfg := config.Config{
		"transformers": map[string]interface{}{
			"enabled": true,
			"pipeline": []interface{}{
				map[string]interface{}{"id": "tablefmt", "enabled": true, "max_buffer_rows": float64(1000)},
				map[string]interface{}{"id": "txfmt", "enabled": true},
			},
		},
	}
	p := transformer.BuildPipeline(cfg)
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}
	p.NotifyPromptStart()

	makeCells := func(s string) *parser.LogicalLine {
		cells := make([]parser.Cell, len([]rune(s)))
		for i, r := range []rune(s) {
			cells[i] = parser.Cell{Rune: r, FG: parser.DefaultFG, BG: parser.DefaultBG}
		}
		return &parser.LogicalLine{Cells: cells}
	}

	// JSON line should pass through tablefmt untouched and get colored by txfmt
	line := makeCells(`{"key": "value"}`)
	suppressed := p.HandleLine(0, line, true)
	if suppressed {
		t.Error("JSON line should not be suppressed by tablefmt")
	}
}
```

**Step 2: Run integration tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/transformer/ -v`
Expected: PASS

**Step 3: Run full test suite**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/... -count=1`
Expected: All pass

**Step 4: Commit**

```bash
git add apps/texelterm/transformer/tablefmt_integration_test.go
git commit -m "Add integration tests for tablefmt+txfmt pipeline"
```

---

### Task 11: Conservative Hints for Low Confidence

When confidence is below the formatting threshold but above zero, set FixedWidth without structural changes.

**Files:**
- Modify: `apps/texelterm/tablefmt/tablefmt.go`
- Modify: `apps/texelterm/tablefmt/tablefmt_test.go`

**Step 1: Write failing test**

Add to `apps/texelterm/tablefmt/tablefmt_test.go`:

```go
func TestConservativeHints_LowConfidence(t *testing.T) {
	tf := New(1000)
	tf.NotifyPromptStart()

	var insertedLines [][]parser.Cell
	tf.SetInsertFunc(func(_ int64, cells []parser.Cell) {
		insertedLines = append(insertedLines, cells)
	})

	// Lines that look vaguely columnar but don't pass any detector threshold
	lines := []string{
		"something    here    now",
		"other        data    ok",
		"more         stuff   yes",
	}
	for i, s := range lines {
		tf.HandleLine(int64(i), makeCells(s), true)
	}
	tf.HandleLine(3, makeCells("$ "), false)

	// Emitted lines should be the originals (no box-drawing)
	for _, cells := range insertedLines {
		for _, c := range cells {
			if c.Rune == '╭' || c.Rune == '│' || c.Rune == '╰' {
				t.Error("expected no box-drawing for low-confidence table")
				break
			}
		}
	}
}
```

**Step 2: Implement in flush()**

In the `flush()` method, after scoring: if no detector exceeds its threshold but the buffer was entered (meaning at least one detector scored > 0), emit the raw lines via `insertFunc` with `FixedWidth` set.

**Step 3: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add apps/texelterm/tablefmt/tablefmt.go apps/texelterm/tablefmt/tablefmt_test.go
git commit -m "Add conservative hints for low-confidence tables"
```

---

### Task 12: Final Cleanup and Full Regression

Run the complete test suite, fix any issues, ensure builds pass.

**Files:**
- Potentially any file touched in previous tasks

**Step 1: Build everything**

Run: `cd /home/marc/projects/texel/texelation && make build`
Expected: Clean build

**Step 2: Run all tests**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: All pass

**Step 3: Run race detector**

Run: `cd /home/marc/projects/texel/texelation && go test -race ./apps/texelterm/... -count=1`
Expected: No races

**Step 4: Format check**

Run: `cd /home/marc/projects/texel/texelation && gofmt -l ./apps/texelterm/tablefmt/`
Expected: No files listed (all formatted)

**Step 5: Vet check**

Run: `cd /home/marc/projects/texel/texelation && go vet ./apps/texelterm/...`
Expected: Clean

**Step 6: Final commit if any fixes were needed**

```bash
git add -A && git commit -m "Fix issues found during final regression"
```
