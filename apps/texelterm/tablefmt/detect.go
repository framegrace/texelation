// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package tablefmt

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// ─── Common Types ────────────────────────────────────────────────────────────

// alignment represents column alignment.
type alignment int

const (
	alignLeft alignment = iota
	alignRight
	alignCenter
)

// columnInfo describes a detected column.
type columnInfo struct {
	align alignment
}

// tableStructure is the parsed result of a detected table.
type tableStructure struct {
	columns       []columnInfo
	headerRow     int              // index of header row in rows (-1 if none)
	rows          [][]string       // cell values per row (includes header)
	originalCells [][]parser.Cell  // original cells per row for color preservation
	tableType     tableType
}

// tableType identifies the detected table format.
type tableType int

const (
	tableNone tableType = iota
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

// ─── Markdown Detector ───────────────────────────────────────────────────────

// reMDSeparator matches a markdown table separator row.
// Each cell must have at least 3 dashes, optionally with colons for alignment.
var reMDSeparator = regexp.MustCompile(
	`^\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$`,
)

// reMDSepCell matches an individual separator cell for alignment detection.
var reMDSepCell = regexp.MustCompile(`^\s*(:?)-{3,}(:?)\s*$`)

type markdownDetector struct{}

// Score returns a confidence score for markdown table detection.
func (d *markdownDetector) Score(lines []string) float64 {
	if len(lines) < 2 {
		return 0
	}
	sepIdx := -1
	for i, ln := range lines {
		if reMDSeparator.MatchString(strings.TrimRight(ln, "\r\n")) {
			sepIdx = i
			break
		}
	}
	if sepIdx < 1 {
		return 0
	}

	// Count pipes in the separator to determine expected column count.
	sepCols := countPipeCells(lines[sepIdx])
	if sepCols < 2 {
		return 0
	}

	// Check consistency: lines around the separator should have similar pipe count.
	consistent := 0
	total := 0
	for i, ln := range lines {
		if i == sepIdx {
			continue
		}
		trimmed := strings.TrimSpace(ln)
		if len(trimmed) == 0 {
			continue
		}
		total++
		cols := countPipeCells(ln)
		if cols == sepCols {
			consistent++
		}
	}
	if total == 0 {
		return 0
	}
	ratio := float64(consistent) / float64(total)
	if ratio >= 0.7 {
		return 0.95 + 0.05*ratio
	}
	return ratio * 0.9
}

// Compatible returns true if the line could be part of a markdown table.
func (d *markdownDetector) Compatible(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return true
	}
	return strings.Contains(trimmed, "|")
}

// Parse extracts a tableStructure from markdown table lines.
func (d *markdownDetector) Parse(lines []string) *tableStructure {
	if len(lines) < 2 {
		return nil
	}

	// Find separator row.
	sepIdx := -1
	for i, ln := range lines {
		if reMDSeparator.MatchString(strings.TrimRight(ln, "\r\n")) {
			sepIdx = i
			break
		}
	}
	if sepIdx < 1 {
		return nil
	}

	// Parse alignment from separator cells.
	sepCells := splitPipeCells(lines[sepIdx])
	columns := make([]columnInfo, len(sepCells))
	for i, cell := range sepCells {
		columns[i] = columnInfo{align: parseMDAlignment(cell)}
	}
	numCols := len(columns)

	// Build rows, skipping the separator.
	var rows [][]string
	headerRow := -1
	for i, ln := range lines {
		if i == sepIdx {
			continue
		}
		trimmed := strings.TrimSpace(ln)
		if len(trimmed) == 0 {
			continue
		}
		cells := splitPipeCells(ln)
		// Pad or truncate to numCols.
		row := padRow(cells, numCols)
		if i == sepIdx-1 {
			headerRow = len(rows)
		}
		rows = append(rows, row)
	}

	return &tableStructure{
		columns:   columns,
		headerRow: headerRow,
		rows:      rows,
		tableType: tableMarkdown,
	}
}

// countPipeCells returns the number of cells in a pipe-delimited line.
func countPipeCells(line string) int {
	return len(splitPipeCells(line))
}

