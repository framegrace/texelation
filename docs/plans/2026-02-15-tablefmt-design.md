# Table Formatter Plugin Design

## Goal

Extract table formatting from `txfmt` into a standalone `tablefmt` transformer plugin that detects and renders tabular content with box-drawing borders. Must handle tables appearing/disappearing mid-stream, variable column widths, multiple table types, and never format non-tabular content (confidence-gated).

## Architecture

### Plugin Registration

New package `apps/texelterm/tablefmt/` registered as `"tablefmt"`. Runs **before** `txfmt` in the pipeline:

```yaml
transformers:
  pipeline:
    - id: "tablefmt"
      enabled: true
      max_buffer_rows: 1000
    - id: "txfmt"
      style: "catppuccin-mocha"
```

### State Machine

Per-command state machine with three states. Each detected table is an independent buffer cycle.

```
         SCANNING          (pass-through, testing each line)
            │
            │ line looks table-like (any detector confidence > 0)
            ▼
         BUFFERING         (accumulating lines, no output)
            │
            │ non-table line / prompt / buffer limit
            ▼
         FLUSHING          (emit formatted or raw, back to SCANNING)
```

- **SCANNING**: Each line tested by all detectors. If any returns confidence > 0, transition to BUFFERING. Lines pass through untouched.
- **BUFFERING**: Lines accumulate per-table. Each new line tested for compatibility with detected type. Non-compatible line, prompt start, or row limit triggers flush.
- **FLUSHING**: Evaluate final confidence from full buffer. High confidence: full box-drawing redraw. Low confidence: only set `FixedWidth` (conservative hints). Buffer limit exceeded: emit raw, untouched.

### Line Suppression

New optional transformer interface:

```go
type LineSuppressor interface {
    ShouldSuppress(lineIdx int64) bool
}
```

`Pipeline.HandleLine` returns `bool` (true = line consumed, don't persist to scrollback). When tablefmt buffers a line, it clones it and suppresses the original. On flush, formatted lines are emitted via `insertFunc`.

Signature change:

```go
// Before
func (p *Pipeline) HandleLine(lineIdx int64, line *LogicalLine, isCommand bool)
// After
func (p *Pipeline) HandleLine(lineIdx int64, line *LogicalLine, isCommand bool) bool
```

VTerm `OnLineCommit` callback signature changes to match.

## Table Type Detectors

Each type has its own detector implementing:

```go
type tableDetector interface {
    Score(lines []string) float64
    Parse(lines []string) *tableStructure
}
```

### Detection Priority

| Type | Signal | Threshold | Example |
|------|--------|-----------|---------|
| Markdown | `\|` delimiters + `---`/`:---:` separator row | 0.95 | GFM tables |
| Pipe-separated | `\|` delimiters, no MD separator | 0.7 | Custom CLI pipe output |
| Space-aligned | Consistent multi-space gaps at same positions | 0.6 | `ls -l`, `ps aux`, `docker ps` |
| CSV/TSV | Comma/tab delimiters, consistent column count | 0.5 | `.csv` cat output |

All detectors re-score the buffer after each new line. Highest-scoring detector above its threshold wins. If none exceeds threshold after 20 lines, flush as low-confidence.

### Line Compatibility Predicate

Per-type check for "is this line still part of the table?":

- **MD**: Has `|` characters (or is blank)
- **Pipe**: Has `|` at expected positions (within tolerance)
- **Space-aligned**: Has content at expected column positions
- **CSV/TSV**: Expected number of delimiters (within +/-1)

Blank line or failed predicate ends the current table, triggers flush, returns to SCANNING.

## Rendering

### Common Pipeline

```
Raw buffered lines
  → Parse into column values (type-specific)
  → Compute column widths (max content width per column)
  → Build formatted cells with box-drawing
  → Replace original lines via suppress + insertFunc
```

### Box-Drawing Characters

```
╭───┬───╮     Rounded corners (top)
│   │   │     Vertical separators
├───┼───┤     Header/section separator
│   │   │
╰───┴───╯     Rounded corners (bottom)
```

One-space padding on each side of every cell: `│ value │`.

### Per-Type Rendering

**Markdown/Pipe**: Split on `|`, trim. MD separator row consumed, replaced by `├───┼───┤`. GFM alignment hints (`:---:`, `---:`, `:---`) honored.

**Space-aligned**: Column values from detected boundaries. First row treated as header if heuristics match (all-caps, non-numeric). Header gets middle separator; no header = no separator.

**CSV/TSV**: Split on delimiter. Quoted fields respected. First row = header.

### Column Alignment

- Numbers: right-aligned
- Everything else: left-aligned
- MD explicit alignment overrides auto-detection

### Column-Type Colorization

Each column classified as a single type based on majority of values:

| Type | Color |
|------|-------|
| Number | Yellow |
| DateTime | Cyan |
| Path | Green |
| Text | Default FG |

Rule: "all column, same type" — one classification per column, applied uniformly. Existing `classifyColumn` logic extracted from txfmt.

### FixedWidth

All formatted table lines get `FixedWidth` set. Conservative hints (low confidence) also set `FixedWidth` to preserve original alignment.

## Safety Limits

- **Per-table buffer limit**: Default 1000 rows, configurable via `max_buffer_rows`. If exceeded, flush entire buffer raw/untouched. No partial tables.
- **Detection timeout**: If no detector exceeds threshold after 20 buffered lines, flush as low-confidence (conservative hints only).

## Changes to txfmt

### Removed

- `scoreTable()` — detection moves to tablefmt
- `detectTableColumns`, `detectColumnsFromHeader`, `detectColumnsFromGaps`, `refineColumnEnds`
- `classifyColumn`, `classifyAllColumns`
- `colorizeTableCellsWithColumns`, `addTableSideBorders`, `makeBorderLine`
- `modeTable` constant and all table state (`tableLineCount`, `tableColumns`, `tableWidth`, `tableBordersActive`)
- `recolorizeTableBacklog`
- Table-related regexes (`reNonTableLine`, `reColNumber`, `reColDateTime`, `reColPath`)

### Unchanged

JSON, YAML, XML, log, code, markdown detection and Chroma colorization. Markdown mode in txfmt handles headings/bold/fences/lists — NOT markdown tables.

## Integration

txfmt naturally skips tablefmt-formatted lines because they already have non-default FG. No explicit coordination needed beyond pipeline ordering.

## Pipeline Changes

- `transformer.go`: Add `LineSuppressor` interface. `HandleLine` returns `bool`. Pipeline checks `ShouldSuppress` after each transformer.
- `vterm_memory_buffer.go`: `OnLineCommit` callback returns `bool`. If true, line not persisted.
- `term.go`: Pipeline config updated to include tablefmt before txfmt.
