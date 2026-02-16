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

func makeCellsInteg(s string) *parser.LogicalLine {
	cells := make([]parser.Cell, len([]rune(s)))
	for i, r := range []rune(s) {
		cells[i] = parser.Cell{Rune: r, FG: parser.DefaultFG, BG: parser.DefaultBG}
	}
	return &parser.LogicalLine{Cells: cells}
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

	mdLines := []string{
		"| Name  | Age |",
		"| ----- | --- |",
		"| Alice | 30  |",
	}
	for i, s := range mdLines {
		p.HandleLine(int64(i), makeCellsInteg(s), true)
	}
	// Prompt triggers flush
	p.HandleLine(3, makeCellsInteg("$ "), false)

	if len(insertedLines) == 0 {
		t.Fatal("expected formatted table lines")
	}

	// Check first line is top border
	if insertedLines[0][0].Rune != '╭' {
		t.Errorf("expected ╭ top border, got %c", insertedLines[0][0].Rune)
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

	// JSON line should pass through tablefmt untouched
	line := makeCellsInteg(`{"key": "value"}`)
	suppressed := p.HandleLine(0, line, true)
	if suppressed {
		t.Error("JSON line should not be suppressed by tablefmt")
	}
}
