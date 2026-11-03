package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	code := Run(args, &out, &errs)
	return code, out.String(), errs.String()
}

func TestMissingCorpusDoesNotReplaceSavedIndex(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "notes")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n\nretained-marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(base, "index.gob")
	if code, _, errs := runCLI(t, "index", "-root", root, "-db", db); code != 0 {
		t.Fatal(errs)
	}
	before, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-offline"); err != nil {
		t.Fatal(err)
	}
	code, _, errs := runCLI(t, "index", "-root", root, "-db", db)
	if code != 1 || errs == "" {
		t.Errorf("missing root must fail, got code=%d stderr=%q", code, errs)
	}
	after, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("missing root replaced the saved index")
	}
	if err := os.Rename(root+"-offline", root); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := runCLI(t, "search", "retained-marker", "-root", root, "-db", db); code != 0 || !bytes.Contains([]byte(out), []byte("a.md")) {
		t.Fatalf("saved search after recovery: %d %s %s", code, out, errs)
	}
}

func TestCorruptIndexIsNotSilentlyRebuilt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n\nalpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(root, "broken.gob")
	before := []byte("not a gob index")
	if err := os.WriteFile(db, before, 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := runCLI(t, "index", "-root", root, "-db", db)
	if code != 1 || errs == "" {
		t.Errorf("corrupt index must fail explicitly: code=%d stderr=%q", code, errs)
	}
	after, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("corrupt index was silently overwritten")
	}
}
