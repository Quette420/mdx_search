package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quette420/go_rag/internal/index"
)

func buildIndex(t *testing.T, files map[string]string) *index.Index {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ix := index.New(dir)
	if _, err := ix.Crawl(dir, index.CrawlOptions{}); err != nil {
		t.Fatal(err)
	}
	return ix
}

var corpus = map[string]string{
	"net/channels.md": "---\nstatus: open\ntags: [net]\n---\n# Channels\n\n" +
		"AllocateChannelSequence is called at 0xF90410 when a client connects.\n",
	"net/sockets.md": "---\nstatus: done\ntags: [net]\n---\n# Sockets\n\n" +
		"Sockets are allocated per channel; see the address table near 0xF90418.\n",
	"storage/cache.md": "---\nstatus: rejected\n---\n# Cache hypothesis\n\n" +
		"We assumed the cache replicates state; this was rejected.\n",
}

func TestExactIdentifierRanksFirst(t *testing.T) {
	ix := buildIndex(t, corpus)
	for _, q := range []string{"0xF90410", "AllocateChannelSequence"} {
		res := Search(ix, q, Filters{}, 5)
		if len(res) == 0 || res[0].Chunk.Path != "net/channels.md" || !res[0].Exact {
			t.Errorf("%q: unexpected top result %+v", q, res)
		}
	}
}

func TestSubwordMatch(t *testing.T) {
	ix := buildIndex(t, corpus)
	res := Search(ix, "sequence", Filters{}, 5)
	if len(res) == 0 || res[0].Chunk.Path != "net/channels.md" {
		t.Fatalf("camelCase sub-word not found: %+v", res)
	}
}

func TestFilters(t *testing.T) {
	ix := buildIndex(t, corpus)
	res := Search(ix, "channel", Filters{Meta: map[string]string{"status": "done"}}, 5)
	if len(res) != 1 || res[0].Chunk.Path != "net/sockets.md" {
		t.Fatalf("meta filter: %+v", res)
	}
	res = Search(ix, "replicated state", Filters{PathSub: "storage"}, 5)
	if len(res) != 1 || res[0].File.MetaFirst("status") != "rejected" {
		t.Fatalf("path filter / stemming: %+v", res)
	}
}

func TestSnippetCentersOnMatch(t *testing.T) {
	body := strings.Repeat("filler ", 100) + "needle here"
	s := Snippet(body, "needle", []string{"needle"})
	if !strings.Contains(s, "needle") || !strings.HasPrefix(s, "…") {
		t.Errorf("snippet = %q", s)
	}
}