// splitPipeCells splits a pipe-delimited line into trimmed cell values,
// stripping leading and trailing pipes.
func splitPipeCells(line string) []string {
	line = strings.TrimRight(line, "\r\n")
	line = strings.TrimSpace(line)
	// Strip leading pipe.
	if strings.HasPrefix(line, "|") {
		line = line[1:]
	}
	// Strip trailing pipe.
	if strings.HasSuffix(line, "|") {
		line = line[:len(line)-1]
	}
	parts := strings.Split(line, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

// parseMDAlignment returns the alignment from a separator cell like ":---:", "---:", ":---".
func parseMDAlignment(cell string) alignment {
	m := reMDSepCell.FindStringSubmatch(cell)
	if m == nil {
		return alignLeft
	}
	left := m[1] == ":"
	right := m[2] == ":"
	switch {
	case left && right:
		return alignCenter
	case right:
		return alignRight
	default:
		return alignLeft
	}
}

// padRow ensures a row has exactly numCols cells, padding with empty strings
// or truncating as needed.
func padRow(cells []string, numCols int) []string {
	if len(cells) == numCols {
		return cells
	}
	row := make([]string, numCols)
	copy(row, cells)
	return row
}

// ─── Pipe-Separated Detector ─────────────────────────────────────────────────

type pipeDetector struct{}

// Score returns a confidence score for pipe-separated table detection.
// Returns 0 if a markdown separator is present (defer to markdownDetector).
func (d *pipeDetector) Score(lines []string) float64 {
	if len(lines) < 2 {
		return 0
	}

	// If any line matches the markdown separator, defer to markdown detector.
	for _, ln := range lines {
		if reMDSeparator.MatchString(strings.TrimRight(ln, "\r\n")) {
			return 0
		}
	}

	// Count pipe occurrences per non-blank line.
	freq := map[int]int{}
	total := 0
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if len(trimmed) == 0 {
			continue
		}
		total++
		n := strings.Count(trimmed, "|")
		if n > 0 {
			freq[n]++
		}
	}
	if total < 2 {
		return 0
	}

	// Find the most common pipe count.
	bestCount, bestFreq := 0, 0
	for cnt, f := range freq {
		if f > bestFreq || (f == bestFreq && cnt > bestCount) {
			bestCount = cnt
			bestFreq = f
		}
	}
	if bestCount < 1 {
		return 0
	}

	ratio := float64(bestFreq) / float64(total)
	if ratio >= 0.7 {
		return 0.7 + 0.2*ratio
	}
	return ratio * 0.7
}

// Compatible returns true if the line could be part of a pipe-separated table.
func (d *pipeDetector) Compatible(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return true
	}
	return strings.Contains(trimmed, "|")
}

// Parse extracts a tableStructure from pipe-separated lines.
func (d *pipeDetector) Parse(lines []string) *tableStructure {
	if len(lines) < 2 {
		return nil
	}

	var rows [][]string
	maxCols := 0
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if len(trimmed) == 0 {
			continue
		}
		cells := splitPipeCells(ln)
		if len(cells) > maxCols {
			maxCols = len(cells)
		}
		rows = append(rows, cells)
	}
	if len(rows) < 2 || maxCols < 2 {
		return nil
	}

	// Normalize row lengths.
	for i := range rows {
		rows[i] = padRow(rows[i], maxCols)
	}

	columns := make([]columnInfo, maxCols)
	for i := range columns {
		columns[i] = columnInfo{align: alignLeft}
	}

	return &tableStructure{
		columns:   columns,
		headerRow: 0,
		rows:      rows,
		tableType: tablePipeSeparated,
	}
}

// ─── Space-Aligned Detector ──────────────────────────────────────────────────
//
// Adapted from txfmt.go detectColumnsFromHeader and detectColumnsFromGaps.
// Uses a two-strategy approach:
// 1. Header-based: first line's word boundaries validated against data lines
// 2. Gap-based fallback: aligned double-space gaps bucketed by position

type spaceAlignedDetector struct{}

// Score returns a confidence score for space-aligned table detection.
func (d *spaceAlignedDetector) Score(lines []string) float64 {
	filtered := filterUsableLines(lines)
	if len(filtered) < 3 {
		return 0
	}

	// Reject blocks dominated by source code patterns.
	if codeLineFraction(filtered) > 0.3 {
		return 0
	}

	// Try header-based detection first.
	if cols := detectColumnBoundariesFromHeader(filtered); len(cols) >= 2 {
		return scoreFromBoundaryCount(len(cols))
	}

	// Fall back to gap-based detection.
	strong := countStrongGaps(filtered)
	if strong >= 2 {
		return scoreFromBoundaryCount(strong + 1) // N gaps -> N+1 columns
	}
	if strong == 1 {
		return 0.5
	}
	return 0
}

