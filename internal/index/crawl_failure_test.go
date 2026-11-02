package index

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadFailureLeavesEntireIndexUnchanged(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.md", "# A\n\nold-alpha")
	write(t, root, "z.md", "# Z\n\nretained-zulu")
	ix := New(root)
	if _, err := ix.Crawl(root, CrawlOptions{}); err != nil {
		t.Fatal(err)
	}
	db := DefaultPath(root)
	if err := ix.Save(db); err != nil {
		t.Fatal(err)
	}
	before, err := Load(db)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "a.md", "# A\n\nnew-alpha")
	write(t, root, "b.md", "# B\n\nnew-bravo")
	// Real directory traversal and files; inject only the read error so this
	// failure is deterministic on Windows and when tests run as root.
	st, err := ix.crawl(root, CrawlOptions{}, func(path string) ([]byte, error) {
		if filepath.Base(path) == "z.md" {
			return nil, &fs.PathError{Op: "read", Path: path, Err: fs.ErrPermission}
		}
		return os.ReadFile(path)
	})
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("lost read failure: %v", err)
	}
	if st != (Stats{}) || !reflect.DeepEqual(ix, before) {
		t.Fatal("failed scan mutated file records, chunks or postings")
	}
	st, err = ix.Crawl(root, CrawlOptions{})
	if err != nil || st != (Stats{Added: 1, Updated: 1}) {
		t.Fatalf("recovery: %+v, %v", st, err)
	}
	if ix.Files["z.md"] == nil {
		t.Fatal("previously unreadable file was lost")
	}
}
