package parser

import (
	"testing"
)

// TestBasicAttributes tests SGR basic text attributes
func TestBasicAttributes(t *testing.T) {
	tests := []struct {
		name   string
		seq    string
		verify func(*testing.T, *TestHarness)
	}{
		{
			name: "SGR 0 - reset all",
			seq:  "\x1b[1;4;7m\x1b[31m\x1b[44mX\x1b[0mY",
			verify: func(t *testing.T, h *TestHarness) {
				// X should have bold, underline, reverse, red fg, blue bg
				cellX := h.GetCell(0, 0)
				if cellX.Attr&AttrBold == 0 {
					t.Error("X should be bold")
				}
				if cellX.Attr&AttrUnderline == 0 {
					t.Error("X should be underlined")
				}
				if cellX.Attr&AttrReverse == 0 {
					t.Error("X should be reversed")
				}
				// Y should have everything reset
				cellY := h.GetCell(1, 0)
				if cellY.Attr != 0 {
					t.Errorf("Y should have no attributes, got %v", cellY.Attr)
				}
				if cellY.FG.Mode != ColorModeDefault {
					t.Errorf("Y FG should be default, got mode %v", cellY.FG.Mode)
				}
				if cellY.BG.Mode != ColorModeDefault {
					t.Errorf("Y BG should be default, got mode %v", cellY.BG.Mode)
				}
			},
		},
		{
			name: "SGR 1 - bold",
			seq:  "\x1b[1mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.Attr&AttrBold == 0 {
					t.Error("Should be bold")
				}
			},
		},
		{
			name: "SGR 4 - underline",
			seq:  "\x1b[4mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.Attr&AttrUnderline == 0 {
					t.Error("Should be underlined")
				}
			},
		},
		{
			name: "SGR 7 - reverse",
			seq:  "\x1b[7mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.Attr&AttrReverse == 0 {
					t.Error("Should be reversed")
				}
			},
		},
		{
			name: "SGR 22 - not bold",
			seq:  "\x1b[1mX\x1b[22mY",
			verify: func(t *testing.T, h *TestHarness) {
				cellX := h.GetCell(0, 0)
				if cellX.Attr&AttrBold == 0 {
					t.Error("X should be bold")
				}
				cellY := h.GetCell(1, 0)
				if cellY.Attr&AttrBold != 0 {
					t.Error("Y should not be bold")
				}
			},
		},
		{
			name: "SGR 24 - not underlined",
			seq:  "\x1b[4mX\x1b[24mY",
			verify: func(t *testing.T, h *TestHarness) {
				cellX := h.GetCell(0, 0)
				if cellX.Attr&AttrUnderline == 0 {
					t.Error("X should be underlined")
				}
				cellY := h.GetCell(1, 0)
				if cellY.Attr&AttrUnderline != 0 {
					t.Error("Y should not be underlined")
				}
			},
		},
		{
			name: "SGR 27 - not reversed",
			seq:  "\x1b[7mX\x1b[27mY",
			verify: func(t *testing.T, h *TestHarness) {
				cellX := h.GetCell(0, 0)
				if cellX.Attr&AttrReverse == 0 {
					t.Error("X should be reversed")
				}
				cellY := h.GetCell(1, 0)
				if cellY.Attr&AttrReverse != 0 {
					t.Error("Y should not be reversed")
				}
			},
		},
		{
			name: "Combined attributes",
			seq:  "\x1b[1;4;7mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.Attr&AttrBold == 0 {
					t.Error("Should be bold")
				}
				if cell.Attr&AttrUnderline == 0 {
					t.Error("Should be underlined")
				}
				if cell.Attr&AttrReverse == 0 {
					t.Error("Should be reversed")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(80, 24)
			h.SendSeq("\x1b[H") // Home
			h.SendSeq(tt.seq)
			tt.verify(t, h)
		})
	}
}

