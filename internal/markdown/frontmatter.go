// Package markdown parses notes: a minimal YAML frontmatter reader, a
// heading-aware chunker and an outline extractor. It is deliberately small and
// dependency-free; it does not try to be a CommonMark implementation.
package markdown

import "strings"

// ParseFrontmatter reads a minimal YAML subset: scalars, inline [a, b] lists
// and dash lists. Keys are lower-cased. Returns the metadata and the line
// index where the body starts.
func ParseFrontmatter(lines []string) (map[string][]string, int) {
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
