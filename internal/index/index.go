// Package index maintains the on-disk inverted index of a markdown corpus.
// Re-indexing is incremental: only files whose content hash changed are parsed.
package index

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Quette420/mdx_search/internal/markdown"
	"github.com/Quette420/mdx_search/internal/textproc"
)

type Chunk struct {
	ID         int
	Path       string
	Breadcrumb string
	Line       int
	Body       string
}

type FileRec struct {
	Path     string
	Hash     string
	ModTime  int64
	Title    string
	Meta     map[string][]string
	Headings []string
	Lead     string
	ChunkIDs []int
	Summary  string // filled by `mdx digest -summarize`
	SumHash  string // content hash the summary was produced from
}

type Posting struct {
	Chunk int32
	TF    int32
}

type Index struct {
	Root     string
	Files    map[string]*FileRec
	Chunks   map[int]*Chunk
	NextID   int
	Postings map[string][]Posting
	DocLen   map[int]int32
	AvgLen   float64

	// Deleted chunks leave stale postings behind; queries skip them via the
	// Chunks map, and a full rebuild runs once they exceed a quarter of the corpus.
	Dead     int
	TotalLen int64
}

// CrawlOptions controls which files are indexed.
type CrawlOptions struct {
	Extensions []string // e.g. ".md"; matched case-insensitively
	Exclude    []string // base-name globs or path substrings
}

// DefaultExtensions are indexed when CrawlOptions.Extensions is empty.
var DefaultExtensions = []string{".md", ".markdown"}

// Stats reports what a crawl changed.
type Stats struct {
	Added, Updated, Removed int
}

func New(root string) *Index {
	return &Index{
		Root:   root,
		Files:  map[string]*FileRec{},
		Chunks: map[int]*Chunk{},
		NextID: 1,
	}
}

var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "build": true, "dist": true,
	"obj": true, "bin": true, "target": true,
}

// Crawl walks root and re-parses only files whose content hash changed.
func (ix *Index) Crawl(root string, opt CrawlOptions) (Stats, error) {
	var st Stats
	exts := opt.Extensions
	if len(exts) == 0 {
		exts = DefaultExtensions
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if d.IsDir() {
			// The root itself is always walked, even if hidden (e.g. ~/.notes).
			if p != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !hasExt(p, exts) {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		rel = filepath.ToSlash(rel)
		if excluded(rel, opt.Exclude) {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		sum := sha256.Sum256(data)
		h := hex.EncodeToString(sum[:8])
		seen[rel] = true
		if old, ok := ix.Files[rel]; ok && old.Hash == h {
			return nil
		}
		if _, ok := ix.Files[rel]; ok {
			ix.dropFile(rel)
			st.Updated++
		} else {
			st.Added++
		}
		var mt int64
		if info, _ := d.Info(); info != nil {
			mt = info.ModTime().Unix()
		}
		ix.addFile(rel, h, mt, string(data))
		return nil
	})
	if err != nil {
		return st, err
	}
	for rel := range ix.Files {
		if !seen[rel] {
			ix.dropFile(rel)
			delete(ix.Files, rel)
			st.Removed++
		}
	}
	if ix.Postings == nil || ix.Dead*4 > len(ix.Chunks) {
		ix.buildPostings()
	} else if len(ix.Chunks) > 0 {
		ix.AvgLen = float64(ix.TotalLen) / float64(len(ix.Chunks))
	}
	return st, nil
}

func hasExt(p string, exts []string) bool {
	ext := filepath.Ext(p)
	for _, e := range exts {
		if strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

func excluded(rel string, patterns []string) bool {
	for _, ex := range patterns {
		if ok, _ := filepath.Match(ex, filepath.Base(rel)); ok {
			return true
		}
		if strings.Contains(rel, ex) {
			return true
		}
	}
	return false
}

func (ix *Index) dropFile(rel string) {
	f, ok := ix.Files[rel]
	if !ok {
		return
	}
	for _, id := range f.ChunkIDs {
		if _, live := ix.Chunks[id]; live {
			ix.Dead++
			ix.TotalLen -= int64(ix.DocLen[id])
			delete(ix.DocLen, id)
			delete(ix.Chunks, id)
		}
	}
	f.ChunkIDs = nil
}

func (ix *Index) addFile(rel, hash string, mtime int64, text string) {
	doc := markdown.Parse(rel, text)
	rec := &FileRec{
		Path: rel, Hash: hash, ModTime: mtime, Title: doc.Title,
		Meta: doc.Meta, Headings: doc.Headings, Lead: doc.Lead,
	}
	if prev := ix.Files[rel]; prev != nil {
		rec.Summary, rec.SumHash = prev.Summary, prev.SumHash
	}
	for _, s := range doc.Sections {
		c := &Chunk{ID: ix.NextID, Path: rel, Breadcrumb: s.Breadcrumb, Line: s.Line, Body: s.Body}
		ix.NextID++
		ix.Chunks[c.ID] = c
		if ix.Postings != nil { // otherwise a full build runs at the end of the crawl
			ix.indexChunk(c)
		}
		rec.ChunkIDs = append(rec.ChunkIDs, c.ID)
	}
	ix.Files[rel] = rec
}

// indexChunk appends postings for one chunk.
func (ix *Index) indexChunk(c *Chunk) {
	toks := textproc.Tokenize(c.Breadcrumb + "\n" + c.Path + "\n" + c.Body)
	tf := map[string]int32{}
	for _, t := range toks {
		tf[t]++
	}
	if ix.DocLen == nil {
		ix.DocLen = map[int]int32{}
	}
	ix.DocLen[c.ID] = int32(len(toks))
	ix.TotalLen += int64(len(toks))
	for t, n := range tf {
		ix.Postings[t] = append(ix.Postings[t], Posting{Chunk: int32(c.ID), TF: n})
	}
}

// buildPostings rebuilds the whole inverted index. For a notes-sized corpus this
// costs milliseconds and avoids incremental-deletion bugs entirely.
func (ix *Index) buildPostings() {
	ix.Postings = make(map[string][]Posting, len(ix.Chunks)*8)
	ix.DocLen = make(map[int]int32, len(ix.Chunks))
	ix.TotalLen = 0
	ids := make([]int, 0, len(ix.Chunks))
	for id := range ix.Chunks {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		ix.indexChunk(ix.Chunks[id])
	}
	ix.Dead = 0
	ix.AvgLen = 0
	if len(ix.Chunks) > 0 {
		ix.AvgLen = float64(ix.TotalLen) / float64(len(ix.Chunks))
	}
}
