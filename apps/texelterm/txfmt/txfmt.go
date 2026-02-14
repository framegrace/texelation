// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Package txfmt is an inline output formatter for texelterm.
// It hooks into the line-commit boundary (memoryBufferLineFeed) to colorize
// command output before it enters scrollback. Detection and colorization
// logic is ported from cmd/txfmt/main.go, operating directly on parser.Cell
// slices instead of ANSI escape strings.
package txfmt

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	enry "github.com/go-enry/go-enry/v2"
	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/transformer"
)

func init() {
	transformer.Register("txfmt", func(cfg transformer.Config) (transformer.Transformer, error) {
		styleName, _ := cfg["style"].(string)
		return New(styleName), nil
	})
}

// mode represents a detected output format.
type mode string

const (
	modePlain mode = "plain"
	modeJSON  mode = "json"
	modeYAML  mode = "yaml"
	modeXML   mode = "xml"
	modeLog   mode = "log"
	modeTable    mode = "table"
	modeCode     mode = "code"
	modeMarkdown mode = "markdown"
)

// columnType classifies a table column for per-column colorization.
type columnType int

const (
	colText     columnType = iota // default FG
	colNumber                     // yellow
	colDateTime                   // cyan + dim
	colPath                       // green
)

// tableColumn describes a detected column boundary and its inferred type.
type tableColumn struct {
	start int        // rune index of column start (inclusive)
	end   int        // rune index of column end (exclusive)
	ctype columnType // inferred after classification
}

// borderPosition selects horizontal border style (top, middle, bottom).
type borderPosition int

const (
	borderTop    borderPosition = iota // ╭─┬─╮
	borderMiddle                       // ├─┼─┤
	borderBottom                       // ╰─┴─╯
)

// Color constants as parser.Color values (replacing ANSI escape strings).
var (
	colorRed     = parser.Color{Mode: parser.ColorModeStandard, Value: 1}
	colorGreen   = parser.Color{Mode: parser.ColorModeStandard, Value: 2}
	colorYellow  = parser.Color{Mode: parser.ColorModeStandard, Value: 3}
	colorBlue    = parser.Color{Mode: parser.ColorModeStandard, Value: 4}
	colorMagenta = parser.Color{Mode: parser.ColorModeStandard, Value: 5}
	colorCyan    = parser.Color{Mode: parser.ColorModeStandard, Value: 6}
)

