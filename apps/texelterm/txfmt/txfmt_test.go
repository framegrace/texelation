package txfmt

import (
	"strings"
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

func TestDetector_Table(t *testing.T) {
	d := &detector{maxSampleLines: 20, requiredWins: 2}

	d.addSample(`NAME                 STATUS    AGE     VERSION`)
	d.addSample(`nginx-pod            Running   5d      1.21.0`)
	d.addSample(`redis-pod            Running   3d      7.0.5`)
	d.addSample(`postgres-pod         Running   10d     15.2`)
	d.addSample(`memcached-pod        Running   2d      1.6.17`)

	if d.current() != modeTable {
		t.Errorf("expected modeTable, got %s", d.current())
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

func TestColorize_Table_Header(t *testing.T) {
	header := makeCells(`NAME     STATUS    AGE`)
	colorizeTableCellsWithColumns(header, 1, nil)

	// Header: all non-space cells should be cyan+bold
	for i, c := range header.Cells {
		if c.Rune == ' ' {
			continue
		}
		if c.FG != colorCyan {
			t.Errorf("header cell %d (%c): expected cyan, got %+v", i, c.Rune, c.FG)
		}
		if c.Attr&parser.AttrBold == 0 {
			t.Errorf("header cell %d (%c): expected bold", i, c.Rune)
		}
	}
}

func TestDetectTableColumns(t *testing.T) {
	lines := []string{
		"NAME                 STATUS    AGE     VERSION",
		"nginx-pod            Running   5d      1.21.0",
		"redis-pod            Running   3d      7.0.5",
		"postgres-pod         Running   10d     15.2",
		"memcached-pod        Running   2d      1.6.17",
	}
	cols := detectTableColumns(lines)
	if len(cols) < 3 {
		t.Fatalf("expected ≥3 columns, got %d", len(cols))
	}
	// First column should start at 0
	if cols[0].start != 0 {
		t.Errorf("first column start: expected 0, got %d", cols[0].start)
	}
}

func TestClassifyColumn_Number(t *testing.T) {
	lines := []string{
		"NAME         COUNT   STATUS",
		"nginx        42      Running",
		"redis        100     Running",
		"postgres     7       Running",
	}
	// COUNT column is approximately [13, 21)
	col := tableColumn{start: 13, end: 21}
	ct := classifyColumn(lines, col)
	if ct != colNumber {
		t.Errorf("expected colNumber, got %d", ct)
	}
}

func TestClassifyColumn_DateTime(t *testing.T) {
	lines := []string{
		"NAME         AGE    STATUS",
		"nginx        5d     Running",
		"redis        3d     Running",
		"postgres     10d    Running",
	}
	col := tableColumn{start: 13, end: 20}
	ct := classifyColumn(lines, col)
	if ct != colDateTime {
		t.Errorf("expected colDateTime, got %d", ct)
	}
}

func TestClassifyColumn_Path(t *testing.T) {
	lines := []string{
		"NAME         FILE              STATUS",
		"nginx        /etc/nginx.conf   Running",
		"redis        /etc/redis.conf   Running",
		"postgres     /var/lib/pg       Running",
	}
	col := tableColumn{start: 13, end: 31}
	ct := classifyColumn(lines, col)
	if ct != colPath {
		t.Errorf("expected colPath, got %d", ct)
	}
}

func TestClassifyColumn_Text(t *testing.T) {
	lines := []string{
		"NAME         STATUS    AGE",
		"nginx        Running   5d",
		"redis        Running   3d",
		"postgres     Running   10d",
	}
	col := tableColumn{start: 13, end: 23}
	ct := classifyColumn(lines, col)
	if ct != colText {
		t.Errorf("expected colText, got %d", ct)
	}
}

func TestColorizeTableCellsWithColumns_NumberColumn(t *testing.T) {
	data := makeCells(`nginx        42      Running`)
	cols := []tableColumn{
		{start: 0, end: 13, ctype: colText},
		{start: 13, end: 21, ctype: colNumber},
		{start: 21, end: 28, ctype: colText},
	}
	colorizeTableCellsWithColumns(data, 2, cols)

	// "42" at positions 13-14 should be yellow
	for _, i := range []int{13, 14} {
		if data.Cells[i].FG != colorYellow {
			t.Errorf("cell %d (%c): expected yellow, got %+v", i, data.Cells[i].Rune, data.Cells[i].FG)
		}
	}
	// "nginx" at start should remain default (text column)
	if data.Cells[0].FG != parser.DefaultFG {
		t.Errorf("cell 0 (%c): expected default FG, got %+v", data.Cells[0].Rune, data.Cells[0].FG)
	}
	// "Running" should remain default (text column)
	if data.Cells[21].FG != parser.DefaultFG {
		t.Errorf("cell 21 (%c): expected default FG, got %+v", data.Cells[21].Rune, data.Cells[21].FG)
	}
}

func TestColorizeTableCellsWithColumns_NoNumberInText(t *testing.T) {
	// Numbers inside text columns (e.g. "pod-3") should NOT be highlighted
	data := makeCells(`pod-3        Running   text`)
	cols := []tableColumn{
		{start: 0, end: 13, ctype: colText},
		{start: 13, end: 23, ctype: colText},
		{start: 23, end: 27, ctype: colText},
	}
	colorizeTableCellsWithColumns(data, 2, cols)

	// "3" at position 4 should remain default FG (it's in a text column)
	if data.Cells[4].FG != parser.DefaultFG {
		t.Errorf("cell 4 ('3'): expected default FG in text column, got %+v", data.Cells[4].FG)
	}
}

func TestAddTableSideBorders(t *testing.T) {
	cols := []tableColumn{
		{start: 0, end: 5},  // "hello" content ends at 5
		{start: 7, end: 10},
	}
	line := makeCells("hello  world")
	addTableSideBorders(line, 14, cols)

	// Should pad to tableWidth = 14 chars
	if len(line.Cells) != 14 {
		t.Fatalf("expected 14 cells, got %d", len(line.Cells))
	}
	if line.Cells[0].Rune != 'h' {
		t.Errorf("first cell: expected 'h', got %c", line.Cells[0].Rune)
	}
	// Dim │ separator at col[0].end = 5 (right after "hello")
	if line.Cells[5].Rune != '│' {
		t.Errorf("separator at 5: expected '│', got %c", line.Cells[5].Rune)
	}
	if line.Cells[5].Attr&parser.AttrDim == 0 {
		t.Error("separator should have dim attribute")
	}
}

func TestMakeBorderLine(t *testing.T) {
	cols := []tableColumn{
		{start: 0, end: 10},
		{start: 13, end: 20},
	}

	top := makeBorderLine(20, cols, borderTop)
	// No corners — first and last cells are fill
	if top[0].Rune != '─' {
		t.Errorf("top first: expected '─', got %c", top[0].Rune)
	}
	if top[len(top)-1].Rune != '─' {
		t.Errorf("top last: expected '─', got %c", top[len(top)-1].Rune)
	}
	// Junction at col[0].end = 10 (right after first column's content)
	if top[10].Rune != '┬' {
		t.Errorf("top junction at 10: expected '┬', got %c", top[10].Rune)
	}

	mid := makeBorderLine(20, cols, borderMiddle)
	if mid[10].Rune != '┼' {
		t.Errorf("mid junction at 10: expected '┼', got %c", mid[10].Rune)
	}

	bot := makeBorderLine(20, cols, borderBottom)
	if bot[10].Rune != '┴' {
		t.Errorf("bottom junction at 10: expected '┴', got %c", bot[10].Rune)
	}
}

func TestTablePipeline_FullBacklog(t *testing.T) {
	f := New("")
	f.NotifyPromptStart()

	var insertedLines []struct {
		idx   int64
		cells []parser.Cell
	}
	f.SetInsertFunc(func(beforeIdx int64, cells []parser.Cell) {
		insertedLines = append(insertedLines, struct {
			idx   int64
			cells []parser.Cell
		}{beforeIdx, cells})
	})

	tableLines := []string{
		"NAME                 STATUS    AGE     VERSION",
		"nginx-pod            Running   5d      1.21.0",
		"redis-pod            Running   3d      7.0.5",
		"postgres-pod         Running   10d     15.2",
		"memcached-pod        Running   2d      1.6.17",
		"rabbitmq-pod         Running   7d      3.12.0",
	}
	for i, s := range tableLines {
		f.HandleLine(int64(i), makeCells(s), true)
	}

	if !f.det.locked {
		t.Fatal("expected detector to lock on table")
	}
	if f.det.current() != modeTable {
		t.Fatalf("expected modeTable, got %s", f.det.current())
	}

	// Should have inserted: top border + header separator + mode indicator = 3 lines.
	// Bottom border is deferred to command→prompt transition.
	if len(insertedLines) != 3 {
		t.Fatalf("expected 3 inserted lines, got %d", len(insertedLines))
	}

	// Insertion order: borders from recolorizeTableBacklog, then mode indicator.
	// [0] = top border (─...┬...─)
	// [1] = header separator (─...┼...─)
	// [2] = mode indicator (reverse text)
	if insertedLines[0].cells[0].Rune != '─' {
		t.Errorf("top border: expected '─', got %c", insertedLines[0].cells[0].Rune)
	}
	if insertedLines[1].cells[0].Rune != '─' {
		t.Errorf("header sep: expected '─', got %c", insertedLines[1].cells[0].Rune)
	}

	// Column detection should have worked (check before prompt resets state).
	if len(f.tableColumns) < 3 {
		t.Errorf("expected ≥3 table columns, got %d", len(f.tableColumns))
	}

	// Simulate command end — bottom border should be inserted
	promptIdx := int64(len(tableLines) + len(insertedLines))
	f.HandleLine(promptIdx, makeCells("$ "), false)
	if len(insertedLines) != 4 {
		t.Fatalf("expected 4 inserted lines after prompt, got %d", len(insertedLines))
	}
	if insertedLines[3].cells[0].Rune != '─' {
		t.Errorf("bottom border: expected '─', got %c", insertedLines[3].cells[0].Rune)
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

func cellsToString(cells []parser.Cell) string {
	rs := make([]rune, len(cells))
	for i, c := range cells {
		rs[i] = c.Rune
	}
	return string(rs)
}

func TestSideBordersOnContentLines(t *testing.T) {
	f := New("")
	f.NotifyPromptStart()

	var insertCount int
	f.SetInsertFunc(func(beforeIdx int64, cells []parser.Cell) {
		insertCount++
		t.Logf("INSERT #%d @%d: %q", insertCount, beforeIdx, cellsToString(cells))
	})

	tableLines := []string{
		"NAME                 STATUS    AGE     VERSION",
		"nginx-pod            Running   5d      1.21.0",
		"redis-pod            Running   3d      7.0.5",
		"postgres-pod         Running   10d     15.2",
		"memcached-pod        Running   2d      1.6.17",
		"rabbitmq-pod         Running   7d      3.12.0",
	}
	lines := make([]*parser.LogicalLine, len(tableLines))
	for i, s := range tableLines {
		lines[i] = makeCells(s)
		f.HandleLine(int64(i), lines[i], true)
	}

	// Check content lines are padded to same width (no side borders, just uniform width)
	for i, line := range lines {
		text := cellsToString(line.Cells)
		t.Logf("Line %d: fixedWidth=%d len=%d text=%q", i, line.FixedWidth, len(line.Cells), text)
		if line.FixedWidth == 0 {
			t.Errorf("Line %d: expected FixedWidth to be set", i)
		}
	}
	// All lines should have the same width
	if lines[0].FixedWidth != lines[1].FixedWidth {
		t.Errorf("Lines have different FixedWidth: %d vs %d", lines[0].FixedWidth, lines[1].FixedWidth)
	}

	// Now send a streaming line and check it gets padded too
	streaming1 := makeCells("extra-pod            Running   1d      2.0.0")
	f.HandleLine(int64(len(tableLines)), streaming1, true)
	t.Logf("Streaming line: fixedWidth=%d text=%q", streaming1.FixedWidth, cellsToString(streaming1.Cells))
	if streaming1.FixedWidth == 0 {
		t.Error("Streaming line: expected FixedWidth to be set")
	}
}

func TestBottomBorderOnPrompt(t *testing.T) {
	f := New("")
	f.NotifyPromptStart()

	type insertedLine struct {
		idx  int64
		text string
	}
	var inserts []insertedLine
	f.SetInsertFunc(func(beforeIdx int64, cells []parser.Cell) {
		inserts = append(inserts, insertedLine{beforeIdx, cellsToString(cells)})
	})

	tableLines := []string{
		"NAME                 STATUS    AGE     VERSION",
		"nginx-pod            Running   5d      1.21.0",
		"redis-pod            Running   3d      7.0.5",
		"postgres-pod         Running   10d     15.2",
		"memcached-pod        Running   2d      1.6.17",
		"rabbitmq-pod         Running   7d      3.12.0",
	}
	for i, s := range tableLines {
		f.HandleLine(int64(i), makeCells(s), true)
	}
	// 3 inserts from backlog: top border, header sep, mode indicator
	backlogInserts := len(inserts)
	t.Logf("After backlog: %d inserts", backlogInserts)

	// Send 3 more streaming lines
	for i := 0; i < 3; i++ {
		f.HandleLine(int64(len(tableLines)+i+backlogInserts), makeCells("more-pod             Running   1d      1.0.0"), true)
	}
	streamingInserts := len(inserts)
	t.Logf("After streaming: %d inserts (should still be %d)", streamingInserts, backlogInserts)
	if streamingInserts != backlogInserts {
		t.Error("unexpected inserts during streaming")
	}

	// Now send a prompt line (command end) — bottom border should appear
	promptIdx := int64(len(tableLines) + 3 + backlogInserts)
	f.HandleLine(promptIdx, makeCells("$ "), false)
	t.Logf("After prompt: %d inserts", len(inserts))

	// Should have 1 more insert: bottom border
	if len(inserts) != backlogInserts+1 {
		t.Fatalf("expected %d inserts after prompt, got %d", backlogInserts+1, len(inserts))
	}
	last := inserts[len(inserts)-1]
	t.Logf("Bottom border: @%d %q", last.idx, last.text)
	if len([]rune(last.text)) < 2 || []rune(last.text)[0] != '─' {
		t.Errorf("expected bottom border (─...┴...─), got %q", last.text)
	}
}

func TestDockerPsColumns(t *testing.T) {
	// docker ps has multi-word header "CONTAINER ID" — should NOT split
	lines := []string{
		"CONTAINER ID   IMAGE     COMMAND       CREATED       STATUS    PORTS     NAMES",
		"abcdef123456   nginx     \"/docker…\"   5 days ago    Up 5d     80/tcp    web1",
		"789abc012345   redis     \"redis-s…\"   3 days ago    Up 3d     6379/tcp  cache1",
		"def012345678   postgres  \"docker-…\"   10 days ago   Up 10d    5432/tcp  db1",
	}
	cols := detectTableColumns(lines)
	t.Logf("Detected %d columns:", len(cols))
	for i, col := range cols {
		header := lines[0]
		runes := []rune(header)
		end := col.end
		if end > len(runes) {
			end = len(runes)
		}
		t.Logf("  col %d: [%d, %d) header=%q", i, col.start, col.end, string(runes[col.start:end]))
	}

	// CONTAINER ID should be ONE column (not split)
	if len(cols) > 0 {
		headerRunes := []rune(lines[0])
		firstColText := string(headerRunes[cols[0].start:cols[0].end])
		if !strings.Contains(firstColText, "CONTAINER ID") {
			t.Errorf("first column should contain 'CONTAINER ID', got %q", firstColText)
		}
	}
}

func TestPsAxColumns(t *testing.T) {
	// Simulate ps -ax output
	lines := []string{
		"  PID TTY      STAT   TIME COMMAND",
		"    1 ?        Ss     0:01 /sbin/init",
		"   42 ?        S      0:00 /usr/lib/systemd/systemd-journald",
		"  123 tty1     Ss+    0:00 /sbin/agetty",
		" 1234 pts/0    Ss     0:00 bash",
		"12345 pts/1    R+     0:00 ps -ax",
	}
	cols := detectTableColumns(lines)
	t.Logf("Detected %d columns:", len(cols))
	for i, col := range cols {
		t.Logf("  col %d: [%d, %d) type=%d", i, col.start, col.end, col.ctype)
	}

	// Should have at least 4 columns (PID, TTY, STAT, TIME, COMMAND)
	if len(cols) < 4 {
		t.Errorf("expected ≥4 columns for ps -ax, got %d", len(cols))
	}

	// Check that TTY is a separate column
	ttyFound := false
	for _, col := range cols {
		header := lines[0]
		runes := []rune(header)
		if col.start < len(runes) {
			end := col.end
			if end > len(runes) {
				end = len(runes)
			}
			colHeader := string(runes[col.start:end])
			t.Logf("  header text: %q", colHeader)
			if len(colHeader) >= 3 && colHeader[:3] == "TTY" {
				ttyFound = true
			}
		}
	}
	if !ttyFound {
		t.Error("TTY column not detected separately")
	}
}
