package index

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCrawlIsIncremental(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.md", "# A\n\nalpha")
	write(t, dir, "sub/b.md", "# B\n\nbeta")
	write(t, dir, ".hidden/c.md", "# C")
	write(t, dir, "skip.txt", "not markdown")

	ix := New(dir)
	st, err := ix.Crawl(dir, CrawlOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if st != (Stats{Added: 2}) {
		t.Fatalf("first crawl: %+v", st)
	}

	st, _ = ix.Crawl(dir, CrawlOptions{})
	if st != (Stats{}) {
		t.Fatalf("unchanged crawl: %+v", st)
	}

	write(t, dir, "a.md", "# A\n\nalpha changed")
	os.Remove(filepath.Join(dir, "sub", "b.md"))
	st, _ = ix.Crawl(dir, CrawlOptions{})
	if st != (Stats{Updated: 1, Removed: 1}) {
		t.Fatalf("third crawl: %+v", st)
	}
	if len(ix.Files) != 1 {
		t.Fatalf("files = %d", len(ix.Files))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.md", "---\nstatus: open\n---\n# A\n\nalpha")
	ix := New(dir)
	if _, err := ix.Crawl(dir, CrawlOptions{}); err != nil {
		t.Fatal(err)
	}
	p := DefaultPath(dir)
	if err := ix.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Files["a.md"].MetaHas("status", "OPEN") {
		t.Errorf("metadata lost: %+v", got.Files["a.md"].Meta)
	}
}
