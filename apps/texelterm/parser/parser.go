// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/parser.go
// Summary: Implements parser capabilities for the terminal parser module.
// Usage: Consumed by the terminal app when decoding VT sequences.
// Notes: Keeps parsing concerns isolated from rendering.

package parser

import (
	"log"
	"strconv"
	"strings"
)

type State int

const (
	StateGround State = iota
	StateEscape
	StateCSI
	StateOSC
	StateCharset
	StateDCS
	StateDCSEscape
	StateHash
)

type Parser struct {
	state        State
	vterm        *VTerm
	params       []int
	currentParam int
	private      bool
	oscBuffer    []rune
	dcsBuffer    []rune
	intermediate rune
}

func NewParser(v *VTerm) *Parser {
	return &Parser{
		state:     StateGround,
		vterm:     v,
		params:    make([]int, 0, 16),
		oscBuffer: make([]rune, 0, 128),
		dcsBuffer: make([]rune, 0, 128),
	}
}

// Parse processes a slice of bytes from the PTY.
func (p *Parser) Parse(r rune) {
	//	if p.state == StateGround && r == '\x1b' {
	//		p.vterm.DumpGrid("Before ESC sequence")
	//		log.Printf("Parser: Processing sequence starting with ESC")
	//	}
	switch p.state {
	case StateGround:
		switch r {
		case '\x1b':
			p.state = StateEscape
		case '\n':
			// LF (Line Feed) - just move down (like IND/Index)
			// Note: LNM (Line Feed/New Line Mode) would make this behave as CR+LF,
			// but that mode is not currently implemented. Pure LF just moves down.
			p.vterm.LineFeed()
		case '\r':
			p.vterm.CarriageReturn()
		case '\b':
			p.vterm.Backspace()
		case '\t':
			p.vterm.Tab()
		case '\v':
			// VT (Vertical Tab) - behaves like IND (Index)
			p.vterm.Index()
		case '\f':
			// FF (Form Feed) - behaves like IND (Index)
			p.vterm.Index()
		default:
			if r >= ' ' {
				p.vterm.placeChar(r)
			} else if r == '\x07' {
				// BEL - ignore (visual bell not implemented)
			} else {
				log.Printf("Parser: StateGround unprintable 0x%02x", r)
			}
		}
	case StateEscape:
		switch r {
		case '[':
			p.state = StateCSI
			p.params = p.params[:0]
			p.currentParam = 0
			p.private = false
			p.intermediate = 0 // Reset intermediate byte
		case ']':
			p.state = StateOSC
			p.oscBuffer = p.oscBuffer[:0]
		case 'P':
			p.state = StateDCS
			p.dcsBuffer = p.dcsBuffer[:0]
		case '\\':
			// ST (String Terminator) - ESC \
			// This completes string sequences that were terminated with ESC \
			// (The actual string handling already occurred when we saw the ESC)
			p.state = StateGround
		case 'c':
			p.vterm.Reset()
			p.state = StateGround
		case '(':
			p.state = StateCharset
		case '#':
			p.state = StateHash
		case 'D':
			p.vterm.Index()
			p.state = StateGround
		case 'E':
			// NEL - Next Line (move down and to column 1)
			p.vterm.NextLine()
			p.state = StateGround
		case 'M':
			p.vterm.ReverseIndex()
			p.state = StateGround
		case 'H':
			// HTS - Horizontal Tab Set
			p.vterm.SetTabStop()
			p.state = StateGround
		case '7':
			// DECSC - Save Cursor
			p.vterm.SaveCursor()
			p.state = StateGround
		case '8':
			// DECRC - Restore Cursor
			p.vterm.RestoreCursor()
			p.state = StateGround
		case '=', '>':
			p.state = StateGround
		default:
			log.Printf("Parser: Unhandled ESC sequence: %q", r)
			p.state = StateGround
		}
	case StateCSI:
		switch {
		case r >= '0' && r <= '9':
			p.currentParam = p.currentParam*10 + int(r-'0')
		case r == ';':
			p.params = append(p.params, p.currentParam)
			p.currentParam = 0
		case r >= '<' && r <= '?':
			p.private = true
		case r >= ' ' && r <= '/':
			p.intermediate = r
		case r >= '@' && r <= '~':
			p.params = append(p.params, p.currentParam)
			p.vterm.ProcessCSI(r, p.params, p.intermediate)
			p.state = StateGround
		}
	case StateOSC:
		if r == '\x07' || r == '\x1b' { // Terminated by BEL or another ESC
			p.handleOSC(p.oscBuffer)
			p.state = StateGround
			if r == '\x1b' {
				p.Parse(r) // Re-parse the ESC
			}
		} else {
			p.oscBuffer = append(p.oscBuffer, r)

			// CRITICAL FIX: OSC 133 subcommands A/B/C have no parameters
			// Bash/Starship doesn't send terminators for these, so we must auto-terminate
			// to prevent swallowing command output
			// OSC 133;D has parameters (;exitcode), so we let it collect until BEL/ESC
			payload := string(p.oscBuffer)
			if len(payload) >= 5 && payload[:4] == "133;" {
				lastChar := payload[len(payload)-1]
				if lastChar == 'A' || lastChar == 'B' || lastChar == 'C' {
					// Auto-terminate A/B/C immediately (no parameters expected)
					p.handleOSC(p.oscBuffer)
					p.state = StateGround
				}
				// For 'D', continue collecting until we see the actual BEL/ESC terminator
				// to capture the exit code parameter
			}
		}
	case StateDCS:
		if r == '\x1b' {
			p.state = StateDCSEscape
		} else {
			p.dcsBuffer = append(p.dcsBuffer, r)
		}
	case StateDCSEscape:
		if r == '\\' {
			p.vterm.handleDCS(p.dcsBuffer)
			p.state = StateGround
		} else {
			p.state = StateDCS
			p.dcsBuffer = append(p.dcsBuffer, '\x1b', r)
		}
	case StateHash:
		// ESC # sequences
		switch r {
		case '8':
			// DECALN - Screen Alignment Test
			p.vterm.DECALN()
		}
		p.state = StateGround
	case StateCharset:
		p.state = StateGround
	}
}

