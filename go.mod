module td-fb

go 1.24.3

require (
	github.com/creack/pty v1.1.24
	github.com/nsf/termbox-go v1.1.1
	github.com/veops/go-ansiterm v0.0.5
)

require (
	github.com/mattn/go-runewidth v0.0.15 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)

replace github.com/veops/go-ansiterm => ./localmods/github.com/veops/go-ansiterm