var (
	reISOTime = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?\b`)
	reSyslog  = regexp.MustCompile(`\b(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}\b`)
	reLevel   = regexp.MustCompile(`\b(INFO|WARN|WARNING|ERR|ERROR|DEBUG|TRACE|FATAL)\b`)
	reKV      = regexp.MustCompile(`\b[a-zA-Z_][a-zA-Z0-9_]*=([^\s]+)\b`)
	reYAMLKey = regexp.MustCompile(`^\s*[\w.-]+\s*:\s+.*$`)
	reXMLish  = regexp.MustCompile(`^\s*<(/?[\w:-]+)(\s+[^>]*)?>`)
	reJSONish = regexp.MustCompile(`^\s*[\{\[]`)
	reCodeTok   = regexp.MustCompile(`\b(func|package|import|class|def|fn|let|const|var|public|private|return|if|else|for|while|switch|case)\b`)
	reMDHeading = regexp.MustCompile(`^#{1,6}\s+\S`)
	reMDBold    = regexp.MustCompile(`\*\*[^*]+\*\*|__[^_]+__`)
	reMDFence   = regexp.MustCompile("^```")
	reMDList    = regexp.MustCompile(`^(\s*[-*+]|\s*\d+\.)\s`)

	// Column type classification regexes.
	reColNumber   = regexp.MustCompile(`^-?[0-9][0-9,.]*%?$`)
	reColDateTime = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}|\d{1,3}[dhms]|<?\d+[dhms](\d+[dhms])*>?|\d{2}:\d{2}(:\d{2})?|\d{1,2}[A-Z][a-z]{2}\d{2,4}|\d+\.\d+[dhms])$`)
	reColPath     = regexp.MustCompile(`[/\\]|^\.\w+$|^[\w.-]+\.\w{1,5}$`)

	// Non-table summary lines to filter out (e.g. "total N" from ls -l).
	reNonTableLine = regexp.MustCompile(`^total \d+$`)
)

// Formatter is the inline output formatter. It detects the format of command
// output and colorizes cells in-place on the committed LogicalLine.
type Formatter struct {
	det                 detector
	wasCommand          bool
	hasShellIntegration bool
	tableLineCount      int
	tableColumns        []tableColumn         // column metadata, persists for streaming lines
	tableWidth          int                   // max rune width across backlog lines (for borders)
	tableBordersActive  bool                  // true after top/header borders have been inserted
	backlog             []*backlogEntry       // lines buffered during detection
	style               *chroma.Style
	chromaContext        []string              // previous lines kept as lexer context
	codeLexer           string                // inferred language for modeCode (e.g. "go", "python")
	codeLexerMethod     string                // detection method (e.g. "shebang", "classifier")
	insertFunc          func(beforeIdx int64, cells []parser.Cell)
}

// backlogEntry stores a line and its global index for deferred processing.
type backlogEntry struct {
	lineIdx int64
	line    *parser.LogicalLine
}

// SetInsertFunc implements transformer.LineInserter.
func (f *Formatter) SetInsertFunc(fn func(beforeIdx int64, cells []parser.Cell)) {
	f.insertFunc = fn
}

// New creates a new Formatter. styleName selects the Chroma theme
// (e.g. "catppuccin-mocha", "dracula"). Empty string uses the default.
func New(styleName string) *Formatter {
	return &Formatter{
		det: detector{
			maxSampleLines: 20,
			requiredWins:   2,
		},
		style: chromaStyle(styleName),
	}
}

// NotifyPromptStart records that shell integration is active.
// Called from the OnPromptStart hook.
func (f *Formatter) NotifyPromptStart() {
	f.hasShellIntegration = true
}

// HandleLine is the OnLineCommit callback. It detects and colorizes command
// output lines in-place.
func (f *Formatter) HandleLine(lineIdx int64, line *parser.LogicalLine, isCommand bool) {
	// If no shell integration, treat all lines as command output
	effectiveIsCommand := isCommand || !f.hasShellIntegration

	// Reset detector on command→prompt transition
	if f.wasCommand && !effectiveIsCommand {
		// Insert bottom border before the prompt line if table borders are active.
		if f.tableBordersActive && f.insertFunc != nil && f.tableWidth > 0 {
			botBorder := makeBorderLine(f.tableWidth, f.tableColumns, borderBottom)
			f.insertFunc(lineIdx, botBorder)
		}
		f.det.reset()
		f.tableLineCount = 0
		f.tableColumns = nil
		f.tableWidth = 0
		f.tableBordersActive = false
		f.backlog = f.backlog[:0]
		f.chromaContext = f.chromaContext[:0]
		f.codeLexer = ""
		f.codeLexerMethod = ""
	}
	f.wasCommand = effectiveIsCommand

	if !effectiveIsCommand {
		return
	}

	plainText := extractPlainText(line)
	if plainText == "" {
		return
	}

	wasLocked := f.det.locked
	if !wasLocked {
		f.det.addSample(plainText)
		f.backlog = append(f.backlog, &backlogEntry{lineIdx: lineIdx, line: line})
	}

	m := f.det.current()

	if f.det.locked && !wasLocked {
		// Detection just locked — re-colorize all buffered lines
		f.recolorizeBacklog(m)
	} else if f.det.locked {
		// Normal post-lock path
		f.colorize(line, m)
		if m == modeLog || m == modeTable {
			line.FixedWidth = len(line.Cells)
		}
	}
	// else: still detecting, line is in backlog, no colorization yet
}

// backlogLines returns just the LogicalLine pointers from the backlog.
func (f *Formatter) backlogLines() []*parser.LogicalLine {
	lines := make([]*parser.LogicalLine, len(f.backlog))
	for i, e := range f.backlog {
		lines[i] = e.line
	}
	return lines
}

// recolorizeBacklog applies the locked mode to all lines buffered during detection.
func (f *Formatter) recolorizeBacklog(m mode) {
	f.tableLineCount = 0
	lines := f.backlogLines()

	if lexName, ok := chromaLexerName[m]; ok {
		// For modeCode, infer the specific language from sample content.
		if m == modeCode {
			result := inferLanguage(f.det.sampleLines)
			lexName = result.name
			f.codeLexer = result.name
			f.codeLexerMethod = result.method
		}
		// Batch tokenize all backlog lines together for full context.
		chromaColorizeLines(lines, lexName, f.style)
		// Populate chromaContext from backlog for future streaming lines.
		for _, line := range lines {
			f.chromaContext = append(f.chromaContext, extractPlainText(line))
		}
		if len(f.chromaContext) > maxChromaContext {
			f.chromaContext = f.chromaContext[len(f.chromaContext)-maxChromaContext:]
		}
	} else if m == modeTable {
		f.recolorizeTableBacklog(lines)
	} else {
		for _, line := range lines {
			f.colorize(line, m)
		}
	}

	// Insert a mode indicator as a new line before the first backlog line.
	if len(f.backlog) > 0 && m != modePlain {
		label := "auto-color as: " + string(m)
		if m == modeCode && f.codeLexer != "" {
			label = "auto-color as: " + f.codeLexer
			if f.codeLexerMethod != "" {
				label += " (" + f.codeLexerMethod + ")"
			}
		}
		f.insertModeIndicator(f.backlog[0].lineIdx, label)
	}

	for _, line := range lines {
		if m == modeLog || m == modeTable {
			line.FixedWidth = len(line.Cells)
		}
	}
	f.backlog = f.backlog[:0]
}

// recolorizeTableBacklog handles the full table pipeline: detect columns,
// classify types, colorize, add borders, and insert border lines.
func (f *Formatter) recolorizeTableBacklog(lines []*parser.LogicalLine) {
	// 1. Extract plain text for column detection.
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = extractPlainText(line)
	}

	// 2. Detect columns and classify types.
	f.tableColumns = detectTableColumns(plainLines)
	classifyAllColumns(plainLines, f.tableColumns)

	// 3. Compute max line width (in runes).
	f.tableWidth = 0
	for _, s := range plainLines {
		w := utf8.RuneCountInString(s)
		if w > f.tableWidth {
			f.tableWidth = w
		}
	}

	// 4. Colorize each line with per-column colors.
	for _, line := range lines {
		f.tableLineCount++
		colorizeTableCellsWithColumns(line, f.tableLineCount, f.tableColumns)
		addTableSideBorders(line, f.tableWidth, f.tableColumns)
		line.FixedWidth = len(line.Cells)
	}

	// 5. Insert horizontal border lines (top border + header separator only).
	// Bottom border is deferred to command→prompt transition since we don't
	// know where the table ends yet (streaming lines continue after backlog).
	if f.insertFunc == nil || len(f.backlog) == 0 {
		return
	}
	offset := int64(0)

	// Top border: before the first data line.
	topBorder := makeBorderLine(f.tableWidth, f.tableColumns, borderTop)
	f.insertFunc(f.backlog[0].lineIdx+offset, topBorder)
	offset++

	// Header separator: after the first data line (header).
	if len(f.backlog) > 1 {
		midBorder := makeBorderLine(f.tableWidth, f.tableColumns, borderMiddle)
		f.insertFunc(f.backlog[1].lineIdx+offset, midBorder)
	}

	f.tableBordersActive = true
}

// extractPlainText extracts runes from cells with default FG color.
// These are the cells that are eligible for colorization.
func extractPlainText(line *parser.LogicalLine) string {
	var b strings.Builder
	for _, cell := range line.Cells {
		if cell.Rune != 0 {
			b.WriteRune(cell.Rune)
		}
	}
	return b.String()
}

// chromaLexerName maps a detected mode to a Chroma lexer name.
var chromaLexerName = map[mode]string{
	modeJSON:     "json",
	modeYAML:     "yaml",
	modeXML:      "xml",
	modeMarkdown: "markdown",
	modeCode:     "", // resolved at runtime by inferLanguage
}

// commonLanguages is the curated candidate set for the Bayesian classifier.
// Keeping this focused avoids false positives from obscure languages (e.g.
// Golo, GDScript3) that share keywords with common ones.
var commonLanguages = []string{
	"C", "C++", "C#", "CSS", "Dart", "Elixir", "Erlang",
	"Go", "Groovy", "HTML", "Haskell", "Java", "JavaScript",
	"Kotlin", "Lua", "Markdown", "Objective-C",
	"PHP", "Perl", "PowerShell", "Python", "R", "Ruby",
	"Rust", "Scala", "Shell", "Swift", "TypeScript", "Zig",
}

// langResult holds the detected language and how it was detected.
type langResult struct {
	name   string // chroma lexer name (e.g. "go", "python")
	method string // detection method (e.g. "shebang", "classifier")
}

// inferLanguage detects the programming language from sample lines using
// go-enry (GitHub's Linguist port). It uses a four-tier strategy:
// shebang → modeline → Go heuristic → Bayesian classifier.
func inferLanguage(lines []string) langResult {
	content := []byte(strings.Join(lines, "\n") + "\n")

	// Tier 1: shebang (high confidence).
	if lang, safe := enry.GetLanguageByShebang(content); safe {
		return langResult{enryToChroma(lang), "shebang"}
	}
	// Tier 2: editor modeline (high confidence).
	if lang, safe := enry.GetLanguageByModeline(content); safe {
		return langResult{enryToChroma(lang), "modeline"}
	}
	// Tier 3: Go heuristic — "package " + "func " is distinctively Go.
	// The classifier confuses Go with other languages (Golo, R) due to
	// overlapping token distributions.
	text := string(content)
	if strings.Contains(text, "package ") && strings.Contains(text, "func ") {
		return langResult{"go", "heuristic"}
	}
	// Tier 4: Bayesian classifier with curated candidate set.
	if lang, _ := enry.GetLanguageByClassifier(content, commonLanguages); lang != "" {
		return langResult{enryToChroma(lang), "classifier"}
	}
	return langResult{}
}

// enryToChroma maps go-enry language names to Chroma lexer aliases.
// Most names match, but a few differ.
var enryToChromaMap = map[string]string{
	"Shell": "bash",
}

func enryToChroma(enryName string) string {
	if alias, ok := enryToChromaMap[enryName]; ok {
		return alias
	}
	return strings.ToLower(enryName)
}

// colorize applies format-specific colorization to cells with default FG.
func (f *Formatter) colorize(line *parser.LogicalLine, m mode) {
	switch m {
	case modeLog:
		colorizeLogCells(line)
	case modeTable:
		f.tableLineCount++
		colorizeTableCellsWithColumns(line, f.tableLineCount, f.tableColumns)
		if f.tableWidth > 0 {
			addTableSideBorders(line, f.tableWidth, f.tableColumns)
		}
	case modeJSON, modeYAML, modeXML, modeCode, modeMarkdown:
		plainText := extractPlainText(line)
		lexName := chromaLexerName[m]
		if m == modeCode {
			lexName = f.codeLexer
		}
		chromaColorizeWithContext(line, f.chromaContext, lexName, f.style)
		f.chromaContext = append(f.chromaContext, plainText)
		if len(f.chromaContext) > maxChromaContext {
			f.chromaContext = f.chromaContext[len(f.chromaContext)-maxChromaContext:]
		}
	}
}

// ─── Detector ───────────────────────────────────────────────────────────────

type detector struct {
	maxSampleLines int
	requiredWins   int
	sampleLines    []string
	locked         bool
	lockedMode     mode
	lastBest       mode
	stableWins     int
}

func (d *detector) current() mode {
	if d.locked {
		return d.lockedMode
	}
	return d.lastBest
}

func (d *detector) reset() {
	d.sampleLines = d.sampleLines[:0]
	d.locked = false
	d.lockedMode = modePlain
	d.lastBest = modePlain
	d.stableWins = 0
}

func (d *detector) addSample(line string) {
	if len(d.sampleLines) < d.maxSampleLines {
		d.sampleLines = append(d.sampleLines, line)
	}

	scores := d.score()
	best := pickBest(scores)

	if best == d.lastBest && best != modePlain {
		d.stableWins++
	} else {
		d.stableWins = 0
	}
	d.lastBest = best

	if d.stableWins >= d.requiredWins || len(d.sampleLines) >= d.maxSampleLines {
		d.locked = true
		d.lockedMode = best
	}
}

func (d *detector) score() map[mode]float64 {
	s := map[mode]float64{
		modePlain:    0.2,
		modeJSON:     0,
		modeYAML:     0,
		modeXML:      0,
		modeLog:      0,
		modeTable:    0,
		modeCode:     0,
		modeMarkdown: 0,
	}

	lines := d.sampleLines
	text := strings.Join(lines, "\n")

	// JSON
	if reJSONish.MatchString(strings.TrimSpace(text)) {
		s[modeJSON] += 0.8
	}
	quotes := float64(strings.Count(text, `"`))
	colons := float64(strings.Count(text, `:`))
	braces := float64(strings.Count(text, `{`) + strings.Count(text, `}`) + strings.Count(text, `[`) + strings.Count(text, `]`))
	if quotes > 6 && colons > 2 && braces > 2 {
		s[modeJSON] += 0.7
	}
	if looksLikeCompleteJSON(strings.TrimSpace(text)) {
		var tmp any
		if json.Unmarshal([]byte(strings.TrimSpace(text)), &tmp) == nil {
			s[modeJSON] += 2.5
		}
	}

	// YAML
	yamlKeyLines := 0
	dashLines := 0
	for _, ln := range lines {
		if reYAMLKey.MatchString(ln) {
			yamlKeyLines++
		}
		if strings.HasPrefix(strings.TrimSpace(ln), "- ") {
			dashLines++
		}
	}
	if yamlKeyLines >= 3 {
		s[modeYAML] += 1.2
	}
	if dashLines >= 3 {
		s[modeYAML] += 0.6
	}
	if strings.HasPrefix(strings.TrimSpace(text), "---") {
		s[modeYAML] += 0.8
	}

	// XML
	xmlLines := 0
	for _, ln := range lines {
		if reXMLish.MatchString(ln) {
			xmlLines++
		}
	}
	if xmlLines >= 2 {
		s[modeXML] += 1.0
	}

	// Logs
	logHits := 0
	for _, ln := range lines {
		if reISOTime.MatchString(ln) || reSyslog.MatchString(ln) {
			logHits++
		}
		if reLevel.MatchString(ln) {
			logHits++
		}
		if reKV.MatchString(ln) {
			logHits++
		}
	}
	if logHits >= 6 {
		s[modeLog] += 1.4
	} else if logHits >= 3 {
		s[modeLog] += 0.8
	}

	// Markdown
	mdHits := 0
	for _, ln := range lines {
		if reMDHeading.MatchString(ln) {
			mdHits += 2
		}
		if reMDBold.MatchString(ln) {
			mdHits++
		}
		if reMDFence.MatchString(ln) {
			mdHits += 2
		}
		if reMDList.MatchString(ln) {
			mdHits++
		}
	}
	if mdHits >= 4 {
		s[modeMarkdown] += 1.2
	} else if mdHits >= 2 {
		s[modeMarkdown] += 0.7
	}

	// Code
	codeTok := float64(len(reCodeTok.FindAllString(text, -1)))
	semi := float64(strings.Count(text, ";"))
	curly := float64(strings.Count(text, "{") + strings.Count(text, "}"))
	if codeTok >= 2 {
		s[modeCode] += 0.9
	}
	if curly >= 6 {
		s[modeCode] += 0.5
	}
	if semi >= 4 {
		s[modeCode] += 0.3
	}

	// Table
	s[modeTable] += scoreTable(lines)

	return s
}

