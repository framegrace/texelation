package parser

type State int

const (
	StateGround State = iota
	StateEscape
	StateCSI
)

// Parser is a VT100/ANSI stream parser.
type Parser struct {
	state        State
	vterm        *VTerm
	params       []int
	currentParam int
	private      bool
}

// NewParser creates a new parser associated with a virtual terminal.
func NewParser(v *VTerm) *Parser {
	return &Parser{
		state:  StateGround,
		vterm:  v,
		params: make([]int, 0, 16),
	}
}

// Parse processes a slice of bytes from the PTY.
func (p *Parser) Parse(data []byte) {
	for _, b := range data {
		switch p.state {
		case StateGround:
			switch b {
			case '\x1b':
				p.state = StateEscape
			case '\n':
				p.vterm.LineFeed()
			case '\r':
				p.vterm.CarriageReturn()
			case '\b':
				p.vterm.Backspace()
			case '\t':
				p.vterm.Tab()
			default:
				p.vterm.placeChar(rune(b))
			}
		case StateEscape:
			switch b {
			case '[': // Control Sequence Introducer
				p.state = StateCSI
				p.params = p.params[:0]
				p.currentParam = 0
				p.private = false
			case '=', '>': // Keypad modes
				p.state = StateGround // Recognize but ignore
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
		}
	}
}
