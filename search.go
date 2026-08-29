package main

import (
	"math"
	"sort"
	"strings"
	"time"
)

const (
	bmK1    = 1.2
	bmB     = 0.75
	rrfK    = 60.0
	snipLen = 260
)

type Filters struct {
	Subsystem string
	Status    string
	PathSub   string
	Since     time.Time
}

type Result struct {
	Chunk   *Chunk
	File    *FileRec
	Score   float64
	Snippet string
	Exact   bool
}

func (ix *Index) match(f *FileRec, ft Filters) bool {
	// Осиротевший фрагмент: запись о файле удалена, чанк ещё жив.
	// Без этой проверки любой фильтр падал бы на разыменовании nil.
	if f == nil {
		return false
	}
	if ft.PathSub != "" && !strings.Contains(strings.ToLower(f.Path), strings.ToLower(ft.PathSub)) {
		return false
	}
	if ft.Subsystem != "" && !metaHas(f.Meta, "subsystem", ft.Subsystem) {
		return false
	}
	if ft.Status != "" && !metaHas(f.Meta, "status", ft.Status) {
		return false
	}
	if !ft.Since.IsZero() {
		t := fileDate(f)
		if t.IsZero() || t.Before(ft.Since) {
			return false
		}
	}
	return true
}

func metaHas(m map[string][]string, key, want string) bool {
	for _, v := range m[key] {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}

func fileDate(f *FileRec) time.Time {
	for _, k := range []string{"date", "updated", "created"} {
		for _, v := range f.Meta[k] {
			for _, layout := range []string{"2006-01-02", time.RFC3339, "2006/01/02", "02.01.2006"} {
				if t, err := time.Parse(layout, strings.TrimSpace(v)); err == nil {
					return t
				}
			}
		}
	}
	if f.ModTime > 0 {
		return time.Unix(f.ModTime, 0)
	}
	return time.Time{}
}

// Search fuses two rankings with reciprocal rank fusion: BM25 over the inverted
// index, and a literal substring pass that keeps hex offsets and symbol names
// from being drowned out by their lexical neighbours.
func (ix *Index) Search(query string, ft Filters, k int) []Result {
	qTokens := dedupe(tokenize(query))
	if len(qTokens) == 0 {
		return nil
	}

	bm := ix.bm25(qTokens, ft)
	ex := ix.exact(query, ft)

	rank := map[int]float64{}
	for i, id := range bm {
		rank[id] += 1.0 / (rrfK + float64(i+1))
	}
	exactSet := map[int]bool{}
	for i, id := range ex {
		rank[id] += 1.0 / (rrfK + float64(i+1))
		exactSet[id] = true
	}

	ids := make([]int, 0, len(rank))
	for id := range rank {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool {
		if rank[ids[a]] != rank[ids[b]] {
			return rank[ids[a]] > rank[ids[b]]
		}
		return ids[a] < ids[b]
	})
	if k > 0 && len(ids) > k {
		ids = ids[:k]
	}

	out := make([]Result, 0, len(ids))
	for _, id := range ids {
		c := ix.Chunks[id]
		out = append(out, Result{
			Chunk:   c,
			File:    ix.Files[c.Path],
			Score:   rank[id],
			Snippet: snippet(c.Body, query, qTokens),
			Exact:   exactSet[id],
		})
	}
	return out
}

func (ix *Index) bm25(qTokens []string, ft Filters) []int {
	n := float64(len(ix.Chunks))
	if n == 0 {
		return nil
	}
	score := map[int]float64{}
	for _, t := range qTokens {
		pl := ix.Postings[t]
		if len(pl) == 0 {
			continue
		}
		df := float64(len(pl))
		idf := math.Log(1 + (n-df+0.5)/(df+0.5))
		for _, p := range pl {
			id := int(p.Chunk)
			c := ix.Chunks[id]
			if c == nil {
				continue
			}
			if !ix.match(ix.Files[c.Path], ft) {
				continue
			}
			dl := float64(ix.DocLen[id])
			tf := float64(p.TF)
			score[id] += idf * (tf * (bmK1 + 1)) / (tf + bmK1*(1-bmB+bmB*dl/ix.AvgLen))
		}
	}
	return sortByScore(score)
}

// exact scans chunk bodies for the literal query and for identifier-like terms.
// Linear over the corpus, which is fine at notes scale and exactly right for
// queries such as 0xF90410 or AllocateChannelSequence.
func (ix *Index) exact(query string, ft Filters) []int {
	var terms []string
	if q := strings.TrimSpace(query); len(q) > 2 && strings.ContainsAny(q, " ") {
		terms = append(terms, q)
	}
	for _, f := range strings.FieldsFunc(query, func(r rune) bool {
		return r == ' ' || r == ',' || r == '"' || r == '\''
	}) {
		if identLike(f) || len(f) > 5 {
			terms = append(terms, f)
		}
	}
	if len(terms) == 0 {
		return nil
	}
	score := map[int]float64{}
	for _, t := range terms {
		for _, id := range ix.candidates(t) {
			c := ix.Chunks[id]
			if c == nil || !ix.match(ix.Files[c.Path], ft) {
				continue
			}
			n := countFold(c.Body, t) + countFold(c.Breadcrumb, t) + countFold(c.Path, t)
			if n > 0 {
				score[id] += float64(n) * float64(len(t))
			}
		}
	}
	return sortByScore(score)
}

// candidates narrows the literal scan to chunks that contain the term's rarest
// token. A chunk holding the literal string necessarily holds all of its tokens,
// so this is exact, not an approximation.
func (ix *Index) candidates(term string) []int {
	toks := dedupe(tokenize(term))
	if len(toks) == 0 {
		return nil
	}
	best := ""
	bestN := -1
	for _, t := range toks {
		pl, ok := ix.Postings[t]
		if !ok {
			return nil // a token is absent, so the literal cannot occur
		}
		if bestN < 0 || len(pl) < bestN {
			best, bestN = t, len(pl)
		}
	}
	pl := ix.Postings[best]
	out := make([]int, 0, len(pl))
	for _, p := range pl {
		out = append(out, int(p.Chunk))
	}
	return out
}

// countFold counts case-insensitive occurrences without allocating. Both ASCII
// and Cyrillic keep their byte length across case, so a fixed-width window is safe.
func countFold(hay, needle string) int {
	n := len(needle)
	if n == 0 || len(hay) < n {
		return 0
	}
	count := 0
	for i := 0; i+n <= len(hay); i++ {
		if strings.EqualFold(hay[i:i+n], needle) {
			count++
			i += n - 1
		}
	}
	return count
}

func sortByScore(m map[int]float64) []int {
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool {
		if m[ids[a]] != m[ids[b]] {
			return m[ids[a]] > m[ids[b]]
		}
		return ids[a] < ids[b]
	})
	return ids
}

func snippet(body, query string, toks []string) string {
	flat := strings.Join(strings.Fields(body), " ")
	rs := []rune(flat)
	// Ищем в рунном срезе: strings.Index вернул бы байтовое смещение,
	// а окно режется по рунам — на кириллице эти шкалы расходятся вдвое.
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
	start := pos - 60
	if start < 0 {
		start = 0
	}
	if start > len(rs) {
		start = len(rs) // ToLower может изменить длину в рунах на экзотических письменностях
	}
	end := start + snipLen
	if end > len(rs) {
		end = len(rs)
	}
	s := strings.TrimSpace(string(rs[start:end]))
	if start > 0 {
		s = "…" + s
	}
	if end < len(rs) {
		s += "…"
	}
	return s
}

// runeIndex — аналог strings.Index, но и вход, и выход в рунах.
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