func pickBest(scores map[mode]float64) mode {
	best := modePlain
	bestScore := -1e9
	for m, s := range scores {
		if s > bestScore {
			bestScore = s
			best = m
		}
	}
	return best
}

func looksLikeCompleteJSON(trim string) bool {
	if trim == "" {
		return false
	}
	first := trim[0]
	last := trim[len(trim)-1]
	return (first == '{' && last == '}') || (first == '[' && last == ']')
}

func scoreTable(lines []string) float64 {
	type posKey int
	counts := map[posKey]int{}
	usable := 0
	for _, ln := range lines {
		ln = strings.TrimRight(ln, "\r\n")
		if len(strings.TrimSpace(ln)) == 0 || len(ln) < 20 {
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
	if usable < 4 {
		return 0
	}
	strong := 0
	for _, c := range counts {
		if c >= usable/2 {
			strong++
		}
	}
	if strong >= 2 {
		return 1.0
	}
	if strong == 1 {
		return 0.6
	}
	return 0.0
}

// ─── Table Column Detection & Classification ────────────────────────────────

// detectTableColumns finds column boundaries using a two-strategy approach:
// 1. Header-based: use the first line's word boundaries, validated against data
// 2. Gap-based fallback: aligned double-space gaps (for tables without a header)
func detectTableColumns(lines []string) []tableColumn {
	// Filter out non-table lines (e.g. "total N" from ls -l).
	filtered := make([]string, 0, len(lines))
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if len(trimmed) > 0 && !reNonTableLine.MatchString(trimmed) {
			filtered = append(filtered, ln)
		}
	}
	if len(filtered) < 3 {
		return nil
	}
	var cols []tableColumn
	// Try header-based detection first (handles narrow 1-space gaps like ps -ax).
	if c := detectColumnsFromHeader(filtered); len(c) >= 2 {
		cols = c
	} else {
		// Fall back to gap-based detection.
		cols = detectColumnsFromGaps(filtered)
	}
	if len(cols) < 2 {
		return cols
	}
	// Refine column ends: set col[i].end to just past the widest actual
	// content in that column across all lines. This places the separator
	// right after the left column's content rather than right before the
	// right column's content — important for right-aligned columns like
	// file sizes where the gap is variable.
	refineColumnEnds(cols, filtered)
	return cols
}

// refineColumnEnds adjusts col[i].end for non-last columns by finding where
// the left column's content actually ends. For each line, it finds the first
// space after the column's non-space content (scanning left to right). This
// avoids being fooled by right-aligned values from the NEXT column that bleed
// leftward into the gap (e.g. large file sizes in ls -l).
func refineColumnEnds(cols []tableColumn, lines []string) {
	for ci := 0; ci+1 < len(cols); ci++ {
		maxContentEnd := cols[ci].start
		for _, ln := range lines {
			runes := []rune(strings.TrimRight(ln, "\r\n"))
			limit := cols[ci+1].start
			if limit > len(runes) {
				limit = len(runes)
			}
			// Scan left-to-right: skip leading spaces (for right-aligned
			// columns like PID), then find end of first content cluster.
			j := cols[ci].start
			// Skip leading spaces.
			for j < limit && runes[j] == ' ' {
				j++
			}
			// Find end of content (first space after non-space).
			for j < limit && runes[j] != ' ' {
				j++
			}
			// j is now at the first space after content.
			if j > maxContentEnd {
				maxContentEnd = j
			}
		}
		// Don't let content end exceed the next column's start.
		if maxContentEnd >= cols[ci+1].start {
			maxContentEnd = cols[ci+1].start - 1
		}
		cols[ci].end = maxContentEnd
	}
}

// detectColumnsFromHeader detects columns using the first line as a header.
// It finds word-start positions in the header, then validates each boundary
// by checking that data lines have a space at that position.
// This correctly handles narrow gaps (e.g. "PID TTY" with 1 space).
// Multi-word headers like "CONTAINER ID" are preserved because data lines
// don't have a space at the intra-header gap.
func detectColumnsFromHeader(lines []string) []tableColumn {
	header := strings.TrimRight(lines[0], "\r\n")
	headerRunes := []rune(header)
	if len(headerRunes) < 10 {
		return nil
	}

	// Find word-start positions in the header (space→non-space transitions).
	var wordStarts []int
	for i, r := range headerRunes {
		if r != ' ' && (i == 0 || headerRunes[i-1] == ' ') {
			wordStarts = append(wordStarts, i)
		}
	}
	if len(wordStarts) < 2 {
		return nil
	}

	// Validate each boundary: for the gap just before each word start,
	// check that ≥50% of data lines have a space at that position.
	dataLines := lines[1:]
	validStarts := []int{wordStarts[0]}
	for k := 1; k < len(wordStarts); k++ {
		// The gap position is just before the word start.
		gapPos := wordStarts[k] - 1
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
		if total >= 2 && hasSpace*100/total >= 50 {
			validStarts = append(validStarts, wordStarts[k])
		}
	}
	if len(validStarts) < 2 {
		return nil
	}

	// Find max line width.
	maxWidth := 0
	for _, ln := range lines {
		w := utf8.RuneCountInString(strings.TrimRight(ln, "\r\n"))
		if w > maxWidth {
			maxWidth = w
		}
	}

	// Build columns: each column spans from its start to the next column's start.
	cols := make([]tableColumn, len(validStarts))
	for i, start := range validStarts {
		end := maxWidth
		if i+1 < len(validStarts) {
			end = validStarts[i+1]
		}
		cols[i] = tableColumn{start: start, end: end}
	}
	// Extend first column to start at 0 if there's leading whitespace
	// (common for right-aligned numeric columns like PID).
	if cols[0].start > 0 {
		cols[0].start = 0
	}
	return cols
}

// detectColumnsFromGaps finds columns using aligned double-space gaps.
// Used as a fallback when header-based detection fails (e.g. ls -l).
func detectColumnsFromGaps(lines []string) []tableColumn {
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

	// Collect strong gap bucket positions (appear in ≥50% of usable lines).
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

	// Refine each bucket into a precise gap range [gapStart, gapEnd).
	type gapRange struct{ start, end int }
	gaps := make([]gapRange, 0, len(strongBuckets))
	for _, bucket := range strongBuckets {
		gs, ge := 9999, 0
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
						if runEnd > ge {
							ge = runEnd
						}
					}
				}
			}
		}
		if gs < ge {
			gaps = append(gaps, gapRange{gs, ge})
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
		if g.start <= last.end {
			if g.end > last.end {
				last.end = g.end
			}
		} else {
			merged = append(merged, g)
		}
	}

	// Find max line width.
	maxWidth := 0
	for _, ln := range lines {
		w := utf8.RuneCountInString(strings.TrimRight(ln, "\r\n"))
		if w > maxWidth {
			maxWidth = w
		}
	}

	// Build columns from gap boundaries.
	cols := make([]tableColumn, 0, len(merged)+1)
	colStart := 0
	for _, g := range merged {
		if g.start > colStart {
			cols = append(cols, tableColumn{start: colStart, end: g.start})
		}
		colStart = g.end
	}
	if colStart < maxWidth {
		cols = append(cols, tableColumn{start: colStart, end: maxWidth})
	}
	return cols
}

