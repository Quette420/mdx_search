package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineCorpusLifecycle(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	root := t.TempDir()
	writeNote := func(name, text string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	command := func(args ...string) string {
		t.Helper()
		args = append(args, "-root", root)
		code, out, errs := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, out, errs)
		}
		return out
	}
	query := func(q string, filters ...string) []jsonHit {
		t.Helper()
		args := append([]string{"search", q, "-json"}, filters...)
		var hits []jsonHit
		if err := json.Unmarshal([]byte(command(args...)), &hits); err != nil {
			t.Fatal(err)
		}
		return hits
	}
	writeNote("network/transport.md", "---\nstatus: open\ntags: [networking]\n---\n# Transport\n\nAllocateChannelSequence handles retries and backoff.\n\n## Очередь\n\nОчередь сообщений сохраняет порядок.\n")
	writeNote("retired.md", "# Retired\n\nobsoletequartz\n")
	if out := command("index"); !strings.Contains(out, "+2 ~0 -0") {
		t.Fatal(out)
	}
	hits := query("AllocateChannelSequence", "-f", "status=open")
	if len(hits) != 1 || hits[0].Path != "network/transport.md" || hits[0].Line != 7 || hits[0].Breadcrumb != "Transport" || !hits[0].Exact {
		t.Fatalf("literal provenance: %+v", hits)
	}
	if hits := query("backoff"); len(hits) == 0 {
		t.Fatal("English query lost")
	}
	hits = query("очередь", "-f", "tags=networking")
	if len(hits) != 1 || hits[0].Line != 11 || hits[0].Breadcrumb != "Transport > Очередь" {
		t.Fatalf("Russian heading/line: %+v", hits)
	}
	if hits := query("AllocateChannelSequence", "-f", "status=done"); len(hits) != 0 {
		t.Fatalf("filter leaked: %+v", hits)
	}
	if out := command("stats"); !strings.Contains(out, "files 2") {
		t.Fatal(out)
	}

	writeNote("network/transport.md", "---\nstatus: done\ntags: [networking]\n---\n# Transport revised\n\nCommitChannelSequence uses bounded retries.\n")
	writeNote("new.md", "# New\n\nfreshcobalt\n")
	if err := os.Remove(filepath.Join(root, "retired.md")); err != nil {
		t.Fatal(err)
	}
	if out := command("index"); !strings.Contains(out, "+1 ~1 -1") {
		t.Fatal(out)
	}
	for _, hit := range query("AllocateChannelSequence") {
		// Identifier splitting may legitimately find the shared Channel/Sequence
		// terms; it must not claim the removed identifier as a literal hit.
		if hit.Exact || strings.Contains(hit.Body, "AllocateChannelSequence") {
			t.Fatalf("old literal survived: %+v", hit)
		}
	}
	for _, obsolete := range []string{"obsoletequartz", "очередь"} {
		if hits := query(obsolete); len(hits) != 0 {
			t.Fatalf("stale %q hits after reload: %+v", obsolete, hits)
		}
	}
	hits = query("CommitChannelSequence", "-f", "status=done")
	if len(hits) != 1 || hits[0].Line != 7 || hits[0].Breadcrumb != "Transport revised" || !hits[0].Exact {
		t.Fatalf("updated provenance: %+v", hits)
	}
	if hits := query("freshcobalt"); len(hits) != 1 || hits[0].Path != "new.md" {
		t.Fatalf("added note: %+v", hits)
	}
	command("digest")
	mapBytes, err := os.ReadFile(filepath.Join(root, "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(mapBytes)
	if !strings.Contains(contents, "network/transport.md") || !strings.Contains(contents, "new.md") || strings.Contains(contents, "retired.md") {
		t.Fatal(contents)
	}
	if out := command("index"); !strings.Contains(out, "+0 ~0 -0") || !strings.Contains(out, "2 files") {
		t.Fatalf("digest indexed itself: %s", out)
	}
}