// TestBasicColors tests SGR 8 basic ANSI colors
func TestBasicColors(t *testing.T) {
	tests := []struct {
		name   string
		seq    string
		verify func(*testing.T, *TestHarness)
	}{
		{
			name: "FG black (30)",
			seq:  "\x1b[30mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 0 {
					t.Errorf("Expected standard black (0), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "FG red (31)",
			seq:  "\x1b[31mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 1 {
					t.Errorf("Expected standard red (1), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "FG green (32)",
			seq:  "\x1b[32mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 2 {
					t.Errorf("Expected standard green (2), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "FG yellow (33)",
			seq:  "\x1b[33mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 3 {
					t.Errorf("Expected standard yellow (3), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "FG blue (34)",
			seq:  "\x1b[34mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 4 {
					t.Errorf("Expected standard blue (4), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "FG magenta (35)",
			seq:  "\x1b[35mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 5 {
					t.Errorf("Expected standard magenta (5), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "FG cyan (36)",
			seq:  "\x1b[36mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 6 {
					t.Errorf("Expected standard cyan (6), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "FG white (37)",
			seq:  "\x1b[37mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 7 {
					t.Errorf("Expected standard white (7), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "FG default (39)",
			seq:  "\x1b[31mX\x1b[39mY",
			verify: func(t *testing.T, h *TestHarness) {
				cellY := h.GetCell(1, 0)
				if cellY.FG.Mode != ColorModeDefault {
					t.Errorf("Expected default FG, got mode=%v", cellY.FG.Mode)
				}
			},
		},
		{
			name: "BG black (40)",
			seq:  "\x1b[40mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 0 {
					t.Errorf("Expected standard black bg (0), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "BG red (41)",
			seq:  "\x1b[41mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 1 {
					t.Errorf("Expected standard red bg (1), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "BG green (42)",
			seq:  "\x1b[42mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 2 {
					t.Errorf("Expected standard green bg (2), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "BG yellow (43)",
			seq:  "\x1b[43mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 3 {
					t.Errorf("Expected standard yellow bg (3), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "BG blue (44)",
			seq:  "\x1b[44mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 4 {
					t.Errorf("Expected standard blue bg (4), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "BG magenta (45)",
			seq:  "\x1b[45mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 5 {
					t.Errorf("Expected standard magenta bg (5), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "BG cyan (46)",
			seq:  "\x1b[46mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 6 {
					t.Errorf("Expected standard cyan bg (6), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "BG white (47)",
			seq:  "\x1b[47mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 7 {
					t.Errorf("Expected standard white bg (7), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "BG default (49)",
			seq:  "\x1b[41mX\x1b[49mY",
			verify: func(t *testing.T, h *TestHarness) {
				cellY := h.GetCell(1, 0)
				if cellY.BG.Mode != ColorModeDefault {
					t.Errorf("Expected default BG, got mode=%v", cellY.BG.Mode)
				}
			},
		},
		{
			name: "FG and BG together",
			seq:  "\x1b[31;44mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 1 {
					t.Errorf("Expected red FG, got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 4 {
					t.Errorf("Expected blue BG, got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(80, 24)
			h.SendSeq("\x1b[H") // Home
			h.SendSeq(tt.seq)
			tt.verify(t, h)
		})
	}
}

// TestBrightColors tests SGR bright/intense colors (90-97, 100-107)
func TestBrightColors(t *testing.T) {
	tests := []struct {
		name   string
		seq    string
		verify func(*testing.T, *TestHarness)
	}{
		{
			name: "Bright FG black (90)",
			seq:  "\x1b[90mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 8 {
					t.Errorf("Expected bright black (8), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "Bright FG red (91)",
			seq:  "\x1b[91mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 9 {
					t.Errorf("Expected bright red (9), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "Bright FG green (92)",
			seq:  "\x1b[92mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 10 {
					t.Errorf("Expected bright green (10), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "Bright FG yellow (93)",
			seq:  "\x1b[93mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 11 {
					t.Errorf("Expected bright yellow (11), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "Bright FG blue (94)",
			seq:  "\x1b[94mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 12 {
					t.Errorf("Expected bright blue (12), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "Bright FG magenta (95)",
			seq:  "\x1b[95mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 13 {
					t.Errorf("Expected bright magenta (13), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "Bright FG cyan (96)",
			seq:  "\x1b[96mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 14 {
					t.Errorf("Expected bright cyan (14), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "Bright FG white (97)",
			seq:  "\x1b[97mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 15 {
					t.Errorf("Expected bright white (15), got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "Bright BG black (100)",
			seq:  "\x1b[100mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 8 {
					t.Errorf("Expected bright black bg (8), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "Bright BG red (101)",
			seq:  "\x1b[101mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 9 {
					t.Errorf("Expected bright red bg (9), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "Bright BG green (102)",
			seq:  "\x1b[102mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 10 {
					t.Errorf("Expected bright green bg (10), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "Bright BG yellow (103)",
			seq:  "\x1b[103mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 11 {
					t.Errorf("Expected bright yellow bg (11), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "Bright BG blue (104)",
			seq:  "\x1b[104mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 12 {
					t.Errorf("Expected bright blue bg (12), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "Bright BG magenta (105)",
			seq:  "\x1b[105mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 13 {
					t.Errorf("Expected bright magenta bg (13), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "Bright BG cyan (106)",
			seq:  "\x1b[106mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 14 {
					t.Errorf("Expected bright cyan bg (14), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "Bright BG white (107)",
			seq:  "\x1b[107mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 15 {
					t.Errorf("Expected bright white bg (15), got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(80, 24)
			h.SendSeq("\x1b[H") // Home
			h.SendSeq(tt.seq)
			tt.verify(t, h)
		})
	}
}

// Test256Colors tests SGR 256-color mode (38;5;n and 48;5;n)
func Test256Colors(t *testing.T) {
	tests := []struct {
		name   string
		seq    string
		verify func(*testing.T, *TestHarness)
	}{
		{
			name: "256 FG color 0",
			seq:  "\x1b[38;5;0mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorMode256 || cell.FG.Value != 0 {
					t.Errorf("Expected 256-color mode 0, got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "256 FG color 15",
			seq:  "\x1b[38;5;15mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorMode256 || cell.FG.Value != 15 {
					t.Errorf("Expected 256-color mode 15, got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "256 FG color 128",
			seq:  "\x1b[38;5;128mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorMode256 || cell.FG.Value != 128 {
					t.Errorf("Expected 256-color mode 128, got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "256 FG color 255",
			seq:  "\x1b[38;5;255mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorMode256 || cell.FG.Value != 255 {
					t.Errorf("Expected 256-color mode 255, got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
			},
		},
		{
			name: "256 BG color 0",
			seq:  "\x1b[48;5;0mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorMode256 || cell.BG.Value != 0 {
					t.Errorf("Expected 256-color bg mode 0, got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "256 BG color 196",
			seq:  "\x1b[48;5;196mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorMode256 || cell.BG.Value != 196 {
					t.Errorf("Expected 256-color bg mode 196, got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
		{
			name: "256 FG and BG together",
			seq:  "\x1b[38;5;100;48;5;200mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorMode256 || cell.FG.Value != 100 {
					t.Errorf("Expected 256-color FG 100, got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
				}
				if cell.BG.Mode != ColorMode256 || cell.BG.Value != 200 {
					t.Errorf("Expected 256-color BG 200, got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(80, 24)
			h.SendSeq("\x1b[H") // Home
			h.SendSeq(tt.seq)
			tt.verify(t, h)
		})
	}
}

// TestRGBColors tests SGR RGB true-color mode (38;2;r;g;b and 48;2;r;g;b)
func TestRGBColors(t *testing.T) {
	tests := []struct {
		name   string
		seq    string
		verify func(*testing.T, *TestHarness)
	}{
		{
			name: "RGB FG black",
			seq:  "\x1b[38;2;0;0;0mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeRGB {
					t.Errorf("Expected RGB mode, got %v", cell.FG.Mode)
				}
				if cell.FG.R != 0 || cell.FG.G != 0 || cell.FG.B != 0 {
					t.Errorf("Expected RGB(0,0,0), got RGB(%d,%d,%d)", cell.FG.R, cell.FG.G, cell.FG.B)
				}
			},
		},
		{
			name: "RGB FG red",
			seq:  "\x1b[38;2;255;0;0mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeRGB {
					t.Errorf("Expected RGB mode, got %v", cell.FG.Mode)
				}
				if cell.FG.R != 255 || cell.FG.G != 0 || cell.FG.B != 0 {
					t.Errorf("Expected RGB(255,0,0), got RGB(%d,%d,%d)", cell.FG.R, cell.FG.G, cell.FG.B)
				}
			},
		},
		{
			name: "RGB FG green",
			seq:  "\x1b[38;2;0;255;0mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeRGB {
					t.Errorf("Expected RGB mode, got %v", cell.FG.Mode)
				}
				if cell.FG.R != 0 || cell.FG.G != 255 || cell.FG.B != 0 {
					t.Errorf("Expected RGB(0,255,0), got RGB(%d,%d,%d)", cell.FG.R, cell.FG.G, cell.FG.B)
				}
			},
		},
		{
			name: "RGB FG blue",
			seq:  "\x1b[38;2;0;0;255mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeRGB {
					t.Errorf("Expected RGB mode, got %v", cell.FG.Mode)
				}
				if cell.FG.R != 0 || cell.FG.G != 0 || cell.FG.B != 255 {
					t.Errorf("Expected RGB(0,0,255), got RGB(%d,%d,%d)", cell.FG.R, cell.FG.G, cell.FG.B)
				}
			},
		},
		{
			name: "RGB FG custom color",
			seq:  "\x1b[38;2;123;45;67mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeRGB {
					t.Errorf("Expected RGB mode, got %v", cell.FG.Mode)
				}
				if cell.FG.R != 123 || cell.FG.G != 45 || cell.FG.B != 67 {
					t.Errorf("Expected RGB(123,45,67), got RGB(%d,%d,%d)", cell.FG.R, cell.FG.G, cell.FG.B)
				}
			},
		},
		{
			name: "RGB BG black",
			seq:  "\x1b[48;2;0;0;0mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeRGB {
					t.Errorf("Expected RGB mode, got %v", cell.BG.Mode)
				}
				if cell.BG.R != 0 || cell.BG.G != 0 || cell.BG.B != 0 {
					t.Errorf("Expected RGB(0,0,0), got RGB(%d,%d,%d)", cell.BG.R, cell.BG.G, cell.BG.B)
				}
			},
		},
		{
			name: "RGB BG white",
			seq:  "\x1b[48;2;255;255;255mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.BG.Mode != ColorModeRGB {
					t.Errorf("Expected RGB mode, got %v", cell.BG.Mode)
				}
				if cell.BG.R != 255 || cell.BG.G != 255 || cell.BG.B != 255 {
					t.Errorf("Expected RGB(255,255,255), got RGB(%d,%d,%d)", cell.BG.R, cell.BG.G, cell.BG.B)
				}
			},
		},
		{
			name: "RGB FG and BG together",
			seq:  "\x1b[38;2;100;150;200;48;2;50;75;100mX",
			verify: func(t *testing.T, h *TestHarness) {
				cell := h.GetCell(0, 0)
				if cell.FG.Mode != ColorModeRGB {
					t.Errorf("Expected RGB FG mode, got %v", cell.FG.Mode)
				}
				if cell.FG.R != 100 || cell.FG.G != 150 || cell.FG.B != 200 {
					t.Errorf("Expected FG RGB(100,150,200), got RGB(%d,%d,%d)", cell.FG.R, cell.FG.G, cell.FG.B)
				}
				if cell.BG.Mode != ColorModeRGB {
					t.Errorf("Expected RGB BG mode, got %v", cell.BG.Mode)
				}
				if cell.BG.R != 50 || cell.BG.G != 75 || cell.BG.B != 100 {
					t.Errorf("Expected BG RGB(50,75,100), got RGB(%d,%d,%d)", cell.BG.R, cell.BG.G, cell.BG.B)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(80, 24)
			h.SendSeq("\x1b[H") // Home
			h.SendSeq(tt.seq)
			tt.verify(t, h)
		})
	}
}

// TestSGRCombinations tests complex SGR combinations
func TestSGRCombinations(t *testing.T) {
	t.Run("Attributes with colors", func(t *testing.T) {
		h := NewTestHarness(80, 24)
		h.SendSeq("\x1b[H")
		h.SendSeq("\x1b[1;4;31;44mX")
		cell := h.GetCell(0, 0)
		if cell.Attr&AttrBold == 0 {
			t.Error("Should be bold")
		}
		if cell.Attr&AttrUnderline == 0 {
			t.Error("Should be underlined")
		}
		if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 1 {
			t.Errorf("Expected red FG, got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
		}
		if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 4 {
			t.Errorf("Expected blue BG, got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
		}
	})

	t.Run("Reset clears attributes and colors", func(t *testing.T) {
		h := NewTestHarness(80, 24)
		h.SendSeq("\x1b[H")
		h.SendSeq("\x1b[1;4;7;31;44mX\x1b[0mY")
		cellY := h.GetCell(1, 0)
		if cellY.Attr != 0 {
			t.Errorf("Y should have no attributes after reset, got %v", cellY.Attr)
		}
		if cellY.FG.Mode != ColorModeDefault {
			t.Errorf("Y FG should be default after reset, got mode=%v", cellY.FG.Mode)
		}
		if cellY.BG.Mode != ColorModeDefault {
			t.Errorf("Y BG should be default after reset, got mode=%v", cellY.BG.Mode)
		}
	})

	t.Run("Mix color modes", func(t *testing.T) {
		h := NewTestHarness(80, 24)
		h.SendSeq("\x1b[H")
		// Basic red FG, 256 blue BG
		h.SendSeq("\x1b[31;48;5;21mX")
		cell := h.GetCell(0, 0)
		if cell.FG.Mode != ColorModeStandard || cell.FG.Value != 1 {
			t.Errorf("Expected standard red FG, got mode=%v value=%v", cell.FG.Mode, cell.FG.Value)
		}
		if cell.BG.Mode != ColorMode256 || cell.BG.Value != 21 {
			t.Errorf("Expected 256-color BG 21, got mode=%v value=%v", cell.BG.Mode, cell.BG.Value)
		}
	})

	t.Run("Override colors", func(t *testing.T) {
		h := NewTestHarness(80, 24)
		h.SendSeq("\x1b[H")
		h.SendSeq("\x1b[31mX\x1b[32mY\x1b[34mZ")
		cellX := h.GetCell(0, 0)
		cellY := h.GetCell(1, 0)
		cellZ := h.GetCell(2, 0)
		if cellX.FG.Value != 1 {
			t.Errorf("X should be red (1), got %v", cellX.FG.Value)
		}
		if cellY.FG.Value != 2 {
			t.Errorf("Y should be green (2), got %v", cellY.FG.Value)
		}
		if cellZ.FG.Value != 4 {
			t.Errorf("Z should be blue (4), got %v", cellZ.FG.Value)
		}
	})
}
