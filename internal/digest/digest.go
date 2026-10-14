// Package digest renders a browsable markdown map of the corpus.
package digest

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Quette420/mdx_search/internal/index"
)

// Options controls the digest layout.
type Options struct {
	Title    string    // document heading
	GroupBy  string    // frontmatter field; empty groups by top-level directory
	Fields   []string  // frontmatter fields shown as tags under each note
	Since    time.Time // skip notes older than this
	MaxHeads int       // headings listed per note
	Now      time.Time // generation timestamp; zero means time.Now()
}

// Build renders a grouped map of the corpus: one block per file with its tags,
// date, outline and either the lead paragraph or an LLM summary.
func Build(ix *index.Index, opt Options) string {
	if opt.Title == "" {
		opt.Title = "Notes map"
	}
	if opt.MaxHeads <= 0 {
		opt.MaxHeads = 12
	}
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}

	groups := map[string][]*index.FileRec{}
	total := 0
	for _, f := range ix.Files {
		if !opt.Since.IsZero() {
			t := f.Date()
			if t.IsZero() || t.Before(opt.Since) {
				continue
			}
		}
		g := groupOf(f, opt.GroupBy)
		groups[g] = append(groups[g], f)
		total++
	}
	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	sort.Strings(names)

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", opt.Title)
	fmt.Fprintf(&b, "_Generated %s", opt.Now.Format("2006-01-02 15:04"))
	if !opt.Since.IsZero() {
		fmt.Fprintf(&b, ", changes since %s", opt.Since.Format("2006-01-02"))
	}
	fmt.Fprintf(&b, ". Files: %d._\n\n", total)

	for _, g := range names {
		files := groups[g]
		sort.Slice(files, func(i, j int) bool {
			di, dj := files[i].Date(), files[j].Date()
			if !di.Equal(dj) {
				return di.After(dj)
			}
			return files[i].Path < files[j].Path
		})
		fmt.Fprintf(&b, "## %s\n\n", g)
		for _, f := range files {
			writeFile(&b, f, opt)
		}
	}
	return b.String()
}

func groupOf(f *index.FileRec, field string) string {
	if field == "" {
		if dir := path.Dir(f.Path); dir != "." {
			return strings.SplitN(dir, "/", 2)[0]
		}
		return "(root)"
	}
	if v := f.MetaFirst(field); v != "" {
		return v
	}
	return "(no " + field + ")"
}

func writeFile(b *strings.Builder, f *index.FileRec, opt Options) {
	fmt.Fprintf(b, "### [%s](%s)\n\n", f.Title, f.Path)
	var tags []string
	for _, k := range opt.Fields {
		if v := f.Meta[strings.ToLower(k)]; len(v) > 0 {
			tags = append(tags, k+": "+strings.Join(v, ", "))
		}
	}
	if t := f.Date(); !t.IsZero() {
		tags = append(tags, t.Format("2006-01-02"))
	}
	if len(tags) > 0 {
		fmt.Fprintf(b, "`%s`\n\n", strings.Join(tags, " · "))
	}
	text := f.Summary
	if text == "" {
		text = f.Lead
	}
	if text != "" {
		fmt.Fprintf(b, "%s\n\n", text)
	}
	if len(f.Headings) > 0 {
		n := min(len(f.Headings), opt.MaxHeads)
		for _, h := range f.Headings[:n] {
			fmt.Fprintf(b, "- %s\n", h)
		}
		if len(f.Headings) > n {
			fmt.Fprintf(b, "- …%d more\n", len(f.Headings)-n)
		}
		b.WriteString("\n")
	}
}
