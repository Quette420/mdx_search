package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Quette420/go_rag/internal/digest"
	"github.com/Quette420/go_rag/internal/index"
	"github.com/Quette420/go_rag/internal/search"
	"github.com/Quette420/go_rag/internal/summarize"
)

const defaultDigestFile = "INDEX.md"

var defaultShow = listFlag{"status", "tags"}

func cmdIndex(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	var c common
	c.register(fs)
	var exts listFlag
	fs.Var(&exts, "ext", "file extensions to index (default .md,.markdown)")
	var ex listFlag
	fs.Var(&ex, "exclude", "skip files by base-name glob or path substring (repeatable)")
	fs.Parse(reorder(fs, args))

	root, db, err := c.resolve()
	if err != nil {
		return err
	}
	ix, err := index.Load(db)
	if err != nil {
		ix = index.New(root)
	}
	ix.Root = root
	ex = append(ex, defaultDigestFile)

	t0 := time.Now()
	st, err := ix.Crawl(root, index.CrawlOptions{Extensions: exts, Exclude: ex})
	if err != nil {
		return err
	}
	if err := ix.Save(db); err != nil {
		return err
	}
	fmt.Fprintf(out, "indexed: +%d ~%d -%d · %d files, %d chunks, %d terms · %s\n",
		st.Added, st.Updated, st.Removed, len(ix.Files), len(ix.Chunks), len(ix.Postings),
		time.Since(t0).Round(time.Millisecond))
	return nil
}

func cmdSearch(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	var c common
	c.register(fs)
	k := fs.Int("k", 10, "number of results")
	filters := kvFlag{}
	fs.Var(filters, "f", "frontmatter filter key=value (repeatable, all must match)")
	pth := fs.String("path", "", "only files whose path contains this substring")
	since := fs.String("since", "", "only notes dated after this")
	var show listFlag
	fs.Var(&show, "show", "frontmatter fields to print with each hit (default status,tags)")
	full := fs.Bool("full", false, "print the whole chunk instead of a snippet")
	asJSON := fs.Bool("json", false, "machine-readable output")
	fs.Parse(reorder(fs, args))

	if len(show) == 0 {
		show = defaultShow
	}
	q := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if q == "" {
		return fmt.Errorf("empty query")
	}
	ix, _, err := c.open()
	if err != nil {
		return err
	}
	sinceT, err := parseSince(*since, time.Now())
	if err != nil {
		return err
	}

	res := search.Search(ix, q, search.Filters{Meta: filters, PathSub: *pth, Since: sinceT}, *k)
	if *asJSON {
		return writeJSON(out, res)
	}
	if len(res) == 0 {
		fmt.Fprintln(out, "no results")
		return nil
	}
	fields := uniq(append(show, keys(filters)...))
	for i, r := range res {
		mark := ""
		if r.Exact {
			mark = " ="
		}
		fmt.Fprintf(out, "\n%d. %s:%d%s\n", i+1, r.Chunk.Path, r.Chunk.Line, mark)
		if r.Chunk.Breadcrumb != "" {
			fmt.Fprintf(out, "   %s\n", r.Chunk.Breadcrumb)
		}
		if meta := metaLine(r.File, fields); meta != "" {
			fmt.Fprintf(out, "   %s\n", meta)
		}
		if *full {
			for _, ln := range strings.Split(r.Chunk.Body, "\n") {
				fmt.Fprintf(out, "   | %s\n", ln)
			}
		} else {
			fmt.Fprintf(out, "   %s\n", r.Snippet)
		}
	}
	fmt.Fprintln(out)
	return nil
}

type jsonHit struct {
	Path       string              `json:"path"`
	Line       int                 `json:"line"`
	Title      string              `json:"title"`
	Breadcrumb string              `json:"breadcrumb"`
	Score      float64             `json:"score"`
	Exact      bool                `json:"exact"`
	Snippet    string              `json:"snippet"`
	Body       string              `json:"body"`
	Meta       map[string][]string `json:"meta,omitempty"`
}

