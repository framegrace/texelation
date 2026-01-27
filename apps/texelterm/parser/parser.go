// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/parser.go
// Summary: Implements parser capabilities for the terminal parser module.
// Usage: Consumed by the terminal app when decoding VT sequences.
// Notes: Keeps parsing concerns isolated from rendering.

package parser

import (
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
		case '\x7f': // DEL - treat same as backspace
			p.vterm.Backspace()
		default:
			if r >= ' ' && r != '\x7f' {
				p.vterm.writeCharWithWrapping(r)
			}
			// Ignore BEL and other unprintable control characters
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
		case '6':
			// DECBI - Back Index (horizontal scroll right)
			p.vterm.BackIndex()
			p.state = StateGround
		case '9':
			// DECFI - Forward Index (horizontal scroll left)
			p.vterm.ForwardIndex()
			p.state = StateGround
		case '=', '>':
			p.state = StateGround
		default:
			// Ignore unhandled ESC sequences
			p.state = StateGround
		}
	case StateCSI:
		switch {
		case r >= '0' && r <= '9':
			p.currentParam = p.currentParam*10 + int(r-'0')
		case r == ';', r == ':':
			// Both semicolon and colon are valid parameter separators.
			// Colon is used for SGR subparameters (ITU T.416) like 38:2:r:g:b
			p.params = append(p.params, p.currentParam)
			p.currentParam = 0
		case r == '?':
			// '?' is the DEC private parameter marker (e.g., CSI ? 6 h for DECSET)
			p.private = true
		case r == '>' || r == '<' || r == '=':
			// '>' is used for DA2 and similar queries (CSI > c)
			// '<' is used for Kitty keyboard protocol pop mode (CSI < u)
			// '=' is used for Kitty keyboard protocol query mode (CSI = u)
			// Treat these as intermediate bytes for simplicity
			p.intermediate = r
		case r >= ' ' && r <= '/':
			// Intermediate bytes: space (0x20) through '/' (0x2F)
			// Includes '!', '\'',' etc.
			p.intermediate = r
		case r >= '@' && r <= '~':
			p.params = append(p.params, p.currentParam)
			p.vterm.ProcessCSI(r, p.params, p.intermediate, p.private)
			p.state = StateGround
			p.private = false // Reset for next CSI
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
	// Parse DCS sequences
	// Format: <prefix>;<data>
	// Supported:
	//   - texel-env;<base64-encoded-env>  (shell environment capture)
	//   - tmux;<escaped_command>          (tmux passthrough)

	payloadStr := string(payload)
	if strings.HasPrefix(payloadStr, "texel-env;") {
		// Extract base64-encoded environment
		encodedEnv := strings.TrimPrefix(payloadStr, "texel-env;")
		if v.OnEnvironmentUpdate != nil {
			v.OnEnvironmentUpdate(encodedEnv)
		}
	}
	// Other DCS sequences (like tmux) are ignored for now
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
	case 0, 1, 2:
		p.vterm.SetTitle(payload)
	case 133:
		// OSC 133 - Shell integration (numeric form)
		p.handleOSC133(payload)
	}
}

// handleOSC133 processes OSC 133 shell integration sequences.
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
		// Record prompt START position for seamless recovery
		// Using A (not B) so multiline prompts are fully excluded from history
		p.vterm.MarkPromptStart()
		if p.vterm.OnPromptStart != nil {
			p.vterm.OnPromptStart()
		}

	case "B":
		// Input start (prompt end)
		p.vterm.PromptActive = false
		p.vterm.InputActive = true
		p.vterm.CommandActive = false
		// Record where input starts (convert screen position to history line index)
		p.vterm.InputStartLine = p.vterm.getTopHistoryLine() + p.vterm.CursorY()
		p.vterm.InputStartCol = p.vterm.CursorX()
		// Calculate prompt height (from OSC 133;A to now)
		p.vterm.MarkInputStart()
		if p.vterm.OnInputStart != nil {
			p.vterm.OnInputStart()
		}

	case "C":
		// Command start (input end)
		p.vterm.PromptActive = false
		p.vterm.InputActive = false
		p.vterm.CommandActive = true
		cmd := ""
		if len(parts) > 1 {
			cmd = parts[1]
		}
		if p.vterm.OnCommandStart != nil {
			p.vterm.OnCommandStart(cmd)
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