// Compatible returns true if the line could be part of a space-aligned table.
// Requires either a double-space gap or enough space-separated fields to be a
// plausible multi-column row. This breaks buffering on non-table lines like
// directory headers ("./apps/clock:") or summary lines ("total 24") in ls -lR
// output, enabling each directory listing to be detected as a separate table,
// while still accepting rows where wide values collapse all gaps to single
// spaces (e.g., ls -l with large file sizes).
func (d *spaceAlignedDetector) Compatible(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return true // blank lines are OK inside a table
	}
	if len(trimmed) < 10 {
		return false // too short for a table row
	}
	if looksLikeCode(trimmed) {
		return false
	}
	// Double-space gap is the standard signal for column separation.
	if strings.Contains(trimmed, "  ") {
		return true
	}
	// Fallback: rows with wide values may lose all double-space gaps but
	// still have many single-space-separated fields (e.g., 9 in ls -l).
	return len(strings.Fields(trimmed)) >= 4
}

// Parse extracts a tableStructure from space-aligned lines.
func (d *spaceAlignedDetector) Parse(lines []string) *tableStructure {
	filtered := filterUsableLines(lines)
	if len(filtered) < 3 {
		return nil
	}

	// Detect column boundaries.
	var boundaries []int
	if cols := detectColumnBoundariesFromHeader(filtered); len(cols) >= 2 {
		boundaries = cols
	} else {
		boundaries = detectColumnBoundariesFromGaps(filtered)
	}
	if len(boundaries) < 2 {
		return nil
	}

	// Extract cell values.
	numCols := len(boundaries)
	maxWidth := maxRuneWidth(lines)
	var rows [][]string
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if len(trimmed) == 0 {
			continue
		}
		runes := []rune(strings.TrimRight(ln, "\r\n"))
		row := make([]string, numCols)
		for c := 0; c < numCols; c++ {
			start := boundaries[c]
			end := maxWidth
			if c+1 < numCols {
				end = boundaries[c+1]
			}
			if start >= len(runes) {
				row[c] = ""
				continue
			}
			if end > len(runes) {
				end = len(runes)
			}
			row[c] = strings.TrimSpace(string(runes[start:end]))
		}
		rows = append(rows, row)
	}

	columns := make([]columnInfo, numCols)
	for i := range columns {
		columns[i] = columnInfo{align: alignLeft}
	}

	headerRow := -1
	if len(rows) > 0 && looksLikeHeader(rows[0]) {
		headerRow = 0
	}

	return &tableStructure{
		columns:   columns,
		headerRow: headerRow,
		rows:      rows,
		tableType: tableSpaceAligned,
	}
}

// filterUsableLines returns non-blank lines with content.
func filterUsableLines(lines []string) []string {
	filtered := make([]string, 0, len(lines))
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if len(trimmed) > 0 {
			filtered = append(filtered, ln)
		}
	}
	return filtered
}

// detectColumnBoundariesFromHeader finds column start positions using the
// first line as a header. All word-start positions (space→non-space transitions)
// are candidates; they are validated against data lines to filter false positives.
// Single-space header gaps (like ps's "PID TTY") require stronger data line
// validation (70%) than multi-space gaps (50%).
func detectColumnBoundariesFromHeader(lines []string) []int {
	if len(lines) < 3 {
		return nil
	}

	header := strings.TrimRight(lines[0], "\r\n")
	headerRunes := []rune(header)
	if len(headerRunes) < 10 {
		return nil
	}

	// Find ALL space→non-space transitions with their gap widths.
	type candidate struct {
		pos      int
		gapWidth int
	}
	var candidates []candidate
	for i, r := range headerRunes {
		if r == ' ' || i == 0 {
			continue
		}
		if headerRunes[i-1] != ' ' {
			continue
		}
		gapStart := i - 1
		for gapStart > 0 && headerRunes[gapStart-1] == ' ' {
			gapStart--
		}
		candidates = append(candidates, candidate{pos: i, gapWidth: i - gapStart})
	}
	// Always include position 0 as the first column (or the first non-space).
	firstNonSpace := 0
	for firstNonSpace < len(headerRunes) && headerRunes[firstNonSpace] == ' ' {
		firstNonSpace++
	}
	if firstNonSpace >= len(headerRunes) || len(candidates) < 1 {
		return nil
	}

	// Validate each boundary by checking data lines have a space at the gap.
	// Single-space header gaps need stronger validation to reject prose.
	dataLines := lines[1:]
	validStarts := []int{firstNonSpace}
	for _, c := range candidates {
		gapPos := c.pos - 1
		if gapPos < 0 {
			continue
		}
		hasSpace, total := 0, 0
		for _, ln := range dataLines {
			lnRunes := []rune(strings.TrimRight(ln, "\r\n"))
			if len(lnRunes) < 10 {
				continue
			}
			total++
			if gapPos < len(lnRunes) && lnRunes[gapPos] == ' ' {
				hasSpace++
			}
		}
		if total < 2 {
			continue
		}
		// Single-space header gaps need more evidence: 70% threshold and at
		// least 5 data lines to avoid coincidental alignment in prose.
		threshold := 50
		minLines := 2
		if c.gapWidth < 2 {
			threshold = 70
			minLines = 5
		}
		if total < minLines {
			continue
		}
		if hasSpace*100/total >= threshold {
			validStarts = append(validStarts, c.pos)
		}
	}
	if len(validStarts) < 2 {
		return nil
	}

	// Extend first column to start at 0 if there's leading whitespace.
	if validStarts[0] > 0 {
		validStarts[0] = 0
	}
	return validStarts
}

