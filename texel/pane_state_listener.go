package texel

// PaneStateListener observes active/resizing changes so remotes can mirror visuals.
type PaneStateListener interface {
	PaneStateChanged(id [16]byte, active bool, resizing bool, z int)
}
