// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Package txfmt is an inline output formatter for texelterm.
// It hooks into the line-commit boundary (memoryBufferLineFeed) to colorize
// command output before it enters scrollback, operating directly on parser.Cell
// slices.
package txfmt

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
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
	modePlain    mode = "plain"
	modeJSON     mode = "json"
	modeYAML     mode = "yaml"
	modeXML      mode = "xml"
	modeLog      mode = "log"
	modeCode     mode = "code"
	modeMarkdown mode = "markdown"
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
)

// Formatter is the inline output formatter. It detects the format of command
// output and colorizes cells in-place on the committed LogicalLine.
type Formatter struct {
	det                 detector
	wasCommand          bool
	hasShellIntegration bool
	currentCommand      string
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

// NotifyCommandStart records the current command for filename-based detection.
func (f *Formatter) NotifyCommandStart(cmd string) {
	f.currentCommand = cmd
}

// prepareOverlay clones Cells into Overlay so colorization modifies the copy.
func prepareOverlay(line *parser.LogicalLine) {
	if line.Overlay != nil {
		return // already prepared
	}
	line.Overlay = make([]parser.Cell, len(line.Cells))
	copy(line.Overlay, line.Cells)
	line.OverlayWidth = len(line.Cells)
}

// HandleLine is the OnLineCommit callback. It detects and colorizes command
// output lines in-place.
func (f *Formatter) HandleLine(lineIdx int64, line *parser.LogicalLine, isCommand bool) {
	// If no shell integration, treat all lines as command output
	effectiveIsCommand := isCommand || !f.hasShellIntegration

	// Reset detector on command→prompt transition
	if f.wasCommand && !effectiveIsCommand {
		f.det.reset()
		f.backlog = f.backlog[:0]
		f.chromaContext = f.chromaContext[:0]
		f.codeLexer = ""
		f.codeLexerMethod = ""
		f.currentCommand = ""
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
		prepareOverlay(line)
		f.colorize(line, m)
		if m == modeLog {
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
	lines := f.backlogLines()

	// Prepare overlays for all lines before colorization
	for _, line := range lines {
		prepareOverlay(line)
	}

	if lexName, ok := chromaLexerName[m]; ok {
		// For modeCode, try filename from command first, then infer from content.
		if m == modeCode {
			if name := f.lexerFromCommand(); name != "" {
				lexName = name
				f.codeLexer = name
				f.codeLexerMethod = "filename"
			} else {
				result := inferLanguage(f.det.sampleLines)
				lexName = result.name
				f.codeLexer = result.name
				f.codeLexerMethod = result.method
			}
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
		if m == modeLog {
			line.FixedWidth = len(line.Cells)
		}
	}
	f.backlog = f.backlog[:0]
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

// fileArgsFromCommand extracts non-flag arguments from a shell command string,
// skipping env vars (FOO=bar), prefix commands (sudo, env, etc.), and flags (-x).
func fileArgsFromCommand(cmd string) []string {
	fields := strings.Fields(cmd)
	var args []string
	pastCommand := false
	for _, f := range fields {
		if !pastCommand {
			if strings.Contains(f, "=") {
				continue
			}
			switch f {
			case "sudo", "env", "nice", "nohup", "time", "command":
				continue
			}
			pastCommand = true
			continue // skip the command itself
		}
		if strings.HasPrefix(f, "-") {
			continue
		}
		args = append(args, f)
	}
	return args
}

// lexerFromCommand tries to match a Chroma lexer from filenames in the command.
func (f *Formatter) lexerFromCommand() string {
	if f.currentCommand == "" {
		return ""
	}
	for _, arg := range fileArgsFromCommand(f.currentCommand) {
		if l := lexers.Match(arg); l != nil {
			return l.Config().Name
		}
	}
	return ""
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
	if line.Overlay == nil {
		prepareOverlay(line)
	}
	cells := line.Overlay
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

