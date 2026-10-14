package search

import "strings"

const snipLen = 260

// Snippet returns a one-line excerpt of body around the first match of the
// query (or, failing that, of any query token).
func Snippet(body, query string, toks []string) string {
	flat := strings.Join(strings.Fields(body), " ")
	rs := []rune(flat)
	// Search in runes: strings.Index returns a byte offset while the window is
	// cut in runes, and on Cyrillic those scales differ by a factor of two.
	lowRunes := []rune(strings.ToLower(flat))
	pos := -1
	if q := strings.ToLower(strings.TrimSpace(query)); q != "" {
		pos = runeIndex(lowRunes, []rune(q))
	}
	if pos < 0 {
		for _, t := range toks {
			if p := runeIndex(lowRunes, []rune(t)); p >= 0 {
				pos = p
				break
			}
		}
	}
	if pos < 0 {
		pos = 0
	}
	start := max(pos-60, 0)
	start = min(start, len(rs)) // ToLower may change rune count in exotic scripts
	end := min(start+snipLen, len(rs))
	s := strings.TrimSpace(string(rs[start:end]))
	if start > 0 {
		s = "…" + s
	}
	if end < len(rs) {
		s += "…"
	}
	return s
}

// runeIndex is strings.Index with both input and output measured in runes.
func runeIndex(hay, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(hay) {
		return -1
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		ok := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}