// detectColumnBoundariesFromGaps finds column boundaries using aligned
// double-space gaps. Returns column start positions.
func detectColumnBoundariesFromGaps(lines []string) []int {
	type posKey int
	counts := map[posKey]int{}
	usable := 0
	for _, ln := range lines {
		ln = strings.TrimRight(ln, "\r\n")
		if len(strings.TrimSpace(ln)) == 0 || utf8.RuneCountInString(ln) < 20 {
			continue
		}
		usable++
		runes := []rune(ln)
		for i := 0; i < len(runes)-2; i++ {
			if runes[i] == ' ' && runes[i+1] == ' ' {
				bucket := (i / 4) * 4
				counts[posKey(bucket)]++
				for i+1 < len(runes) && runes[i+1] == ' ' {
					i++
				}
			}
		}
	}
	if usable < 2 {
		return nil
	}

	// Collect strong gap bucket positions (>= 50% of usable lines).
	threshold := usable / 2
	if threshold < 1 {
		threshold = 1
	}
	var strongBuckets []int
	for k, c := range counts {
		if c >= threshold {
			strongBuckets = append(strongBuckets, int(k))
		}
	}
	sort.Ints(strongBuckets)
	if len(strongBuckets) == 0 {
		return nil
	}

	// Refine each bucket into a precise gap range.
	// Track both the full extent [start, maxEnd) and the minimum end (minEnd)
	// which is where the widest value starts — the safe column boundary for
	// right-aligned fields.
	type gapRange struct{ start, minEnd, maxEnd int }
	gaps := make([]gapRange, 0, len(strongBuckets))
	for _, bucket := range strongBuckets {
		gs, minE, maxE := 9999, 9999, 0
		for _, ln := range lines {
			ln = strings.TrimRight(ln, "\r\n")
			runes := []rune(ln)
			if len(runes) < 20 {
				continue
			}
			for i := 0; i < len(runes)-1; i++ {
				if runes[i] == ' ' && runes[i+1] == ' ' {
					runStart := i
					for i+1 < len(runes) && runes[i+1] == ' ' {
						i++
					}
					runEnd := i + 1
					b := (runStart / 4) * 4
					if b == bucket {
						if runStart < gs {
							gs = runStart
						}
						if runEnd < minE {
							minE = runEnd
						}
						if runEnd > maxE {
							maxE = runEnd
						}
					}
				}
			}
		}
		if gs < maxE {
			gaps = append(gaps, gapRange{gs, minE, maxE})
		}
	}
	if len(gaps) == 0 {
		return nil
	}

	// Merge overlapping or adjacent gaps.
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].start < gaps[j].start })
	merged := []gapRange{gaps[0]}
	for _, g := range gaps[1:] {
		last := &merged[len(merged)-1]
		if g.start <= last.maxEnd {
			if g.maxEnd > last.maxEnd {
				last.maxEnd = g.maxEnd
			}
			if g.minEnd < last.minEnd {
				last.minEnd = g.minEnd
			}
		} else {
			merged = append(merged, g)
		}
	}

	// Build column start positions from gap boundaries. For each gap, scan
	// ALL lines (including those without double-space gaps) to find the
	// earliest content start. This handles right-aligned fields like file
	// sizes where the widest value may have only a single-space separator.
	starts := make([]int, 0, len(merged)+1)
	starts = append(starts, 0) // first column always starts at 0
	for _, g := range merged {
		colStart := g.minEnd
		for _, ln := range lines {
			runes := []rune(strings.TrimRight(ln, "\r\n"))
			if g.start >= len(runes) || runes[g.start] != ' ' {
				continue // line doesn't have a gap here
			}
			for pos := g.start + 1; pos < len(runes); pos++ {
				if runes[pos] != ' ' {
					if pos < colStart {
						colStart = pos
					}
					break
				}
			}
		}
		starts = append(starts, colStart)
	}
	return starts
}

