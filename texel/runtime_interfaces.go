package texel

import "github.com/gdamore/tcell/v2"

// ScreenDriver abstracts the rendering surface used by the desktop. It mirrors the
// subset of tcell.Screen functionality required today so we can swap in a remote
// implementation later.
type ScreenDriver interface {
	Init() error
	Fini()
	Size() (int, int)
	SetStyle(style tcell.Style)
	HideCursor()
	Show()
	PollEvent() tcell.Event
	SetContent(x, y int, mainc rune, combc []rune, style tcell.Style)
	GetContent(x, y int) (rune, []rune, tcell.Style, int)
}

// BufferStore tracks the last rendered buffer for a drawable region so we can
// compute diffs or persist snapshots.
type BufferStore interface {
	Snapshot() [][]Cell
	Save(buf [][]Cell)
	Clear()
}

// EventRouter exposes the subset of dispatcher behaviour the rest of the system
// relies on. Having it as an interface lets us inject remote or recorded routers.
type EventRouter interface {
	Subscribe(listener Listener)
	Unsubscribe(listener Listener)
	Broadcast(event Event)
}

// AppLifecycleManager governs how app instances are started and stopped. The
// default implementation simply runs apps locally, but remote runtimes can
// provide their own orchestration while preserving the same call sites.
type AppLifecycleManager interface {
	StartApp(app App)
	StopApp(app App)
}