// classifyColumn infers the type of a column by examining data rows (skipping header).
func classifyColumn(lines []string, col tableColumn) columnType {
	numCount, dateCount, pathCount, total := 0, 0, 0, 0
	for i, ln := range lines {
		if i == 0 {
			continue // skip header
		}
		runes := []rune(strings.TrimRight(ln, "\r\n"))
		s := col.start
		e := col.end
		if s >= len(runes) {
			continue
		}
		if e > len(runes) {
			e = len(runes)
		}
		val := strings.TrimSpace(string(runes[s:e]))
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

// classifyAllColumns sets the ctype field on each column.
func classifyAllColumns(lines []string, cols []tableColumn) {
	for i := range cols {
		cols[i].ctype = classifyColumn(lines, cols[i])
	}
}

// ─── Table Border & Coloring Functions ──────────────────────────────────────

// colorizeTableCellsWithColumns applies per-column coloring.
// Header (lineNum == 1) gets bold cyan; data rows use column type colors.
func colorizeTableCellsWithColumns(line *parser.LogicalLine, lineNum int, columns []tableColumn) {
	cells := line.Cells
	if len(cells) == 0 {
		return
	}

	if lineNum == 1 {
		for i := range cells {
			setFGAttr(&cells[i], colorCyan, parser.AttrBold)
		}
		return
	}

	if len(columns) == 0 {
		return // no column info — leave at default FG
	}

	for ci, col := range columns {
		var color parser.Color
		var attr parser.Attribute
		switch col.ctype {
		case colNumber:
			color = colorYellow
		case colDateTime:
			color = colorCyan
			attr = parser.AttrDim
		case colPath:
			color = colorGreen
		default:
			continue // colText: leave default
		}
		s := col.start
		// Use the full column range up to the next column's start for
		// coloring. col.end is the refined content-end used for separator
		// placement, but right-aligned values (like sizes) may extend
		// beyond col.end into the gap.
		e := len(cells)
		if ci+1 < len(columns) {
			e = columns[ci+1].start
		}
		if s >= len(cells) {
			continue
		}
		if e > len(cells) {
			e = len(cells)
		}
		for i := s; i < e; i++ {
			c := &cells[i]
			if c.Rune == ' ' {
				continue // skip whitespace within column
			}
			if attr != 0 {
				setFGAttr(c, color, attr)
			} else {
				setFG(c, color)
			}
		}
	}
}

// addTableSideBorders pads the line to tableWidth and inserts dim │ column
// separators at col[i-1].end (right after the previous column's content).
func addTableSideBorders(line *parser.LogicalLine, tableWidth int, columns []tableColumn) {
	cells := line.Cells
	spaceCell := parser.Cell{Rune: ' ', FG: parser.DefaultFG, BG: parser.DefaultBG}

	// Pad to tableWidth so all lines are the same width.
	for len(cells) < tableWidth {
		cells = append(cells, spaceCell)
	}

	// Place dim │ at column separator positions, but only if the cell
	// is a space. Right-aligned columns (like file sizes) may have content
	// at the separator position for wider values — never overwrite those.
	for i := 1; i < len(columns); i++ {
		pos := columns[i-1].end
		if pos > 0 && pos < len(cells) && cells[pos].Rune == ' ' {
			cells[pos] = parser.Cell{Rune: '│', FG: parser.DefaultFG, BG: parser.DefaultBG, Attr: parser.AttrDim}
		}
	}
	line.Cells = cells
}

// makeBorderLine creates a horizontal border line of the given width.
// No left/right corners — just fill characters with junctions at column gaps.
func makeBorderLine(width int, columns []tableColumn, pos borderPosition) []parser.Cell {
	var fill, junction rune
	switch pos {
	case borderTop:
		fill, junction = '─', '┬'
	case borderMiddle:
		fill, junction = '─', '┼'
	case borderBottom:
		fill, junction = '─', '┴'
	}

	cells := make([]parser.Cell, width)
	for i := range cells {
		cells[i] = parser.Cell{Rune: fill, FG: parser.DefaultFG, BG: parser.DefaultBG, Attr: parser.AttrDim}
	}

	// Place junctions right after the previous column's content end.
	for i := 1; i < len(columns); i++ {
		jPos := columns[i-1].end
		if jPos > 0 && jPos < width {
			cells[jPos].Rune = junction
		}
	}
	return cells
}

// ─── Cell-based colorizers ──────────────────────────────────────────────────
//
// These operate directly on LogicalLine cells. Only cells with default FG
// are modified — cells already colored by the application are preserved.

// isDefaultFG returns true if the cell's foreground is the terminal default.
func isDefaultFG(c *parser.Cell) bool {
	return c.FG.Mode == parser.ColorModeDefault
}

// setFG sets a cell's foreground color if it currently has the default FG.
func setFG(c *parser.Cell, color parser.Color) {
	if isDefaultFG(c) {
		c.FG = color
	}
}

// setFGAttr sets a cell's foreground color and adds attributes if it currently has the default FG.
func setFGAttr(c *parser.Cell, color parser.Color, attr parser.Attribute) {
	if isDefaultFG(c) {
		c.FG = color
		c.Attr |= attr
	}
}

// insertModeIndicator inserts a new line before beforeIdx with a reverse-video
// mode tag. Falls back to prepending cells if no insert function is available.
func (f *Formatter) insertModeIndicator(beforeIdx int64, label string) {
	tag := " " + label + " "
	cells := make([]parser.Cell, len([]rune(tag)))
	for i, r := range []rune(tag) {
		cells[i] = parser.Cell{
			Rune: r,
			FG:   parser.DefaultFG,
			BG:   parser.DefaultBG,
			Attr: parser.AttrReverse | parser.AttrBold,
		}
	}
	if f.insertFunc != nil {
		f.insertFunc(beforeIdx, cells)
	}
}

// colorizeLogCells applies log/YAML colorization using regex matching on the
// plain text, then maps matched ranges back to cell indices.
func colorizeLogCells(line *parser.LogicalLine) {
	cells := line.Cells
	if len(cells) == 0 {
		return
	}

	// Build plain text and mapping from text index to cell index.
	// We only consider printable runes (skip zero runes).
	plain, textToCell := buildPlainTextMap(cells)
	if len(plain) == 0 {
		return
	}

	text := string(plain)

	// Apply regex-based colorization to ranges
	applyRegexColor(cells, text, textToCell, reISOTime, colorCyan, parser.AttrDim)
	applyRegexColor(cells, text, textToCell, reSyslog, colorCyan, parser.AttrDim)
	applyLevelColors(cells, text, textToCell)
	applyKVColors(cells, text, textToCell)
}

// buildPlainTextMap builds a rune slice and a mapping from rune index to cell index.
func buildPlainTextMap(cells []parser.Cell) ([]rune, []int) {
	plain := make([]rune, 0, len(cells))
	textToCell := make([]int, 0, len(cells))
	for i, c := range cells {
		if c.Rune != 0 {
			plain = append(plain, c.Rune)
			textToCell = append(textToCell, i)
		}
	}
	return plain, textToCell
}

// applyRegexColor applies a color and attribute to all regex matches.
func applyRegexColor(cells []parser.Cell, text string, textToCell []int, re *regexp.Regexp, color parser.Color, attr parser.Attribute) {
	for _, loc := range re.FindAllStringIndex(text, -1) {
		for ti := runeIndex(text, loc[0]); ti < runeIndex(text, loc[1]) && ti < len(textToCell); ti++ {
			ci := textToCell[ti]
			setFGAttr(&cells[ci], color, attr)
		}
	}
}

// applyLevelColors colorizes log level keywords.
func applyLevelColors(cells []parser.Cell, text string, textToCell []int) {
	for _, loc := range reLevel.FindAllStringSubmatchIndex(text, -1) {
		matchText := text[loc[0]:loc[1]]
		var color parser.Color
		var attr parser.Attribute = parser.AttrBold
		switch strings.ToUpper(matchText) {
		case "ERROR", "ERR", "FATAL":
			color = colorRed
		case "WARN", "WARNING":
			color = colorYellow
		case "INFO":
			color = colorGreen
		case "DEBUG", "TRACE":
			color = colorMagenta
		default:
			continue
		}
		for ti := runeIndex(text, loc[0]); ti < runeIndex(text, loc[1]) && ti < len(textToCell); ti++ {
			ci := textToCell[ti]
			setFGAttr(&cells[ci], color, attr)
		}
	}
}

// applyKVColors colorizes key=value pairs.
func applyKVColors(cells []parser.Cell, text string, textToCell []int) {
	for _, loc := range reKV.FindAllStringIndex(text, -1) {
		matchText := text[loc[0]:loc[1]]
		eqPos := strings.Index(matchText, "=")
		if eqPos < 0 {
			continue
		}
		// Color the key part (blue)
		keyEnd := runeIndex(text, loc[0]+eqPos)
		for ti := runeIndex(text, loc[0]); ti < keyEnd && ti < len(textToCell); ti++ {
			ci := textToCell[ti]
			setFG(&cells[ci], colorBlue)
		}
		// Color the value part (yellow)
		valStart := runeIndex(text, loc[0]+eqPos+1)
		for ti := valStart; ti < runeIndex(text, loc[1]) && ti < len(textToCell); ti++ {
			ci := textToCell[ti]
			setFG(&cells[ci], colorYellow)
		}
	}
}

// runeIndex converts a byte offset in a string to a rune index.
func runeIndex(s string, byteOff int) int {
	return len([]rune(s[:byteOff]))
}

