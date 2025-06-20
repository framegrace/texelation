package parser

import (
	"log"
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
	intermediate rune
}

func NewParser(v *VTerm) *Parser {
	return &Parser{
		state:     StateGround,
		vterm:     v,
		params:    make([]int, 0, 16),
		oscBuffer: make([]rune, 0, 128),
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
			p.vterm.CarriageReturn()
			p.vterm.LineFeed()
		case '\r':
			p.vterm.CarriageReturn()
		case '\b':
			p.vterm.Backspace()
		case '\t':
			p.vterm.Tab()
		default:
			if r < ' ' {
				// Ignore other control characters for now
			} else {
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
		case r >= '@' && r <= '~':
			p.params = append(p.params, p.currentParam)
			p.vterm.ProcessCSI(r, p.params, p.intermediate)
			//			p.vterm.DumpGrid("After CSI sequence")
			p.state = StateGround
		}
	case StateOSC:
		if r == '\x07' {
			p.handleOSC()
			p.state = StateGround
		} else {
			p.oscBuffer = append(p.oscBuffer, r)
		}
	case StateDCS:
		if r == '\x1b' {
			p.state = StateDCSEscape
		}
	case StateDCSEscape:
		if r == '\\' {
			p.state = StateGround
		} else {
			p.state = StateDCS
		}
	case StateCharset:
		p.state = StateGround
	}
}

func (p *Parser) handleOSC() {
	parts := splitRunesN(p.oscBuffer, ';', 2)
	if len(parts) != 2 {
		return
	}
	command := string(parts[0])
	content := string(parts[1])
	if command == "0" {
		p.vterm.SetTitle(content)
	}
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
