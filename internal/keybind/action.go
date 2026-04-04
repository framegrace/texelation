package keybind

// Action is a named keyboard action.
type Action string

// ActionInfo holds metadata for an action.
type ActionInfo struct {
	Description string
	Category    string
}

// ActionEntry is returned by AllActions for help display.
type ActionEntry struct {
	Action      Action
	Description string
	Category    string
	Keys        []KeyCombo
}

// Desktop actions.
const (
	Help          Action = "help"
	Screenshot    Action = "screenshot"
	Screensaver   Action = "screensaver"
	ConfigEditor  Action = "config.editor"
	ControlToggle Action = "control.toggle"
)

// Pane actions.
const (
	PaneNavUp      Action = "pane.navigate.up"
	PaneNavDown    Action = "pane.navigate.down"
	PaneNavLeft    Action = "pane.navigate.left"
	PaneNavRight   Action = "pane.navigate.right"
	PaneResizeUp   Action = "pane.resize.up"
	PaneResizeDown Action = "pane.resize.down"
	PaneResizeLeft Action = "pane.resize.left"
	PaneResizeRight Action = "pane.resize.right"
)

// Workspace actions.
const (
	WorkspaceSwitchPrev Action = "workspace.switch.prev"
	WorkspaceSwitchNext Action = "workspace.switch.next"
	WorkspaceTabPrev    Action = "workspace.tab.prev"
	WorkspaceTabNext    Action = "workspace.tab.next"
)

// Control mode actions (after prefix).
const (
	ControlClose     Action = "control.close"
	ControlVSplit    Action = "control.vsplit"
	ControlHSplit    Action = "control.hsplit"
	ControlZoom      Action = "control.zoom"
	ControlSwap      Action = "control.swap"
	ControlLauncher  Action = "control.launcher"
	ControlHelp      Action = "control.help"
	ControlConfig    Action = "control.config"
	ControlNewTab    Action = "control.new_tab"
	ControlCloseTab  Action = "control.close_tab"
)

// Texelterm actions.
const (
	TermSearch      Action = "texelterm.search"
	TermScrollbar   Action = "texelterm.scrollbar"
	TermTransformer Action = "texelterm.transformer"
	TermScreenshot  Action = "texelterm.screenshot"
	TermScrollUp    Action = "texelterm.scroll.up"
	TermScrollDown  Action = "texelterm.scroll.down"
	TermScrollPgUp  Action = "texelterm.scroll.pgup"
	TermScrollPgDn  Action = "texelterm.scroll.pgdn"
)

// ActionDescriptions maps every action to its metadata.
var ActionDescriptions = map[Action]ActionInfo{
	// Desktop
	Help:          {Description: "Open help overlay", Category: "Desktop"},
	Screenshot:    {Description: "Save workspace screenshot as PNG", Category: "Desktop"},
	Screensaver:   {Description: "Activate screensaver", Category: "Desktop"},
	ConfigEditor:  {Description: "Open configuration editor", Category: "Desktop"},
	ControlToggle: {Description: "Toggle control mode", Category: "Desktop"},

	// Pane
	PaneNavUp:       {Description: "Move focus to pane above", Category: "Pane"},
	PaneNavDown:     {Description: "Move focus to pane below", Category: "Pane"},
	PaneNavLeft:     {Description: "Move focus to pane left", Category: "Pane"},
	PaneNavRight:    {Description: "Move focus to pane right", Category: "Pane"},
	PaneResizeUp:    {Description: "Shrink active pane vertically", Category: "Pane"},
	PaneResizeDown:  {Description: "Grow active pane vertically", Category: "Pane"},
	PaneResizeLeft:  {Description: "Shrink active pane horizontally", Category: "Pane"},
	PaneResizeRight: {Description: "Grow active pane horizontally", Category: "Pane"},

	// Workspace
	WorkspaceSwitchPrev: {Description: "Switch to previous workspace", Category: "Workspace"},
	WorkspaceSwitchNext: {Description: "Switch to next workspace", Category: "Workspace"},
	WorkspaceTabPrev:    {Description: "Previous workspace (tab mode)", Category: "Workspace"},
	WorkspaceTabNext:    {Description: "Next workspace (tab mode)", Category: "Workspace"},

	// Control
	ControlClose:    {Description: "Close active pane", Category: "Control"},
	ControlVSplit:   {Description: "Split pane vertically", Category: "Control"},
	ControlHSplit:   {Description: "Split pane horizontally", Category: "Control"},
	ControlZoom:     {Description: "Toggle pane zoom", Category: "Control"},
	ControlSwap:     {Description: "Enter pane swap mode", Category: "Control"},
	ControlLauncher: {Description: "Open app launcher", Category: "Control"},
	ControlHelp:     {Description: "Open help", Category: "Control"},
	ControlConfig:   {Description: "Open config editor", Category: "Control"},
	ControlNewTab:   {Description: "Create new workspace", Category: "Control"},
	ControlCloseTab: {Description: "Close workspace", Category: "Control"},

	// Terminal
	TermSearch:      {Description: "Toggle history search", Category: "Terminal"},
	TermScrollbar:   {Description: "Toggle scrollbar", Category: "Terminal"},
	TermTransformer: {Description: "Toggle transformer pipeline", Category: "Terminal"},
	TermScreenshot:  {Description: "Save pane screenshot as PNG", Category: "Terminal"},
	TermScrollUp:    {Description: "Scroll up one line", Category: "Terminal"},
	TermScrollDown:  {Description: "Scroll down one line", Category: "Terminal"},
	TermScrollPgUp:  {Description: "Scroll up one page", Category: "Terminal"},
	TermScrollPgDn:  {Description: "Scroll down one page", Category: "Terminal"},
}