func (v *VTerm) handleDCS(payload []rune) {
	// A tmux payload is typically in the format "tmux;<escaped_command>"
	// For now, we will just log it. In a full implementation, you would
	// parse this further. For example, if you received a query, you
	// would construct a response here and send it back via v.WriteToPty.
	//
	// The simple act of acknowledging these DCS sequences, even without
	// a full response, is often enough to make applications like nvim
	// use their more advanced rendering paths.
}

func (p *Parser) handleOSC(sequence []rune) {
	// Use your existing helper to split the sequence at the first semicolon.
	parts := splitRunesN(sequence, ';', 2)

	// We must have at least a command part.
	if len(parts) == 0 {
		return
	}

	commandPart := parts[0]
	command, err := strconv.Atoi(string(commandPart))
	if err != nil {
		// Check for non-numeric commands (e.g., OSC 133)
		commandStr := string(commandPart)
		if commandStr == "133" {
			// OSC 133 - Shell integration
			if len(parts) >= 2 {
				p.handleOSC133(string(parts[1]))
			}
		}
		return
	}

	// For setting colors, we require a payload.
	if len(parts) < 2 {
		return
	}

	payload := string(parts[1])

	switch command {
	case 10: // Set/Query Default Foreground Color
		if payload == "?" {
			// --- TRIGGER QUERY CALLBACK ---
			if p.vterm.QueryDefaultFg != nil {
				p.vterm.QueryDefaultFg()
			}
			return
		}
		if color, ok := parseOSCColor(payload); ok {
			p.vterm.defaultFG = color
			if p.vterm.DefaultFgChanged != nil {
				p.vterm.DefaultFgChanged(color)
			}
		}
	case 11: // Set/Query Default Background Color
		if payload == "?" {
			// --- TRIGGER QUERY CALLBACK ---
			if p.vterm.QueryDefaultBg != nil {
				p.vterm.QueryDefaultBg()
			}
			return
		}
		if color, ok := parseOSCColor(payload); ok {
			p.vterm.defaultBG = color
			if p.vterm.DefaultBgChanged != nil {
				p.vterm.DefaultBgChanged(color)
			}
		}
	case 0:
		p.vterm.SetTitle(string(payload))
	case 133:
		// OSC 133 - Shell integration (numeric form)
		p.handleOSC133(payload)
	}
}

// handleOSC133 processes OSC 133 shell integration sequences
// Format: OSC 133 ; <subcommand> [; <params>] ST
// A = Prompt start
// B = Prompt end / Input start
// C = Input end / Command start
// D = Command end [; exitcode]
func (p *Parser) handleOSC133(payload string) {
	parts := strings.Split(payload, ";")
	if len(parts) == 0 {
		return
	}

	subcommand := strings.TrimSpace(parts[0])

	switch subcommand {
	case "A":
		// Prompt start
		p.vterm.PromptActive = true
		p.vterm.InputActive = false
		p.vterm.CommandActive = false
		if p.vterm.OnPromptStart != nil {
			p.vterm.OnPromptStart()
		}

	case "B":
		// Input start (prompt end)
		p.vterm.PromptActive = false
		p.vterm.InputActive = true
		p.vterm.CommandActive = false
		// Record where input starts (convert screen position to history line index)
		p.vterm.InputStartLine = p.vterm.getTopHistoryLine() + p.vterm.GetCursorY()
		p.vterm.InputStartCol = p.vterm.GetCursorX()
		if p.vterm.OnInputStart != nil {
			p.vterm.OnInputStart()
		}

	case "C":
		// Command start (input end)
		p.vterm.PromptActive = false
		p.vterm.InputActive = false
		p.vterm.CommandActive = true
		if p.vterm.OnCommandStart != nil {
			p.vterm.OnCommandStart()
		}

	case "D":
		// Command end
		exitCode := 0
		if len(parts) >= 2 {
			if code, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
				exitCode = code
			}
		}
		p.vterm.PromptActive = false
		p.vterm.InputActive = false
		p.vterm.CommandActive = false
		if p.vterm.OnCommandEnd != nil {
			p.vterm.OnCommandEnd(exitCode)
		}
	}
}

func parseOSCColor(payload string) (Color, bool) {
	if strings.HasPrefix(payload, "rgb:") {
		parts := strings.Split(strings.TrimPrefix(payload, "rgb:"), "/")
		if len(parts) == 3 {
			r, errR := strconv.ParseInt(parts[0], 16, 32)
			g, errG := strconv.ParseInt(parts[1], 16, 32)
			b, errB := strconv.ParseInt(parts[2], 16, 32)
			if errR == nil && errG == nil && errB == nil {
				// OSC colors are often 16-bit (4 hex digits), so we scale down to 8-bit.
				return Color{Mode: ColorModeRGB, R: uint8(r / 257), G: uint8(g / 257), B: uint8(b / 257)}, true
			}
		}
	}
	// Can add support for named colors like "red" here if needed
	return Color{}, false
}

func splitRunesN(r []rune, sep rune, n int) [][]rune {
	if n <= 1 {
		return [][]rune{r}
	}
	res := make([][]rune, 0, n)
	start := 0
	count := 1
	for i, ru := range r {
		if ru == sep && count < n {
			res = append(res, r[start:i])
			start = i + 1
			count++
		}
	}
	// whatever remains is the last part
	res = append(res, r[start:])
	return res
}
