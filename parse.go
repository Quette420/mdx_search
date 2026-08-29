package main

import (
	"strings"
)

const maxChunkRunes = 2000

type draftChunk struct {
	Breadcrumb string
	Line       int
	Body       string
}

// parseFrontmatter reads a minimal YAML subset: scalars, inline [a, b] lists
// and dash lists. Returns the metadata and the line index where the body starts.
func parseFrontmatter(lines []string) (map[string][]string, int) {
	meta := map[string][]string{}
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return meta, 0
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return meta, 0
	}
	key := ""
	for _, ln := range lines[1:end] {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.HasPrefix(t, "- ") && key != "" {
			meta[key] = append(meta[key], unquote(strings.TrimSpace(t[2:])))
			continue
		}
		i := strings.Index(t, ":")
		if i <= 0 {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(t[:i]))
		val := strings.TrimSpace(t[i+1:])
		if val == "" {
			meta[key] = nil
			continue
		}
		meta[key] = splitScalar(val)
	}
	return meta, end + 1
}

func splitScalar(v string) []string {
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
		var out []string
		for _, p := range strings.Split(v[1:len(v)-1], ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, unquote(p))
			}
		}
		return out
	}
	return []string{unquote(v)}
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func headingLevel(line string) (int, string) {
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || i >= len(line) || line[i] != ' ' {
		return 0, ""
	}
	return i, strings.TrimSpace(strings.TrimRight(line[i+1:], "#"))
}

// chunkBody splits markdown into one chunk per heading section. Fenced code is
// tracked so that '#' comments inside C++/Python snippets are not read as headings.
func chunkBody(lines []string, start int, title string) []draftChunk {
	var chunks []draftChunk
	var stack []string
	fence := ""
	bufLine := start + 1 // 1-based номер первой строки, попадающей в buf
	var buf []string

	emit := func() {
		crumb := title
		if len(stack) > 0 {
			crumb = strings.Join(stack, " > ")
		}
		for _, p := range splitSection(buf) {
			chunks = append(chunks, draftChunk{Breadcrumb: crumb, Line: bufLine + p.Off, Body: p.Text})
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
		if strings.HasPrefix(t, "```") {
			fence = "```"
			buf = append(buf, ln)
			continue
		}
		if strings.HasPrefix(t, "~~~") {
			fence = "~~~"
			buf = append(buf, ln)
			continue
		}
		if lvl, text := headingLevel(ln); lvl > 0 {
			emit()
			if lvl <= len(stack) {
				stack = stack[:lvl-1]
			}
			for len(stack) < lvl-1 {
				stack = append(stack, "")
			}
			stack = append(stack, text)
			bufLine = i + 2 // содержимое секции начинается со строки после заголовка
			continue
		}
		buf = append(buf, ln)
	}
	emit()
	return chunks
}

// part — кусок секции вместе со смещением в строках от её начала,
// чтобы номер строки в выдаче указывал на сам фрагмент, а не на заголовок.
type part struct {
	Off  int
	Text string
}

// splitSection дробит секцию, предпочитая границы абзацев. Если абзац сам по
// себе длиннее полутора лимитов — например, markdown-таблица или список без
// пустых строк, — режет по границе строки: иначе такие документы дают
// единственный гигантский фрагмент, который ломает и BM25, и сниппеты.
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
	if total <= maxChunkRunes {
		return []part{{Off: lo, Text: strings.Join(body, "\n")}}
	}

	var out []part
	var buf []string
	bufStart, n := 0, 0
	fence := ""
	flush := func() {
		if t := strings.TrimSpace(strings.Join(buf, "\n")); t != "" {
			out = append(out, part{Off: lo + bufStart, Text: t})
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
				continue // не начинаем фрагмент с пустой строки — номер должен вести на текст
			}
			bufStart = i
		}
		buf = append(buf, l)
		n += len([]rune(l)) + 1
		if n < maxChunkRunes || fence != "" {
			continue
		}
		atBreak := t == "" || i+1 >= len(body) || strings.TrimSpace(body[i+1]) == ""
		if atBreak || n >= maxChunkRunes*3/2 {
			flush()
		}
	}
	flush()
	return out
}

// outline returns the heading list and the first prose paragraph, used by digest.
func outline(lines []string, start int) ([]string, string) {
	var heads []string
	var leadBuf []string
	lead := ""
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
		if lvl, text := headingLevel(lines[i]); lvl > 0 {
			heads = append(heads, strings.Repeat("  ", lvl-1)+text)
			continue
		}
		// Заметки перенесены по ширине, поэтому абзац собирается до пустой
		// строки: иначе в дайджест попадала бы оборванная первая строка.
		if !leadDone && t != "" && !strings.HasPrefix(t, "|") && !strings.HasPrefix(t, ">") {
			leadBuf = append(leadBuf, t)
			continue
		}
		if len(leadBuf) > 0 && t == "" {
			leadDone = true
		}
	}
	lead = strings.Join(leadBuf, " ")
	if r := []rune(lead); len(r) > 400 {
		lead = strings.TrimSpace(string(r[:400])) + "…"
	}
	return heads, lead
}
