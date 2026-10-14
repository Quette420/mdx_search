package markdown

import "strings"

// MaxChunkRunes is the soft size limit of a single chunk.
const MaxChunkRunes = 2000

// Section is one searchable piece of a note.
type Section struct {
	Breadcrumb string // "H1 > H2 > H3" path of the enclosing headings
	Line       int    // 1-based line of the first body line
	Body       string
}

// HeadingLevel returns the ATX heading level of line (0 if it is not a heading)
// and the heading text.
func HeadingLevel(line string) (int, string) {
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || i >= len(line) || line[i] != ' ' {
		return 0, ""
	}
	return i, strings.TrimSpace(strings.TrimRight(line[i+1:], "#"))
}

// Chunk splits markdown into one section per heading. Fenced code is tracked
// so that '#' comments inside shell/C++/Python snippets are not read as headings.
func Chunk(lines []string, start int, title string) []Section {
	var chunks []Section
	var stack []string
	fence := ""
	bufLine := start + 1 // 1-based line of the first line in buf
	var buf []string

	emit := func() {
		crumb := title
		if path := nonEmpty(stack); len(path) > 0 {
			crumb = strings.Join(path, " > ")
		}
		for _, p := range splitSection(buf) {
			chunks = append(chunks, Section{Breadcrumb: crumb, Line: bufLine + p.off, Body: p.text})
		}
		buf = buf[:0]
	}

	for i := start; i < len(lines); i++ {
		ln := lines[i]
		t := strings.TrimSpace(ln)
		if fence != "" {
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
			buf = append(buf, ln)
			continue
		}
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = t[:3]
			buf = append(buf, ln)
			continue
		}
		if lvl, text := HeadingLevel(ln); lvl > 0 {
			emit()
			if lvl <= len(stack) {
				stack = stack[:lvl-1]
			}
			for len(stack) < lvl-1 {
				stack = append(stack, "")
			}
			stack = append(stack, text)
			bufLine = i + 2 // section content starts on the line after the heading
			continue
		}
		buf = append(buf, ln)
	}
	emit()
	return chunks
}

// part is a piece of a section with its line offset from the section start,
// so the reported line points at the fragment itself rather than the heading.
type part struct {
	off  int
	text string
}

// splitSection breaks an oversized section, preferring paragraph boundaries.
// A paragraph longer than 1.5x the limit (a big table, a list without blank
// lines) is cut at a line boundary: otherwise such documents produce a single
// giant chunk that hurts both BM25 and snippets.
func splitSection(lines []string) []part {
	lo := 0
	for lo < len(lines) && strings.TrimSpace(lines[lo]) == "" {
		lo++
	}
	hi := len(lines)
	for hi > lo && strings.TrimSpace(lines[hi-1]) == "" {
		hi--
	}
	if lo >= hi {
		return nil
	}
	body := lines[lo:hi]

	total := 0
	for _, l := range body {
		total += len([]rune(l)) + 1
	}
	if total <= MaxChunkRunes {
		return []part{{off: lo, text: strings.Join(body, "\n")}}
	}

	var out []part
	var buf []string
	bufStart, n := 0, 0
	fence := ""
	flush := func() {
		if t := strings.TrimSpace(strings.Join(buf, "\n")); t != "" {
			out = append(out, part{off: lo + bufStart, text: t})
		}
		buf, n = nil, 0
	}
	for i, l := range body {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			if fence == "" {
				fence = t[:3]
			} else if strings.HasPrefix(t, fence) {
				fence = ""
			}
		}
		if len(buf) == 0 {
			if t == "" {
				continue // never start a chunk on a blank line: the line number must point at text
			}
			bufStart = i
		}
		buf = append(buf, l)
		n += len([]rune(l)) + 1
		if n < MaxChunkRunes || fence != "" {
			continue
		}
		atBreak := t == "" || i+1 >= len(body) || strings.TrimSpace(body[i+1]) == ""
		if atBreak || n >= MaxChunkRunes*3/2 {
			flush()
		}
	}
	flush()
	return out
}

// nonEmpty drops the placeholders left by skipped heading levels (## without #).
func nonEmpty(stack []string) []string {
	var out []string
	for _, s := range stack {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
