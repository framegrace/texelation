package keybind

import "runtime"

// linuxPreset maps every action to its default key strings on Linux.
var linuxPreset = map[Action][]string{
	Help:          {"f1"},
	Screenshot:    {"f5"},
	Screensaver:   {"ctrl+s"},
	ConfigEditor:  {"f4"},
	ControlToggle: {"ctrl+a"},

	PaneNavUp:    {"shift+up"},
	PaneNavDown:  {"shift+down"},
	PaneNavLeft:  {"shift+left"},
	PaneNavRight: {"shift+right"},

	PaneResizeUp:    {"ctrl+up"},
	PaneResizeDown:  {"ctrl+down"},
	PaneResizeLeft:  {"ctrl+left"},
	PaneResizeRight: {"ctrl+right"},

	WorkspaceSwitchPrev: {"alt+left"},
	WorkspaceSwitchNext: {"alt+right"},

	ControlClose:    {"x"},
	ControlVSplit:   {"|"},
	ControlHSplit:   {"-"},
	ControlZoom:     {"z"},
	ControlSwap:     {"w"},
	ControlLauncher: {"l"},
	ControlHelp:     {"h"},
	ControlConfig:   {"f"},
	ControlRenameTab: {"t"},
	ControlNewTab:    {"T"},
	ControlCloseTab:  {"X"},

	TermSearch:      {"f3"},
	TermScrollbar:   {"f7"},
	TermTransformer: {"f8"},
	TermScreenshot:  {"ctrl+p"},
	TermScrollUp:    {"alt+up"},
	TermScrollDown:  {"alt+down"},
	TermScrollPgUp:  {"alt+pgup"},
	TermScrollPgDn:  {"alt+pgdn"},
}

// macPreset is a full copy of linuxPreset with macOS-specific overrides applied.
var macPreset = func() map[Action][]string {
	m := make(map[Action][]string, len(linuxPreset))
	for k, v := range linuxPreset {
		m[k] = v
	}
	// macOS overrides
	m[PaneNavUp] = []string{"alt+up"}
	m[PaneNavDown] = []string{"alt+down"}
	m[PaneNavLeft] = []string{"alt+left"}
	m[PaneNavRight] = []string{"alt+right"}
	m[WorkspaceSwitchPrev] = []string{"alt+["}
	m[WorkspaceSwitchNext] = []string{"alt+]"}
	return m
}()

// presetByName returns the named preset map. "auto" picks based on runtime.GOOS.
func presetByName(name string) map[Action][]string {
	switch name {
	case "mac":
		return macPreset
	case "auto":
		if runtime.GOOS == "darwin" {
			return macPreset
		}
		return linuxPreset
	default:
		return linuxPreset
	}
}
