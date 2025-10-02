package texel

// InMemoryBufferStore is a simple BufferStore backed by a [][]Cell slice. It is
// sufficient for local rendering and acts as the default implementation until a
// remote snapshot store is introduced.
type InMemoryBufferStore struct {
	buf [][]Cell
}

// Snapshot returns the last saved buffer. Callers should treat the returned
// value as read-only.
func (s *InMemoryBufferStore) Snapshot() [][]Cell {
	return s.buf
}

// Save stores the given buffer reference for later diffing.
func (s *InMemoryBufferStore) Save(buf [][]Cell) {
	s.buf = buf
}

// Clear resets the stored buffer reference.
func (s *InMemoryBufferStore) Clear() {
	s.buf = nil
}

// NewInMemoryBufferStore constructs an empty buffer store.
func NewInMemoryBufferStore() BufferStore {
	return &InMemoryBufferStore{}
}
