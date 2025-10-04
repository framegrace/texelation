package texel

// DesktopFocusListener describes consumers interested in focus changes.
type DesktopFocusListener interface {
	PaneFocused(paneID [16]byte)
}