// countStrongGaps counts double-space gap positions that appear in >= 50% of
// usable lines. Used for scoring.
func countStrongGaps(lines []string) int {
	type posKey int
	counts := map[posKey]int{}
	usable := 0
	for _, ln := range lines {
		ln = strings.TrimRight(ln, "\r\n")
		if len(strings.TrimSpace(ln)) == 0 || utf8.RuneCountInString(ln) < 20 {
			continue
		}
		usable++
		runes := []rune(ln)
		for i := 0; i < len(runes)-2; i++ {
			if runes[i] == ' ' && runes[i+1] == ' ' {
				bucket := (i / 4) * 4
				counts[posKey(bucket)]++
				for i+1 < len(runes) && runes[i+1] == ' ' {
					i++
				}
			}
		}
	}
	if usable < 2 {
		return 0
	}
	threshold := usable / 2
	if threshold < 1 {
		threshold = 1
	}
	strong := 0
	for _, c := range counts {
		if c >= threshold {
			strong++
		}
	}
	return strong
}

// scoreFromBoundaryCount returns a score scaled by the number of detected columns.
func scoreFromBoundaryCount(cols int) float64 {
	if cols < 2 {
		return 0
	}
	// 2 columns -> 0.6, 3 -> 0.7, 4 -> 0.8, 5+ -> 0.9
	score := 0.5 + float64(cols)*0.1
	if score > 0.9 {
		score = 0.9
	}
	return score
}

// codeLineFraction returns the fraction of non-blank lines that look like
// source code. Checks both keyword/brace patterns and tab indentation (terminal
// command output uses spaces, not leading tabs). Used to reject blocks dominated
// by code patterns.
func codeLineFraction(lines []string) float64 {
	if len(lines) == 0 {
		return 0
	}
	codeCount := 0
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if len(trimmed) == 0 {
			continue
		}
		if looksLikeCode(trimmed) || strings.HasPrefix(ln, "\t") {
			codeCount++
		}
	}
	return float64(codeCount) / float64(len(lines))
}

// looksLikeCode returns true if a trimmed line matches common source code
// patterns (braces, keywords like func/return/if/for). Safe for real tables:
// command output from kubectl, ps, docker, ls -la etc. doesn't contain these.
func looksLikeCode(trimmed string) bool {
	// Lone braces
	if trimmed == "{" || trimmed == "}" || trimmed == "})" || trimmed == ")," {
		return true
	}
	// Lines ending with opening brace (function/struct/if definitions)
	if strings.HasSuffix(trimmed, "{") {
		return true
	}
	// Common code constructs
	for _, kw := range []string{":=", "func ", "return ", "if ", "for ", "switch ", "import ", "package "} {
		if strings.Contains(trimmed, kw) {
			return true
		}
	}
	return false
}

// maxRuneWidth returns the maximum rune width across all lines.
func maxRuneWidth(lines []string) int {
	maxW := 0
	for _, ln := range lines {
		w := utf8.RuneCountInString(strings.TrimRight(ln, "\r\n"))
		if w > maxW {
			maxW = w
		}
	}
	return maxW
}

// looksLikeHeader returns true if a row looks like a table header (contains
// uppercase words or non-numeric values).
func looksLikeHeader(row []string) bool {
	upperCount := 0
	for _, cell := range row {
		cell = strings.TrimSpace(cell)
		if len(cell) == 0 {
			continue
		}
		// If cell is all uppercase letters and allowed punctuation, count it.
		isUpper := true
		for _, r := range cell {
			if !unicode.IsUpper(r) && r != '_' && r != '-' && r != '%' && r != '/' && r != ' ' {
				isUpper = false
				break
			}
		}
		if isUpper {
			upperCount++
		}
	}
	return upperCount >= len(row)/2
}

// ─── CSV/TSV Detector ────────────────────────────────────────────────────────

type csvDetector struct {
	detectedDelim byte
	detectedCount int
}

