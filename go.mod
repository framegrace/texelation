module textmode-env

go 1.24.3

require (
	github.com/creack/pty v1.1.24
	github.com/nsf/termbox-go v1.1.1
)

require (
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/rivo/uniseg v0.4.3 // indirect
)

replace github.com/veops/go-ansiterm => ./localmods/github.com/veops/go-ansiterm
