package parser

import (
	"bytes"
	"log"
	"unicode/utf8" // Import the utf8 package
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
	oscBuffer    []byte
	utf8Stash    []byte
	intermediate byte
}

func NewParser(v *VTerm) *Parser {
	return &Parser{
		state:     StateGround,
		vterm:     v,
		params:    make([]int, 0, 16),
		oscBuffer: make([]byte, 0, 128),
		utf8Stash: make([]byte, 0, 4),
	}
}

// Parse processes a slice of bytes from the PTY.
func (p *Parser) Parse(data []byte) {
	if len(p.utf8Stash) > 0 {
		data = append(p.utf8Stash, data...)
		p.utf8Stash = p.utf8Stash[:0]
	}

	end := len(data)
	if end > 0 {
		if p.state == StateGround {
			lastRuneStart := end
			for i := 1; i <= 4 && lastRuneStart > 0; i++ {
				lastRuneStart--
				if utf8.RuneStart(data[lastRuneStart]) {
					break
				}
			}

			if !utf8.FullRune(data[lastRuneStart:]) {
				p.utf8Stash = append(p.utf8Stash, data[lastRuneStart:]...)
				end = lastRuneStart
			}
		}
	}
	dataToParse := data[:end]

	for i := 0; i < len(dataToParse); {
		b := dataToParse[i]
		var size int = 1

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
			default:
				var r rune
				r, size = utf8.DecodeRune(dataToParse[i:])
				p.vterm.placeChar(r)
			}
		case StateEscape:
			switch b {
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
				log.Printf("Parser: Unhandled ESC sequence: %q", b)
				p.state = StateGround
			}
		case StateCSI:
			switch {
			case b >= '0' && b <= '9':
				p.currentParam = p.currentParam*10 + int(b-'0')
			case b == ';':
				p.params = append(p.params, p.currentParam)
				p.currentParam = 0
			case b >= ' ' && b <= '/':
				// This range includes '!' for DECSTR
				p.intermediate = b
			case b >= '<' && b <= '?':
				p.private = true
			case b >= '@' && b <= '~':
				p.params = append(p.params, p.currentParam)
				p.vterm.ProcessCSI(b, p.params, p.intermediate)
				p.state = StateGround
			}
		// ... (rest of the cases remain the same) ...
		case StateOSC:
			if b == '\x07' {
				p.handleOSC()
				p.state = StateGround
			} else {
				p.oscBuffer = append(p.oscBuffer, b)
			}
		case StateDCS:
			if b == '\x1b' {
				p.state = StateDCSEscape
			}
		case StateDCSEscape:
			if b == '\\' {
				p.state = StateGround
			} else {
				p.state = StateDCS
			}
		case StateCharset:
			p.state = StateGround
		}
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
