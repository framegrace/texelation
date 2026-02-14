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

func TestColorize_JSON(t *testing.T) {
	line := makeCells(`{"key": "val"}`)
	colorizeJSONCells(line)

	// { should be cyan
	if line.Cells[0].FG != colorCyan {
		t.Errorf("expected '{' to be cyan, got %+v", line.Cells[0].FG)
	}
	// " (opening of "key") should be green
	if line.Cells[1].FG != colorGreen {
		t.Errorf("expected '\"' to be green, got %+v", line.Cells[1].FG)
	}
	// k, e, y should be green (inside string)
	for i := 2; i <= 4; i++ {
		if line.Cells[i].FG != colorGreen {
			t.Errorf("expected cell %d (%c) to be green, got %+v", i, line.Cells[i].Rune, line.Cells[i].FG)
		}
	}
	// : should be gray
	if line.Cells[6].FG != colorGray {
		t.Errorf("expected ':' to be gray, got %+v", line.Cells[6].FG)
	}
	// } should be cyan
	last := len(line.Cells) - 1
	if line.Cells[last].FG != colorCyan {
		t.Errorf("expected '}' to be cyan, got %+v", line.Cells[last].FG)
	}
}

func TestColorize_JSON_Numbers(t *testing.T) {
	line := makeCells(`{"n": 42}`)
	colorizeJSONCells(line)

	// 4 and 2 should be yellow
	for i, c := range line.Cells {
		if c.Rune == '4' || c.Rune == '2' {
			if c.FG != colorYellow {
				t.Errorf("expected cell %d (%c) to be yellow, got %+v", i, c.Rune, c.FG)
			}
		}
	}
}

func TestColorize_JSON_Keywords(t *testing.T) {
	line := makeCells(`{"ok": true}`)
	colorizeJSONCells(line)

	// t, r, u, e should be magenta
	for i, c := range line.Cells {
		if c.Rune == 't' || c.Rune == 'r' || c.Rune == 'u' || c.Rune == 'e' {
			// Only the keyword "true" outside strings
			if i >= 7 { // after ": "
				if c.FG != colorMagenta {
					t.Errorf("expected cell %d (%c) to be magenta, got %+v", i, c.Rune, c.FG)
				}
			}
		}
	}
}

func TestColorize_JSON_PreservesExistingColors(t *testing.T) {
	line := makeCells(`{"key": "val"}`)
	// Pre-color some cells
	existingColor := parser.Color{Mode: parser.ColorModeStandard, Value: 1} // red
	line.Cells[2].FG = existingColor                                        // 'k' in "key"

	colorizeJSONCells(line)

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

func TestColorize_XML(t *testing.T) {
	line := makeCells(`<root attr="val">`)
	colorizeXMLCells(line)

	// < should be cyan
	if line.Cells[0].FG != colorCyan {
		t.Errorf("expected '<' to be cyan, got %+v", line.Cells[0].FG)
	}
	// > should be cyan
	last := len(line.Cells) - 1
	if line.Cells[last].FG != colorCyan {
		t.Errorf("expected '>' to be cyan, got %+v", line.Cells[last].FG)
	}
	// = should be gray
	for i, c := range line.Cells {
		if c.Rune == '=' {
			if c.FG != colorGray {
				t.Errorf("expected '=' at %d to be gray, got %+v", i, c.FG)
			}
		}
	}
}

func TestColorize_Table(t *testing.T) {
	header := makeCells(`NAME     STATUS    AGE`)
	colorizeTableCells(header, 1)

	// Header: all cells should be cyan+bold
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

	// Data row: numbers should be yellow
	data := makeCells(`nginx    Running   42`)
	colorizeTableCells(data, 2)

	for i, c := range data.Cells {
		if c.Rune == '4' || c.Rune == '2' {
			if c.FG != colorYellow {
				t.Errorf("data cell %d (%c): expected yellow, got %+v", i, c.Rune, c.FG)
			}
		}
	}
}

func TestHandleLine_CommandTransition(t *testing.T) {
	f := New()
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
	f := New()
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
	f := New()
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
