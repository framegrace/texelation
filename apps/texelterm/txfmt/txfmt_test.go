package txfmt

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// makeCells creates a LogicalLine from a string with default FG/BG.
func makeCells(s string) *parser.LogicalLine {
	cells := make([]parser.Cell, len([]rune(s)))
	for i, r := range []rune(s) {
		cells[i] = parser.Cell{Rune: r, FG: parser.DefaultFG, BG: parser.DefaultBG}
	}
	return &parser.LogicalLine{Cells: cells}
}

func TestDetector_JSON(t *testing.T) {
	d := &detector{maxSampleLines: 20, requiredWins: 2}

	d.addSample(`{"name": "test", "value": 42}`)
	d.addSample(`{"name": "test2", "value": 43}`)
	d.addSample(`{"name": "test3", "value": 44}`)

	if d.current() != modeJSON {
		t.Errorf("expected modeJSON, got %s", d.current())
	}
	if !d.locked {
		t.Error("expected detector to be locked")
	}
}

func TestDetector_Log(t *testing.T) {
	d := &detector{maxSampleLines: 20, requiredWins: 2}

	d.addSample(`2024-01-15T10:30:00Z INFO Starting server port=8080`)
	d.addSample(`2024-01-15T10:30:01Z WARN Connection slow timeout=30s`)
	d.addSample(`2024-01-15T10:30:02Z ERROR Failed to connect host=db.local`)
	d.addSample(`2024-01-15T10:30:03Z INFO Retrying attempt=2`)

	if d.current() != modeLog {
		t.Errorf("expected modeLog, got %s", d.current())
	}
}

func TestDetector_XML(t *testing.T) {
	d := &detector{maxSampleLines: 20, requiredWins: 2}

	d.addSample(`<?xml version="1.0"?>`)
	d.addSample(`<root>`)
	d.addSample(`  <item name="test"/>`)
	d.addSample(`</root>`)

	if d.current() != modeXML {
		t.Errorf("expected modeXML, got %s", d.current())
	}
}

func TestDetector_Plain(t *testing.T) {
	d := &detector{maxSampleLines: 5, requiredWins: 2}

	d.addSample("hello world")
	d.addSample("this is just text")
	d.addSample("nothing special here")
	d.addSample("more text")
	d.addSample("final line")

	if d.current() != modePlain {
		t.Errorf("expected modePlain, got %s", d.current())
	}
}

func TestDetector_Reset(t *testing.T) {
	d := &detector{maxSampleLines: 20, requiredWins: 2}

	d.addSample(`{"name": "test"}`)
	d.addSample(`{"name": "test2"}`)
	d.addSample(`{"name": "test3"}`)

	if d.current() != modeJSON {
		t.Fatalf("expected modeJSON before reset, got %s", d.current())
	}

	d.reset()

	if d.current() != modePlain {
		t.Errorf("expected modePlain after reset, got %s", d.current())
	}
	if d.locked {
		t.Error("expected detector to be unlocked after reset")
	}
}

func TestColorize_JSON_Chroma(t *testing.T) {
	line := makeCells(`{"key": "val"}`)
	style := chromaStyle("")
	chromaColorizeLines([]*parser.LogicalLine{line}, "json", style)

	// Some cells should be colorized with distinct colors (strings, keys)
	colored := 0
	for _, c := range line.Cells {
		if c.FG.Mode == parser.ColorModeRGB {
			colored++
		}
	}
	if colored == 0 {
		t.Error("expected Chroma to colorize some cells with RGB")
	}

	// Punctuation like { and } should remain default FG (base text color skipped)
	if line.Cells[0].FG.Mode != parser.ColorModeDefault {
		t.Errorf("expected '{' to remain default FG (base color skip), got mode %d", line.Cells[0].FG.Mode)
	}

	// String content "key" should be colored (not base text color)
	// Cell 1 is opening quote of "key"
	if line.Cells[1].FG.Mode != parser.ColorModeRGB {
		t.Errorf("expected string quote to be RGB, got mode %d", line.Cells[1].FG.Mode)
	}
}

