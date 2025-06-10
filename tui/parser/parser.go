package parser

import (
	"bytes"
	"unicode/utf8" // Import the utf8 package
)

type State int

const (
	StateGround State = iota
	StateEscape
	StateCSI
	StateOSC
	StateCharset
)

type Parser struct {
	state        State
	vterm        *VTerm
	params       []int
	currentParam int
	private      bool
	oscBuffer    []byte
}

func NewParser(v *VTerm) *Parser {
	return &Parser{
		state:     StateGround,
		vterm:     v,
		params:    make([]int, 0, 16),
		oscBuffer: make([]byte, 0, 128),
	}
}

// Parse processes a slice of bytes from the PTY.
func (p *Parser) Parse(data []byte) {
	// Refactored loop to handle multi-byte UTF-8 characters correctly.
	for i := 0; i < len(data); {
		b := data[i]
		var size int = 1 // Default to consuming 1 byte

		switch p.state {
		case StateGround:
			switch {
			case b == '\x1b':
				p.state = StateEscape
			case b == '\n':
				p.vterm.LineFeed()
			case b == '\r':
				p.vterm.CarriageReturn()
			case b == '\b':
				p.vterm.Backspace()
			case b == '\t':
				p.vterm.Tab()
			case b < ' ':
				// Ignore other control characters for now
			default:
				// Decode a full rune and its size in bytes
				var r rune
				r, size = utf8.DecodeRune(data[i:])
				p.vterm.placeChar(r)
			}
		case StateEscape:
			switch b {
			case '[':
				p.state = StateCSI
				p.params = p.params[:0]
				p.currentParam = 0
				p.private = false
			case ']':
				p.state = StateOSC
				p.oscBuffer = p.oscBuffer[:0]
			case '(':
				p.state = StateCharset
			case '=', '>':
				p.state = StateGround
			default:
				p.state = StateGround
			}
		case StateCSI:
			if b >= '0' && b <= '9' {
				p.currentParam = p.currentParam*10 + int(b-'0')
			} else if b == ';' {
				p.params = append(p.params, p.currentParam)
				p.currentParam = 0
			} else if b == '?' {
				p.private = true
			} else if b >= '@' && b <= '~' {
				p.params = append(p.params, p.currentParam)
				p.vterm.ProcessCSI(b, p.params, p.private)
				p.state = StateGround
			}
		case StateOSC:
			if b == '\x07' {
				p.handleOSC()
				p.state = StateGround
			} else {
				p.oscBuffer = append(p.oscBuffer, b)
			}
		case StateCharset:
			p.state = StateGround
		}
		// Advance the loop by the number of bytes consumed
		i += size
	}
}

func (p *Parser) handleOSC() {
	parts := bytes.SplitN(p.oscBuffer, []byte{';'}, 2)
	if len(parts) != 2 {
		return
	}
	command := string(parts[0])
	content := string(parts[1])
	if command == "0" {
		p.vterm.SetTitle(content)
	}
}
