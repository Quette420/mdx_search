// Package search ranks index chunks for a query by fusing BM25 with a literal
// substring pass via reciprocal rank fusion (RRF).
package search

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Quette420/go_rag/internal/index"
	"github.com/Quette420/go_rag/internal/textproc"
)

const (
	bmK1 = 1.2
	bmB  = 0.75
	rrfK = 60.0
)

// Filters restrict results. All set conditions must hold.
type Filters struct {
	Meta    map[string]string // frontmatter key -> required value (case-insensitive)
	PathSub string            // substring of the file path
	Since   time.Time         // note date not older than this
}

type Result struct {
	Chunk   *index.Chunk
	File    *index.FileRec
	Score   float64
	Snippet string
	Exact   bool // found by the literal pass
}

func (ft Filters) match(f *index.FileRec) bool {
	// Orphaned chunk: the file record is gone but the chunk is still alive.
	if f == nil {
		return false
	}
	if ft.PathSub != "" && !strings.Contains(strings.ToLower(f.Path), strings.ToLower(ft.PathSub)) {
		return false
	}
	for k, v := range ft.Meta {
		if !f.MetaHas(k, v) {
			return false
		}
	}
	if !ft.Since.IsZero() {
		t := f.Date()
		if t.IsZero() || t.Before(ft.Since) {
			return false
		}
	}
	return true
}

// Search fuses two rankings with reciprocal rank fusion: BM25 over the inverted
// index, and a literal substring pass that keeps hex offsets and symbol names
// from being drowned out by their lexical neighbours. k <= 0 means no limit.
func Search(ix *index.Index, query string, ft Filters, k int) []Result {
	qTokens := textproc.Dedupe(textproc.Tokenize(query))
	if len(qTokens) == 0 {
		return nil
	}

	bm := bm25(ix, qTokens, ft)
	ex := exact(ix, query, ft)

	rank := map[int]float64{}
	for i, id := range bm {
		rank[id] += 1.0 / (rrfK + float64(i+1))
	}
	exactSet := map[int]bool{}
	for i, id := range ex {
		rank[id] += 1.0 / (rrfK + float64(i+1))
		exactSet[id] = true
	}

	ids := sortByScore(rank)
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
			Snippet: Snippet(c.Body, query, qTokens),
			Exact:   exactSet[id],
		})
	}
	return out
}

func bm25(ix *index.Index, qTokens []string, ft Filters) []int {
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
			if c == nil || !ft.match(ix.Files[c.Path]) {
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
// Linear over the candidate set, which is fine at notes scale and exactly right
// for queries such as 0xF90410 or AllocateChannelSequence.
func exact(ix *index.Index, query string, ft Filters) []int {
	var terms []string
	if q := strings.TrimSpace(query); len(q) > 2 && strings.ContainsAny(q, " ") {
		terms = append(terms, q)
	}
	for _, f := range strings.FieldsFunc(query, func(r rune) bool {
		return r == ' ' || r == ',' || r == '"' || r == '\''
	}) {
		if textproc.IdentLike(f) || len(f) > 5 {
			terms = append(terms, f)
		}
	}
	if len(terms) == 0 {
		return nil
	}
	score := map[int]float64{}
	for _, t := range terms {
		for _, id := range candidates(ix, t) {
			c := ix.Chunks[id]
			if c == nil || !ft.match(ix.Files[c.Path]) {
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
func candidates(ix *index.Index, term string) []int {
	toks := textproc.Dedupe(textproc.Tokenize(term))
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

// countFold counts case-insensitive occurrences without allocating. ASCII and
// Cyrillic both keep their byte length across case, so a fixed-width window is safe.
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