func TestColorize_JSON_PreservesExistingColors(t *testing.T) {
	line := makeCells(`{"key": "val"}`)
	// Pre-color some cells
	existingColor := parser.Color{Mode: parser.ColorModeStandard, Value: 1} // red
	line.Cells[2].FG = existingColor                                        // 'k' in "key"

	style := chromaStyle("")
	chromaColorizeLines([]*parser.LogicalLine{line}, "json", style)

	// Pre-colored cell should remain red
	if line.Cells[2].FG != existingColor {
		t.Errorf("expected pre-colored cell to be preserved, got %+v", line.Cells[2].FG)
	}
}

func TestColorize_Log(t *testing.T) {
	line := makeCells(`2024-01-15T10:30:00Z ERROR Failed host=db.local`)
	colorizeLogCells(line)

	// Timestamp should be cyan+dim
	if line.Cells[0].FG != colorCyan {
		t.Errorf("expected timestamp cell to be cyan, got %+v", line.Cells[0].FG)
	}
	if line.Cells[0].Attr&parser.AttrDim == 0 {
		t.Error("expected timestamp cell to have dim attribute")
	}

	// Find ERROR in the line and check it's bold red
	for i, c := range line.Cells {
		if c.Rune == 'E' && i+4 < len(line.Cells) {
			word := string([]rune{line.Cells[i].Rune, line.Cells[i+1].Rune, line.Cells[i+2].Rune, line.Cells[i+3].Rune, line.Cells[i+4].Rune})
			if word == "ERROR" {
				if c.FG != colorRed {
					t.Errorf("expected ERROR to be red, got %+v", c.FG)
				}
				if c.Attr&parser.AttrBold == 0 {
					t.Error("expected ERROR to have bold attribute")
				}
				break
			}
		}
	}

	// Find host= and check key is blue, value is yellow
	for i, c := range line.Cells {
		if c.Rune == 'h' && i+3 < len(line.Cells) {
			word := string([]rune{line.Cells[i].Rune, line.Cells[i+1].Rune, line.Cells[i+2].Rune, line.Cells[i+3].Rune})
			if word == "host" {
				if c.FG != colorBlue {
					t.Errorf("expected 'host' key to be blue, got %+v", c.FG)
				}
				break
			}
		}
	}
}

func TestColorize_XML_Chroma(t *testing.T) {
	line := makeCells(`<root attr="val">`)
	style := chromaStyle("")
	chromaColorizeLines([]*parser.LogicalLine{line}, "xml", style)

	// Chroma should colorize XML elements
	colored := 0
	for _, c := range line.Cells {
		if c.Rune != ' ' && c.FG.Mode != parser.ColorModeDefault {
			colored++
		}
	}
	if colored == 0 {
		t.Error("expected Chroma to colorize XML cells")
	}
}









func TestHandleLine_CommandTransition(t *testing.T) {
	f := New("")
	f.NotifyPromptStart() // simulate shell integration

	// Feed some JSON as command output
	line1 := makeCells(`{"key": "val"}`)
	f.HandleLine(0, line1, true)
	line2 := makeCells(`{"key": "val2"}`)
	f.HandleLine(1, line2, true)
	line3 := makeCells(`{"key": "val3"}`)
	f.HandleLine(2, line3, true)

	// Detector should have picked up JSON
	if f.det.current() != modeJSON {
		t.Errorf("expected modeJSON, got %s", f.det.current())
	}

	// Simulate prompt (command end)
	promptLine := makeCells(`$ `)
	f.HandleLine(3, promptLine, false)

	// Detector should have reset
	if f.det.locked {
		t.Error("expected detector to be unlocked after prompt")
	}
}

func TestHandleLine_NoShellIntegration(t *testing.T) {
	f := New("")
	// Don't call NotifyPromptStart — no shell integration

	// All lines should be treated as command output even with isCommand=false
	line := makeCells(`{"key": "val"}`)
	f.HandleLine(0, line, false)

	// Should still have been processed (cells should be colored)
	if line.Cells[0].FG == parser.DefaultFG {
		// With only one sample, might not have locked yet, but should have
		// at least been processed. Check that the detector got a sample.
		if len(f.det.sampleLines) == 0 {
			t.Error("expected detector to have samples when no shell integration")
		}
	}
}

