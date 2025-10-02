package server

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"texelation/texel"
)

// SnapshotStore persists pane snapshots to disk with a content hash for integrity checks.
type SnapshotStore struct {
	path string
	mu   sync.Mutex
}

// StoredSnapshot is the serialized representation written to disk.
type StoredSnapshot struct {
	Timestamp time.Time    `json:"timestamp"`
	Hash      string       `json:"hash"`
	Panes     []StoredPane `json:"panes"`
}

// StoredPane represents a single pane's textual content.
type StoredPane struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Rows  []string `json:"rows"`
}

func NewSnapshotStore(path string) *SnapshotStore {
	return &SnapshotStore{path: path}
}

// Save writes the current snapshots to disk, computing a SHA-1 hash for integrity.
func (s *SnapshotStore) Save(panes []texel.PaneSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored := StoredSnapshot{
		Timestamp: time.Now().UTC(),
		Panes:     make([]StoredPane, len(panes)),
	}

	hasher := sha1.New()

	for i, pane := range panes {
		id := hex.EncodeToString(pane.ID[:])
		rows := make([]string, len(pane.Buffer))
		for y, row := range pane.Buffer {
			runes := make([]rune, len(row))
			for x, cell := range row {
				if cell.Ch == 0 {
					runes[x] = ' '
				} else {
					runes[x] = cell.Ch
				}
			}
			rows[y] = string(runes)
			hasher.Write([]byte(rows[y]))
		}

		hasher.Write(pane.ID[:])
		hasher.Write([]byte(pane.Title))

		stored.Panes[i] = StoredPane{
			ID:    id,
			Title: pane.Title,
			Rows:  rows,
		}
	}

	stored.Hash = hex.EncodeToString(hasher.Sum(nil))

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0o644)
}

// Load retrieves the most recent stored snapshot from disk.
func (s *SnapshotStore) Load() (StoredSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var stored StoredSnapshot
	data, err := os.ReadFile(s.path)
	if err != nil {
		return stored, err
	}

	if err := json.Unmarshal(data, &stored); err != nil {
		return stored, err
	}
	return stored, nil
}

func (sp StoredPane) ToPaneSnapshot() texel.PaneSnapshot {
	var id [16]byte
	decoded, err := hex.DecodeString(sp.ID)
	if err == nil && len(decoded) >= 16 {
		copy(id[:], decoded[:16])
	}

	buffer := make([][]texel.Cell, len(sp.Rows))
	for i, row := range sp.Rows {
		buffer[i] = make([]texel.Cell, len([]rune(row)))
		for j, ch := range []rune(row) {
			buffer[i][j] = texel.Cell{Ch: ch}
		}
	}

	return texel.PaneSnapshot{ID: id, Title: sp.Title, Buffer: buffer}
}
