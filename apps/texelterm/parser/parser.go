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
			// LF should only move down one line, preserving column (not CR+LF)
			// In default mode (LNM reset), \n does only line feed
			p.vterm.LineFeed()
		case '\r':
			p.vterm.CarriageReturn()
		case '\b':
			p.vterm.Backspace()
		case '\t':
			p.vterm.Tab()
		default:
			if r >= ' ' {
				p.vterm.placeChar(r)
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
		case 'c':
			p.vterm.Reset()
			p.state = StateGround
		case '(':
			p.state = StateCharset
		case 'M':
			p.vterm.ReverseIndex()
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
	case StateCharset:
		p.state = StateGround
	}
}

func (v *VTerm) handleDCS(payload []rune) {
	payloadStr := string(payload)
	log.Printf("DCS Payload: %s", payloadStr)

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
		return // Not a valid integer command.
	}

	// For setting colors, we require a payload.
	if len(parts) < 2 {
		return
	}

	payload := string(parts[1])

	switch command {
	case 10: // Set/Query Default Foreground Color
		log.Printf("set/query default fg")
		if payload == "?" {
			// --- TRIGGER QUERY CALLBACK ---
			if p.vterm.QueryDefaultFg != nil {
				p.vterm.QueryDefaultFg()
			}
			return
		}
		if color, ok := parseOSCColor(payload); ok {
			log.Printf("Setting default fg")
			p.vterm.defaultFG = color
			if p.vterm.DefaultFgChanged != nil {
				log.Printf(" default fg (Callback)")
				p.vterm.DefaultFgChanged(color)
			}
		}
	case 11: // Set/Query Default Background Color
		log.Printf("set/query default bg")
		if payload == "?" {
			// --- TRIGGER QUERY CALLBACK ---
			if p.vterm.QueryDefaultBg != nil {
				p.vterm.QueryDefaultBg()
			}
			return
		}
		if color, ok := parseOSCColor(payload); ok {
			log.Printf("Setting default bg")
			p.vterm.defaultBG = color
			if p.vterm.DefaultBgChanged != nil {
				log.Printf(" default bg (Callback)")
				p.vterm.DefaultBgChanged(color)
			}
		}
	case 0:
		p.vterm.SetTitle(string(payload))
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