func TestHandleLine_WithShellIntegration(t *testing.T) {
	f := New("")
	f.NotifyPromptStart() // Shell integration is active

	// Lines with isCommand=false should be skipped
	line := makeCells(`{"key": "val"}`)
	f.HandleLine(0, line, false)

	// Should NOT have been processed
	if len(f.det.sampleLines) != 0 {
		t.Error("expected no samples for non-command lines with shell integration")
	}
}

func TestExtractPlainText(t *testing.T) {
	line := makeCells("hello world")
	text := extractPlainText(line)
	if text != "hello world" {
		t.Errorf("expected 'hello world', got %q", text)
	}
}

func TestExtractPlainText_WithNulls(t *testing.T) {
	line := &parser.LogicalLine{
		Cells: []parser.Cell{
			{Rune: 'h', FG: parser.DefaultFG},
			{Rune: 0, FG: parser.DefaultFG}, // null rune
			{Rune: 'i', FG: parser.DefaultFG},
		},
	}
	text := extractPlainText(line)
	if text != "hi" {
		t.Errorf("expected 'hi', got %q", text)
	}
}

func TestModeIndicator(t *testing.T) {
	f := New("")
	f.NotifyPromptStart()

	// Capture inserted indicator line
	var insertedCells []parser.Cell
	var insertedIdx int64
	f.SetInsertFunc(func(beforeIdx int64, cells []parser.Cell) {
		insertedIdx = beforeIdx
		insertedCells = cells
	})

	// Feed enough JSON lines to trigger detection lock
	lines := []*parser.LogicalLine{
		makeCells(`{"key": "val"}`),
		makeCells(`{"key": "val2"}`),
		makeCells(`{"key": "val3"}`),
	}
	for i, line := range lines {
		f.HandleLine(int64(i), line, true)
	}

	if !f.det.locked {
		t.Fatal("expected detector to lock on JSON")
	}

	// Indicator should have been inserted as a new line before the first backlog line
	if insertedCells == nil {
		t.Fatal("expected indicator line to be inserted")
	}
	if insertedIdx != 0 {
		t.Errorf("expected insert before line 0, got %d", insertedIdx)
	}

	tag := " auto-color as: json "
	tagRunes := []rune(tag)
	if len(insertedCells) != len(tagRunes) {
		t.Fatalf("indicator length: expected %d, got %d", len(tagRunes), len(insertedCells))
	}
	for i, r := range tagRunes {
		c := insertedCells[i]
		if c.Rune != r {
			t.Errorf("indicator cell %d: expected %q, got %q", i, r, c.Rune)
		}
		if c.Attr&parser.AttrReverse == 0 {
			t.Errorf("indicator cell %d: expected reverse attribute", i)
		}
	}
}

func TestInferLanguage_Go(t *testing.T) {
	lines := []string{
		"package main",
		`import "fmt"`,
		"func main() {",
		`    fmt.Println("hello")`,
		"}",
	}
	r := inferLanguage(lines)
	if r.name != "go" {
		t.Errorf("expected 'go', got %q", r.name)
	}
	if r.method != "heuristic" {
		t.Errorf("expected method 'heuristic', got %q", r.method)
	}
}

func TestInferLanguage_Python(t *testing.T) {
	// go-enry's Bayesian classifier detects Python from content
	// (unlike Chroma's lexers.Analyse which requires filename matching).
	lines := []string{
		"import os",
		"class MyApp:",
		"    def run(self):",
		"        pass",
	}
	r := inferLanguage(lines)
	if r.name != "python" {
		t.Errorf("expected 'python', got %q", r.name)
	}
	if r.method != "classifier" {
		t.Errorf("expected method 'classifier', got %q", r.method)
	}
}

func TestInferLanguage_Shebang(t *testing.T) {
	lines := []string{
		"#!/usr/bin/env python3",
		"import os",
		"print('hello')",
	}
	r := inferLanguage(lines)
	if r.name != "python" {
		t.Errorf("expected 'python', got %q", r.name)
	}
	if r.method != "shebang" {
		t.Errorf("expected method 'shebang', got %q", r.method)
	}
}

