package texelterm

import (
	"strings"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/gdamore/tcell/v2"
)

func TestAltScreenSelection(t *testing.T) {
	// 1. Setup
	v := parser.NewVTerm(80, 24)
	app := &TexelTerm{
		vterm: v,
	}
	// Initialize mouse coordinator
	app.mouseCoordinator = NewMouseCoordinator(v, app)
	app.mouseCoordinator.SetSize(80, 24)

	p := parser.NewParser(v)

	// Helper to write string
	writeString := func(s string) {
		for _, r := range s {
			p.Parse(r)
		}
	}

	// 2. Write to Main Screen
	writeString("Main Screen Content")

	// 3. Switch to Alt Screen (CSI ? 1049 h)
	writeString("\x1b[?1049h")

	// 4. Write to Alt Screen
	writeString("Alt Screen Content")

	// 5. Select "Alt Screen Content" using the selection state machine
	// "Alt Screen Content" is at row 0, columns 0-17 (18 chars)
	// Simulate a click-and-drag selection

	// Start selection at position (0, 0) - in alt screen, use -1 for current line
	app.mouseCoordinator.SelectionStart(0, 0, tcell.Button1, 0)

	// Update selection to position (17, 0) - end of "Alt Screen Content"
	// Using content coordinates: logicalLine=-1 (current), charOffset=17, viewportRow=0
	app.mouseCoordinator.selectionMachine.Update(-1, 17, 0, 0)

	// Finish selection and get the text
	mime, data, ok := app.mouseCoordinator.SelectionFinish(17, 0, tcell.Button1, 0)

	if !ok {
		t.Fatalf("Selection did not return data")
	}
	if mime != "text/plain" {
		t.Errorf("Expected mime type 'text/plain', got %q", mime)
	}

	text := string(data)

	if !strings.Contains(text, "Alt Screen") {
		t.Errorf("Expected selection to contain 'Alt Screen', got %q", text)
	}
	if strings.Contains(text, "Main Screen") {
		t.Errorf("Selection contained content from Main Screen: %q", text)
	}
}

func TestDoubleClickWordSelection(t *testing.T) {
	v := parser.NewVTerm(80, 24)
	app := &TexelTerm{
		vterm: v,
	}
	app.mouseCoordinator = NewMouseCoordinator(v, app)
	app.mouseCoordinator.SetSize(80, 24)

	p := parser.NewParser(v)

	// Write some text
	for _, r := range "hello world test" {
		p.Parse(r)
	}

	// Simulate double-click on "world" (at column 6)
	// First click
	app.mouseCoordinator.clickDetector.DetectClick(0, 6)
	// Second click within timeout (simulates double-click)
	clickType := app.mouseCoordinator.clickDetector.DetectClick(0, 6)

	if clickType != DoubleClick {
		t.Errorf("Expected DoubleClick, got %v", clickType)
	}

	// Start selection with double-click type
	// Using content coordinates: logicalLine=-1 (current), charOffset=6, viewportRow=0
	app.mouseCoordinator.selectionMachine.Start(-1, 6, 0, DoubleClick, 0)

	// Finish selection
	mime, data, ok := app.mouseCoordinator.selectionMachine.Finish(-1, 6, 0, 0)

	if !ok {
		t.Fatalf("Selection did not return data")
	}
	if mime != "text/plain" {
		t.Errorf("Expected mime type 'text/plain', got %q", mime)
	}

	text := string(data)
	if text != "world" {
		t.Errorf("Expected 'world', got %q", text)
	}
}

func TestTripleClickLineSelection(t *testing.T) {
	v := parser.NewVTerm(80, 24)
	app := &TexelTerm{
		vterm: v,
	}
	app.mouseCoordinator = NewMouseCoordinator(v, app)
	app.mouseCoordinator.SetSize(80, 24)

	p := parser.NewParser(v)

	// Write a line of text
	for _, r := range "This is a complete line of text" {
		p.Parse(r)
	}

	// Simulate triple-click
	app.mouseCoordinator.clickDetector.DetectClick(0, 5)
	app.mouseCoordinator.clickDetector.DetectClick(0, 5)
	clickType := app.mouseCoordinator.clickDetector.DetectClick(0, 5)

	if clickType != TripleClick {
		t.Errorf("Expected TripleClick, got %v", clickType)
	}

	// Start selection with triple-click type
	// Using content coordinates: logicalLine=-1 (current), charOffset=5, viewportRow=0
	app.mouseCoordinator.selectionMachine.Start(-1, 5, 0, TripleClick, 0)

	// Finish selection
	mime, data, ok := app.mouseCoordinator.selectionMachine.Finish(-1, 5, 0, 0)

	if !ok {
		t.Fatalf("Selection did not return data")
	}
	if mime != "text/plain" {
		t.Errorf("Expected mime type 'text/plain', got %q", mime)
	}

	text := string(data)
	if text != "This is a complete line of text" {
		t.Errorf("Expected 'This is a complete line of text', got %q", text)
	}
}

func TestClickTypeDetection(t *testing.T) {
	detector := NewClickDetector(DefaultMultiClickTimeout)

	// Test single click
	clickType := detector.DetectClick(0, 0)
	if clickType != SingleClick {
		t.Errorf("First click should be SingleClick, got %v", clickType)
	}

	// Test double click (same position, within timeout)
	clickType = detector.DetectClick(0, 0)
	if clickType != DoubleClick {
		t.Errorf("Second click should be DoubleClick, got %v", clickType)
	}

	// Test triple click (same position, within timeout)
	clickType = detector.DetectClick(0, 0)
	if clickType != TripleClick {
		t.Errorf("Third click should be TripleClick, got %v", clickType)
	}

	// Test that fourth click resets to single
	clickType = detector.DetectClick(0, 0)
	if clickType != SingleClick {
		t.Errorf("Fourth click should reset to SingleClick, got %v", clickType)
	}

	// Test that clicking at different position resets
	detector.DetectClick(0, 0) // Single click
	clickType = detector.DetectClick(5, 5) // Different position
	if clickType != SingleClick {
		t.Errorf("Click at different position should be SingleClick, got %v", clickType)
	}
}

func TestSelectionStateMachine(t *testing.T) {
	v := parser.NewVTerm(80, 24)
	sm := NewSelectionStateMachine(v)
	sm.SetSize(80, 24)

	p := parser.NewParser(v)

	// Write some text
	for _, r := range "hello world" {
		p.Parse(r)
	}

	// Test state transitions
	if sm.IsActive() {
		t.Error("Should not be active initially")
	}

	// Start drag selection
	// Using content coordinates: logicalLine=-1 (current), charOffset=0, viewportRow=0
	sm.Start(-1, 0, 0, SingleClick, 0)
	if !sm.IsActive() {
		t.Error("Should be active after Start")
	}
	if sm.State() != StateDragging {
		t.Errorf("Expected StateDragging, got %v", sm.State())
	}

	// Update selection
	sm.Update(-1, 5, 0, 0)

	// Finish selection
	mime, data, ok := sm.Finish(-1, 5, 0, 0)
	if !ok {
		t.Error("Finish should return ok=true")
	}
	if mime != "text/plain" {
		t.Errorf("Expected text/plain, got %s", mime)
	}
	if string(data) != "hello" {
		t.Errorf("Expected 'hello', got %q", string(data))
	}

	// Should no longer be active (single-click drag)
	if sm.IsActive() {
		t.Error("Should not be active after single-click drag finish")
	}

	// Test cancel
	sm.Start(-1, 0, 0, SingleClick, 0)
	sm.Cancel()
	if sm.IsActive() {
		t.Error("Should not be active after Cancel")
	}
}
