package index

import (
	"encoding/gob"
	"os"
	"path/filepath"
)

// DefaultPath returns the conventional index location for a corpus root.
func DefaultPath(root string) string {
	return filepath.Join(root, ".mdx", "index.gob")
}

func Load(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ix := &Index{}
	if err := gob.NewDecoder(f).Decode(ix); err != nil {
		return nil, err
	}
	if ix.Files == nil {
		ix.Files = map[string]*FileRec{}
	}
	if ix.Chunks == nil {
		ix.Chunks = map[int]*Chunk{}
	}
	return ix, nil
}

// Save writes the index atomically (temp file + rename).
func (ix *Index) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(ix); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