func TestInferLanguage_Rust(t *testing.T) {
	lines := []string{
		"use std::io;",
		"fn main() {",
		`    let mut input = String::new();`,
		`    println!("{}", input);`,
		"}",
	}
	r := inferLanguage(lines)
	if r.name != "rust" {
		t.Errorf("expected 'rust', got %q", r.name)
	}
	if r.method != "classifier" {
		t.Errorf("expected method 'classifier', got %q", r.method)
	}
}

func TestModeIndicator_ShowsLanguage(t *testing.T) {
	f := New("")
	f.NotifyPromptStart()

	var insertedCells []parser.Cell
	f.SetInsertFunc(func(_ int64, cells []parser.Cell) {
		insertedCells = cells
	})

	goCode := []string{
		"package main",
		`import "fmt"`,
		"func main() {",
		`    fmt.Println("hello")`,
	}
	for i, code := range goCode {
		f.HandleLine(int64(i), makeCells(code), true)
	}

	if !f.det.locked {
		t.Fatal("expected detector to lock")
	}

	// Indicator should show "auto-color as: go (heuristic)"
	if insertedCells == nil {
		t.Fatal("expected indicator line to be inserted")
	}
	tag := " auto-color as: go (heuristic) "
	tagRunes := []rune(tag)
	if len(insertedCells) != len(tagRunes) {
		got := make([]rune, len(insertedCells))
		for i, c := range insertedCells {
			got[i] = c.Rune
		}
		t.Fatalf("indicator: expected %q, got %q", tag, string(got))
	}
	for i, r := range tagRunes {
		if insertedCells[i].Rune != r {
			t.Errorf("indicator cell %d: expected %q, got %q", i, r, insertedCells[i].Rune)
		}
	}
}

func TestColorize_Go_MultiLineContext(t *testing.T) {
	// Multi-line tokenization should produce significantly more colored tokens
	// than single-line tokenization for Go code.
	lines := []*parser.LogicalLine{
		makeCells(`package main`),
		makeCells(`import "fmt"`),
		makeCells(`func main() {`),
		makeCells(`    fmt.Println("hello")`),
		makeCells(`}`),
	}
	style := chromaStyle("")
	chromaColorizeLines(lines, "go", style)

	colored := 0
	for _, line := range lines {
		for _, c := range line.Cells {
			if c.FG.Mode == parser.ColorModeRGB {
				colored++
			}
		}
	}
	// With full context, Go lexer should color keywords, strings, package names, etc.
	// Without context (per-line), only 1-2 tokens per line get colored.
	if colored < 10 {
		t.Errorf("expected multi-line Go to produce ≥10 colored cells, got %d", colored)
	}
}

func TestColorize_WithContext_Streaming(t *testing.T) {
	// Verify that chromaColorizeWithContext uses previous lines for better results.
	style := chromaStyle("")
	context := []string{
		"package main",
		`import "fmt"`,
	}
	line := makeCells(`func main() {`)
	chromaColorizeWithContext(line, context, "go", style)

	// "func" should be colored as a keyword with full context
	colored := 0
	for _, c := range line.Cells {
		if c.FG.Mode == parser.ColorModeRGB {
			colored++
		}
	}
	if colored == 0 {
		t.Error("expected context-aware tokenization to color Go keywords")
	}
}

func TestColorize_Markdown_MultiLine(t *testing.T) {
	lines := []*parser.LogicalLine{
		makeCells(`# Heading`),
		makeCells(`Some text with **bold** words`),
		makeCells(`- list item`),
	}
	style := chromaStyle("")
	chromaColorizeLines(lines, "markdown", style)

	// Check that heading gets colored or has attributes
	headingColored := false
	for _, c := range lines[0].Cells {
		if c.FG.Mode == parser.ColorModeRGB || c.Attr != 0 {
			headingColored = true
			break
		}
	}
	if !headingColored {
		t.Error("expected markdown heading to be colorized with multi-line context")
	}
}

func TestRuneIndex(t *testing.T) {
	tests := []struct {
		s       string
		byteOff int
		want    int
	}{
		{"hello", 0, 0},
		{"hello", 3, 3},
		{"hello", 5, 5},
	}
	for _, tt := range tests {
		got := runeIndex(tt.s, tt.byteOff)
		if got != tt.want {
			t.Errorf("runeIndex(%q, %d) = %d, want %d", tt.s, tt.byteOff, got, tt.want)
		}
	}
}