// Score returns a confidence score for CSV/TSV detection.
func (d *csvDetector) Score(lines []string) float64 {
	if len(lines) < 3 {
		return 0
	}

	// Try comma first, then tab.
	for _, delim := range []byte{',', '\t'} {
		score := d.scoreDelim(lines, delim)
		if score > 0 {
			return score
		}
	}
	return 0
}

// scoreDelim scores lines for a specific delimiter.
func (d *csvDetector) scoreDelim(lines []string, delim byte) float64 {
	freq := map[int]int{}
	total := 0
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if len(trimmed) == 0 {
			continue
		}
		total++
		n := countDelimitersOutsideQuotes(trimmed, delim)
		freq[n]++
	}
	if total < 3 {
		return 0
	}

	// Find the most common delimiter count.
	bestCount, bestFreq := 0, 0
	for cnt, f := range freq {
		if f > bestFreq || (f == bestFreq && cnt > bestCount) {
			bestCount = cnt
			bestFreq = f
		}
	}
	// Need at least 3 columns (2 delimiters). A single delimiter per line
	// is too weak a signal — matches JSON trailing commas, prose, etc.
	if bestCount < 2 {
		return 0
	}

	ratio := float64(bestFreq) / float64(total)
	if ratio >= 0.8 {
		d.detectedDelim = delim
		d.detectedCount = bestCount
		return 0.5 + 0.4*ratio
	}
	return 0
}

// Compatible returns true if the line could be part of a CSV/TSV table.
// Before Score has been called (detectedDelim == 0), falls back to checking
// for the presence of commas or tabs.
func (d *csvDetector) Compatible(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return true
	}
	if d.detectedDelim == 0 {
		return strings.Count(trimmed, ",") >= 2 || strings.Count(trimmed, "\t") >= 2
	}
	n := countDelimitersOutsideQuotes(trimmed, d.detectedDelim)
	diff := n - d.detectedCount
	return diff >= -1 && diff <= 1
}

// Parse extracts a tableStructure from CSV/TSV lines.
func (d *csvDetector) Parse(lines []string) *tableStructure {
	if len(lines) < 2 {
		return nil
	}

	// Detect delimiter if not already set by Score.
	delim := d.detectedDelim
	if delim == 0 {
		// Try comma first, then tab.
		for _, tryDelim := range []byte{',', '\t'} {
			if d.scoreDelim(lines, tryDelim) > 0 {
				delim = d.detectedDelim
				break
			}
		}
	}
	if delim == 0 {
		return nil
	}

	var rows [][]string
	maxCols := 0
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if len(trimmed) == 0 {
			continue
		}
		cells := splitCSVLine(trimmed, delim)
		if len(cells) > maxCols {
			maxCols = len(cells)
		}
		rows = append(rows, cells)
	}
	if len(rows) < 2 || maxCols < 2 {
		return nil
	}

	// Normalize row lengths.
	for i := range rows {
		rows[i] = padRow(rows[i], maxCols)
	}

	columns := make([]columnInfo, maxCols)
	for i := range columns {
		columns[i] = columnInfo{align: alignLeft}
	}

	return &tableStructure{
		columns:   columns,
		headerRow: 0,
		rows:      rows,
		tableType: tableCSV,
	}
}

// countDelimitersOutsideQuotes counts delimiter occurrences not inside quoted fields.
func countDelimitersOutsideQuotes(line string, delim byte) int {
	count := 0
	inQuote := false
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '"':
			inQuote = !inQuote
		case !inQuote && line[i] == delim:
			count++
		}
	}
	return count
}

// splitCSVLine splits a line by the given delimiter, respecting RFC 4180
// quoting: fields may be enclosed in double quotes, and double quotes within
// quoted fields are escaped by doubling ("").
func splitCSVLine(line string, delim byte) []string {
	var fields []string
	var field strings.Builder
	inQuote := false

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case inQuote:
			if ch == '"' {
				// Check for escaped quote ("").
				if i+1 < len(line) && line[i+1] == '"' {
					field.WriteByte('"')
					i++
				} else {
					inQuote = false
				}
			} else {
				field.WriteByte(ch)
			}
		case ch == '"':
			inQuote = true
		case ch == delim:
			fields = append(fields, strings.TrimSpace(field.String()))
			field.Reset()
		default:
			field.WriteByte(ch)
		}
	}
	fields = append(fields, strings.TrimSpace(field.String()))
	return fields
}
