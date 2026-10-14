package markdown

import (
	"path"
	"strings"
)

// Document is a parsed note.
type Document struct {
	Title    string
	Meta     map[string][]string
	Headings []string // indented by level, used by digest
	Lead     string   // first prose paragraph
	Sections []Section
}

// Parse parses a note. rel is its slash-separated path, used as a title fallback.
func Parse(rel, text string) Document {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	meta, start := ParseFrontmatter(lines)

	title := ""
	if v := meta["title"]; len(v) > 0 {
		title = v[0]
	}
	if title == "" {
		for i := start; i < len(lines); i++ {
			if lvl, t := HeadingLevel(lines[i]); lvl == 1 {
				title = t
				break
			}
		}
	}
	if title == "" {
		title = strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	}

	heads, lead := Outline(lines, start)
	return Document{
		Title:    title,
		Meta:     meta,
		Headings: heads,
		Lead:     lead,
		Sections: Chunk(lines, start, title),
	}
}

// Outline returns the heading list and the first prose paragraph.
func Outline(lines []string, start int) ([]string, string) {
	var heads []string
	var leadBuf []string
	leadDone := false
	fence := ""
	for i := start; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if fence != "" {
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = t[:3]
			continue
		}
		if lvl, text := HeadingLevel(lines[i]); lvl > 0 {
			heads = append(heads, strings.Repeat("  ", lvl-1)+text)
			continue
		}
		// Notes are often hard-wrapped, so the lead paragraph is collected up to
		// a blank line; otherwise the digest would show a truncated first line.
		if !leadDone && t != "" && !strings.HasPrefix(t, "|") && !strings.HasPrefix(t, ">") {
			leadBuf = append(leadBuf, t)
			continue
		}
		if len(leadBuf) > 0 && t == "" {
			leadDone = true
		}
	}
	lead := strings.Join(leadBuf, " ")
	if r := []rune(lead); len(r) > 400 {
		lead = strings.TrimSpace(string(r[:400])) + "…"
	}
	return heads, lead
}
