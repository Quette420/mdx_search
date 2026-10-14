package markdown

import (
	"slices"
	"strings"
	"testing"
)

const sample = `---
title: Replication notes
status: open
tags: [net, "state"]
aliases:
  - repl
  - sync
---

Intro paragraph that is
wrapped over two lines.

## Design

Text about design.

` + "```python" + `
# not a heading
print("hi")
` + "```" + `

### Details

More text.
`

func TestParseFrontmatter(t *testing.T) {
	doc := Parse("notes/repl.md", sample)
	if doc.Title != "Replication notes" {
		t.Errorf("title = %q", doc.Title)
	}
	if got := doc.Meta["tags"]; !slices.Equal(got, []string{"net", "state"}) {
		t.Errorf("tags = %v", got)
	}
	if got := doc.Meta["aliases"]; !slices.Equal(got, []string{"repl", "sync"}) {
		t.Errorf("aliases = %v", got)
	}
}

func TestChunkTracksFencesAndBreadcrumbs(t *testing.T) {
	doc := Parse("notes/repl.md", sample)
	var crumbs []string
	for _, s := range doc.Sections {
		crumbs = append(crumbs, s.Breadcrumb)
	}
	want := []string{"Replication notes", "Design", "Design > Details"}
	if !slices.Equal(crumbs, want) {
		t.Fatalf("breadcrumbs = %q, want %q", crumbs, want)
	}
	if !strings.Contains(doc.Sections[1].Body, "# not a heading") {
		t.Errorf("fenced comment was treated as a heading")
	}
	if doc.Sections[2].Line != 24 {
		t.Errorf("details line = %d, want 24", doc.Sections[2].Line)
	}
}

func TestOutlineLead(t *testing.T) {
	doc := Parse("x.md", sample)
	if doc.Lead != "Intro paragraph that is wrapped over two lines." {
		t.Errorf("lead = %q", doc.Lead)
	}
	if !slices.Equal(doc.Headings, []string{"  Design", "    Details"}) {
		t.Errorf("headings = %q", doc.Headings)
	}
}

func TestTitleFallsBackToFilename(t *testing.T) {
	if got := Parse("dir/my-note.md", "just text").Title; got != "my-note" {
		t.Errorf("title = %q", got)
	}
}

func TestSplitLongSection(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("| row | with | some | table | cells |\n")
	}
	doc := Parse("t.md", "# T\n\n"+b.String())
	if len(doc.Sections) < 2 {
		t.Fatalf("expected long table to be split, got %d chunk(s)", len(doc.Sections))
	}
	for _, s := range doc.Sections {
		if n := len([]rune(s.Body)); n > MaxChunkRunes*3/2+100 {
			t.Errorf("chunk too large: %d runes", n)
		}
	}
}