func writeJSON(out io.Writer, res []search.Result) error {
	hits := make([]jsonHit, 0, len(res))
	for _, r := range res {
		h := jsonHit{
			Path: r.Chunk.Path, Line: r.Chunk.Line, Breadcrumb: r.Chunk.Breadcrumb,
			Score: r.Score, Exact: r.Exact, Snippet: r.Snippet, Body: r.Chunk.Body,
		}
		if r.File != nil {
			h.Title, h.Meta = r.File.Title, r.File.Meta
		}
		hits = append(hits, h)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(hits)
}

func metaLine(f *index.FileRec, fields []string) string {
	if f == nil {
		return ""
	}
	var parts []string
	for _, k := range fields {
		if v := f.Meta[strings.ToLower(k)]; len(v) > 0 {
			parts = append(parts, k+"="+strings.Join(v, ","))
		}
	}
	return strings.Join(parts, " ")
}

func cmdDigest(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("digest", flag.ExitOnError)
	var c common
	c.register(fs)
	dst := fs.String("out", defaultDigestFile, "output file, relative to root (- for stdout)")
	title := fs.String("title", "Notes map", "digest heading")
	since := fs.String("since", "", "only notes dated after this")
	group := fs.String("group", "", "frontmatter field to group by (default: top-level folder)")
	var show listFlag
	fs.Var(&show, "show", "frontmatter fields shown under each note (default status,tags)")
	sum := fs.Bool("summarize", false, "generate summaries with the Anthropic API (needs ANTHROPIC_API_KEY)")
	model := fs.String("model", summarize.DefaultModel, "model for -summarize")
	promptFile := fs.String("prompt", "", "file with a custom summary prompt (one %s for the note)")
	limit := fs.Int("limit", 0, "max files to summarize per run (0 = all)")
	fs.Parse(reorder(fs, args))

	if len(show) == 0 {
		show = defaultShow
	}
	ix, root, err := c.open()
	if err != nil {
		return err
	}
	_, db, _ := c.resolve()
	sinceT, err := parseSince(*since, time.Now())
	if err != nil {
		return err
	}

	if *sum {
		prompt := ""
		if *promptFile != "" {
			b, err := os.ReadFile(*promptFile)
			if err != nil {
				return err
			}
			prompt = string(b)
		}
		n, err := summarize.Run(context.Background(), ix, summarize.Options{
			Model: *model, Prompt: prompt, Limit: *limit, Log: errOut,
		})
		if n > 0 {
			if serr := ix.Save(db); serr != nil {
				return serr
			}
			fmt.Fprintf(errOut, "summaries updated: %d\n", n)
		}
		if err != nil {
			return err
		}
	}

	text := digest.Build(ix, digest.Options{Title: *title, GroupBy: *group, Fields: show, Since: sinceT})
	if *dst == "-" {
		fmt.Fprint(out, text)
		return nil
	}
	path := *dst
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "wrote %s\n", path)
	return nil
}

// statsSkip lists frontmatter fields that are unique per note and therefore
// useless as a breakdown.
var statsSkip = map[string]bool{"title": true, "date": true, "created": true, "updated": true, "aliases": true}

func cmdStats(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	var c common
	c.register(fs)
	var fields listFlag
	fs.Var(&fields, "field", "frontmatter fields to break down (default: auto-detect)")
	maxDistinct := fs.Int("max-values", 30, "auto-detect: skip fields with more distinct values")
	fs.Parse(reorder(fs, args))

	ix, _, err := c.open()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "files %d · chunks %d · terms %d · avg chunk length %.0f tokens\n",
		len(ix.Files), len(ix.Chunks), len(ix.Postings), ix.AvgLen)

	counts := map[string]map[string]int{}
	present := map[string]int{}
	for _, f := range ix.Files {
		for k, vs := range f.Meta {
			if len(vs) == 0 {
				continue
			}
			present[k]++
			if counts[k] == nil {
				counts[k] = map[string]int{}
			}
			for _, v := range vs {
				counts[k][v]++
			}
		}
	}

	if len(fields) == 0 {
		for k, vals := range counts {
			if !statsSkip[k] && len(vals) <= *maxDistinct && present[k] > 1 {
				fields = append(fields, k)
			}
		}
		sort.Slice(fields, func(i, j int) bool {
			if present[fields[i]] != present[fields[j]] {
				return present[fields[i]] > present[fields[j]]
			}
			return fields[i] < fields[j]
		})
	}

	for _, key := range fields {
		key = strings.ToLower(key)
		vals := counts[key]
		if len(vals) == 0 {
			continue
		}
		names := keys(vals)
		sort.Slice(names, func(i, j int) bool {
			if vals[names[i]] != vals[names[j]] {
				return vals[names[i]] > vals[names[j]]
			}
			return names[i] < names[j]
		})
		fmt.Fprintf(out, "\n%s:\n", key)
		for _, v := range names {
			fmt.Fprintf(out, "  %-24s %d\n", v, vals[v])
		}
		if miss := len(ix.Files) - present[key]; miss > 0 {
			fmt.Fprintf(out, "  %-24s %d\n", "(not set)", miss)
		}
	}
	return nil
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
