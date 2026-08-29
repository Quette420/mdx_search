package main

import (
	"regexp"
	"strings"
	"unicode"
)

var hexRe = regexp.MustCompile(`^0[xX][0-9a-fA-F]+$`)

// identLike reports whether a query term looks like a symbol or an address,
// in which case exact substring matching should outrank fuzzy BM25 hits.
func identLike(s string) bool {
	if hexRe.MatchString(s) {
		return true
	}
	if strings.ContainsAny(s, "_:") {
		return true
	}
	hasUpper, hasLower := false, false
	for _, r := range s {
		if unicode.IsUpper(r) {
			hasUpper = true
		}
		if unicode.IsLower(r) {
			hasLower = true
		}
	}
	return hasUpper && hasLower && len(s) > 3
}

// tokenize splits text into search terms. Letters (any script), digits and '_'
// form a raw token; each raw token is then expanded into its sub-words so that
// "stReplicatedStateFSM" is findable as "replicated", "state" or "fsm".
func tokenize(s string) []string {
	var out []string
	cur := make([]rune, 0, 32)
	flush := func() {
		if len(cur) > 0 {
			out = append(out, expand(string(cur))...)
			cur = cur[:0]
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			cur = append(cur, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

func expand(tok string) []string {
	low := strings.ToLower(tok)
	res := []string{low}
	// 0xF90410 stays intact, but also matches a bare "F90410" query.
	if hexRe.MatchString(tok) {
		return append(res, low[2:])
	}
	for _, part := range strings.Split(tok, "_") {
		if part == "" {
			continue
		}
		for _, w := range camelSplit(part) {
			lw := strings.ToLower(w)
			if lw != low && len([]rune(lw)) > 1 {
				res = append(res, lw)
			}
		}
	}
	// Morphological variants: same rules run at index and query time, so
	// "гипотезы" reaches "Гипотеза" and "replicated" reaches "replication".
	for _, w := range append([]string{}, res...) {
		if s := stem(w); s != w {
			res = append(res, s)
		}
		if p := prefixKey(w); p != "" {
			res = append(res, p)
		}
	}
	return dedupe(res)
}

const prefixLen = 6

// prefixKey is a deliberately coarse recall net for long inflected words that
// suffix stripping alone does not unify.
func prefixKey(w string) string {
	rs := []rune(w)
	if len(rs) < prefixLen+2 {
		return ""
	}
	for _, r := range rs {
		if unicode.IsDigit(r) {
			return ""
		}
	}
	return string(rs[:prefixLen])
}

var ruSuffixes = []string{
	"иями", "ями", "ами", "ого", "ему", "ыми", "ими", "ому", "ений", "ение", "ения",
	"ой", "ей", "ая", "ое", "ые", "ый", "ий", "ом", "ах", "ях", "ов", "ев",
	"ам", "ям", "ую", "юю", "ем", "им", "ых", "их", "ся", "ть",
	"а", "я", "ы", "и", "о", "е", "у", "ю", "й", "ь",
}

var enSuffixes = []string{"ings", "ing", "ies", "ied", "es", "ed", "s"}

// stem strips a common inflectional ending, never cutting below four runes.
func stem(w string) string {
	rs := []rune(w)
	if len(rs) < 5 {
		return w
	}
	list := enSuffixes
	if rs[0] >= 'а' && rs[0] <= 'я' || rs[0] == 'ё' {
		list = ruSuffixes
	}
	for _, suf := range list {
		sr := []rune(suf)
		if len(rs)-len(sr) >= 4 && strings.HasSuffix(w, suf) {
			return string(rs[:len(rs)-len(sr)])
		}
	}
	return w
}

// camelSplit breaks on lower->upper, ACRONYMWord and letter<->digit boundaries.
func camelSplit(s string) []string {
	rs := []rune(s)
	if len(rs) < 2 {
		return []string{s}
	}
	var out []string
	start := 0
	for i := 1; i < len(rs); i++ {
		p, c := rs[i-1], rs[i]
		b := false
		switch {
		case unicode.IsLower(p) && unicode.IsUpper(c):
			b = true
		case unicode.IsUpper(p) && unicode.IsUpper(c) && i+1 < len(rs) && unicode.IsLower(rs[i+1]):
			b = true
		case unicode.IsDigit(p) != unicode.IsDigit(c):
			b = true
		}
		if b {
			out = append(out, string(rs[start:i]))
			start = i
		}
	}
	return append(out, string(rs[start:]))
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
