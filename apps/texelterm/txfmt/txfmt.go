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
	"strings"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/transformer"
)

func init() {
	transformer.Register("txfmt", func(cfg transformer.Config) (transformer.Transformer, error) {
		return New(), nil
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
	modeTable mode = "table"
)

// Color constants as parser.Color values (replacing ANSI escape strings).
var (
	colorRed     = parser.Color{Mode: parser.ColorModeStandard, Value: 1}
	colorGreen   = parser.Color{Mode: parser.ColorModeStandard, Value: 2}
	colorYellow  = parser.Color{Mode: parser.ColorModeStandard, Value: 3}
	colorBlue    = parser.Color{Mode: parser.ColorModeStandard, Value: 4}
	colorMagenta = parser.Color{Mode: parser.ColorModeStandard, Value: 5}
	colorCyan    = parser.Color{Mode: parser.ColorModeStandard, Value: 6}
	colorGray    = parser.Color{Mode: parser.ColorMode256, Value: 8}
)

var (
	reISOTime = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?\b`)
	reSyslog  = regexp.MustCompile(`\b(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}\b`)
	reLevel   = regexp.MustCompile(`\b(INFO|WARN|WARNING|ERR|ERROR|DEBUG|TRACE|FATAL)\b`)
	reKV      = regexp.MustCompile(`\b[a-zA-Z_][a-zA-Z0-9_]*=([^\s]+)\b`)
	reYAMLKey = regexp.MustCompile(`^\s*[\w.-]+\s*:\s+.*$`)
	reXMLish  = regexp.MustCompile(`^\s*<(/?[\w:-]+)(\s+[^>]*)?>`)
	reJSONish = regexp.MustCompile(`^\s*[\{\[]`)
)

// Formatter is the inline output formatter. It detects the format of command
// output and colorizes cells in-place on the committed LogicalLine.
type Formatter struct {
	det                 detector
	wasCommand          bool
	hasShellIntegration bool
	tableLineCount      int
}

// New creates a new Formatter.
func New() *Formatter {
	return &Formatter{
		det: detector{
			maxSampleLines: 20,
			requiredWins:   2,
		},
	}
}

// NotifyPromptStart records that shell integration is active.
// Called from the OnPromptStart hook.
func (f *Formatter) NotifyPromptStart() {
	f.hasShellIntegration = true
}

// HandleLine is the OnLineCommit callback. It detects and colorizes command
// output lines in-place.
func (f *Formatter) HandleLine(_ int64, line *parser.LogicalLine, isCommand bool) {
	// If no shell integration, treat all lines as command output
	effectiveIsCommand := isCommand || !f.hasShellIntegration

	// Reset detector on command→prompt transition
	if f.wasCommand && !effectiveIsCommand {
		f.det.reset()
		f.tableLineCount = 0
	}
	f.wasCommand = effectiveIsCommand

	if !effectiveIsCommand {
		return
	}

	plainText := extractPlainText(line)
	if plainText == "" {
		return
	}

	if !f.det.locked {
		f.det.addSample(plainText)
	}

	m := f.det.current()
	f.colorize(line, m)

	// Prevent reflow for structured formats where wrapping destroys readability
	if m == modeLog || m == modeTable {
		line.FixedWidth = len(line.Cells)
	}
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

// colorize applies format-specific colorization to cells with default FG.
func (f *Formatter) colorize(line *parser.LogicalLine, m mode) {
	switch m {
	case modeLog, modeYAML:
		colorizeLogCells(line)
	case modeJSON:
		colorizeJSONCells(line)
	case modeXML:
		colorizeXMLCells(line)
	case modeTable:
		f.tableLineCount++
		colorizeTableCells(line, f.tableLineCount)
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
		modePlain: 0.2,
		modeJSON:  0,
		modeYAML:  0,
		modeXML:   0,
		modeLog:   0,
		modeTable: 0,
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

// colorizeJSONCells applies JSON syntax coloring to cells.
func colorizeJSONCells(line *parser.LogicalLine) {
	cells := line.Cells
	if len(cells) == 0 {
		return
	}

	inStr := false
	esc := false
	for i := range cells {
		c := &cells[i]
		if !isDefaultFG(c) {
			continue
		}
		ch := c.Rune

		if inStr {
			if esc {
				esc = false
				continue
			}
			if ch == '\\' {
				esc = true
				continue
			}
			if ch == '"' {
				inStr = false
				setFG(c, colorGreen)
				continue
			}
			setFG(c, colorGreen)
			continue
		}

		switch ch {
		case '"':
			inStr = true
			setFG(c, colorGreen)
		case '{', '}', '[', ']':
			setFG(c, colorCyan)
		case ':', ',':
			setFG(c, colorGray)
		default:
			if isJSONKeywordStart(cells, i) {
				kw := readJSONKeywordCells(cells, i)
				for j := 0; j < len(kw); j++ {
					setFG(&cells[i+j], colorMagenta)
				}
				// Skip past the keyword (loop will i++ so we go to last char)
				// Can't modify loop var, so we color in-place above
			} else if isNumberStartRune(ch) {
				n := readNumberCells(cells, i)
				for j := 0; j < n; j++ {
					setFG(&cells[i+j], colorYellow)
				}
			}
		}
	}
}

func isJSONKeywordStart(cells []parser.Cell, i int) bool {
	remaining := len(cells) - i
	if remaining >= 4 {
		s := string([]rune{cells[i].Rune, cells[i+1].Rune, cells[i+2].Rune, cells[i+3].Rune})
		if s == "true" || s == "null" {
			return true
		}
		if remaining >= 5 {
			s2 := s + string(cells[i+4].Rune)
			if s2 == "false" {
				return true
			}
		}
	}
	return false
}

func readJSONKeywordCells(cells []parser.Cell, i int) string {
	remaining := len(cells) - i
	if remaining >= 5 {
		s := string([]rune{cells[i].Rune, cells[i+1].Rune, cells[i+2].Rune, cells[i+3].Rune, cells[i+4].Rune})
		if s == "false" {
			return s
		}
	}
	if remaining >= 4 {
		s := string([]rune{cells[i].Rune, cells[i+1].Rune, cells[i+2].Rune, cells[i+3].Rune})
		if s == "true" || s == "null" {
			return s
		}
	}
	return ""
}

func isNumberStartRune(r rune) bool {
	return (r >= '0' && r <= '9') || r == '-'
}

func readNumberCells(cells []parser.Cell, start int) int {
	j := start
	for j < len(cells) {
		r := cells[j].Rune
		if (r >= '0' && r <= '9') || r == '-' || r == '+' || r == '.' || r == 'e' || r == 'E' {
			j++
			continue
		}
		break
	}
	return j - start
}

// colorizeXMLCells applies XML tag coloring to cells.
func colorizeXMLCells(line *parser.LogicalLine) {
	cells := line.Cells
	if len(cells) == 0 {
		return
	}

	inTag := false
	for i := range cells {
		c := &cells[i]
		if !isDefaultFG(c) {
			continue
		}
		ch := c.Rune

		if !inTag {
			if ch == '<' {
				inTag = true
				setFG(c, colorCyan)
			}
			continue
		}
		// Inside tag
		if ch == '>' {
			inTag = false
			setFG(c, colorCyan)
			continue
		}
		if ch == '=' {
			setFG(c, colorGray)
			continue
		}
		setFG(c, colorCyan)
	}
}

// colorizeTableCells applies table colorization. Header (first row) gets bold
// cyan; subsequent rows get number highlighting.
func colorizeTableCells(line *parser.LogicalLine, lineNum int) {
	cells := line.Cells
	if len(cells) == 0 {
		return
	}

	if lineNum == 1 {
		// Header row: bold cyan
		for i := range cells {
			setFGAttr(&cells[i], colorCyan, parser.AttrBold)
		}
		return
	}

	// Data rows: highlight numbers
	inNum := false
	for i := range cells {
		c := &cells[i]
		r := c.Rune
		if (r >= '0' && r <= '9') || r == '.' {
			if !inNum {
				inNum = true
			}
			setFG(c, colorYellow)
		} else {
			inNum = false
		}
	}
}
