module texelation

go 1.24.3

require (
	github.com/creack/pty v1.1.24
	github.com/gdamore/tcell/v2 v2.8.1
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0
	github.com/mattn/go-runewidth v0.0.16
	golang.org/x/image v0.29.0
	golang.org/x/term v0.28.0
)

require (
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/rivo/uniseg v0.4.3 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.27.0 // indirect
)

replace github.com/veops/go-ansiterm => ./localmods/github.com/veops/go-ansiterm
