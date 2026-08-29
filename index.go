package main

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	Summary  string // filled by `digest --summarize`
	SumHash  string // hash the summary was produced from
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

func NewIndex(root string) *Index {
	return &Index{
		Root:   root,
		Files:  map[string]*FileRec{},
		Chunks: map[int]*Chunk{},
		NextID: 1,
	}
}

var skipDirs = map[string]bool{
	".git": true, ".svn": true, "node_modules": true, ".mdx": true,
	"vendor": true, "build": true, "obj": true, "bin": true,
}

// Crawl re-parses only files whose content hash changed. Returns counts of
// added, updated and removed files.
func (ix *Index) Crawl(root string, exclude []string) (add, upd, del int, err error) {
	seen := map[string]bool{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if d.IsDir() {
			// p != root: корень обходим всегда, даже если он сам скрытый
			// (напр. индексируем ~/.notes) — иначе обход завершится сразу.
			if p != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		rel = filepath.ToSlash(rel)
		for _, ex := range exclude {
			if ok, _ := filepath.Match(ex, filepath.Base(rel)); ok {
				return nil
			}
			if strings.Contains(rel, ex) {
				return nil
			}
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
			upd++
		} else {
			add++
		}
		st, _ := d.Info()
		var mt int64
		if st != nil {
			mt = st.ModTime().Unix()
		}
		ix.addFile(rel, h, mt, string(data))
		return nil
	})
	if err != nil {
		return
	}
	for rel := range ix.Files {
		if !seen[rel] {
			ix.dropFile(rel)
			delete(ix.Files, rel)
			del++
		}
	}
	if ix.Postings == nil || ix.Dead*4 > len(ix.Chunks) {
		ix.buildPostings()
	} else if len(ix.Chunks) > 0 {
		ix.AvgLen = float64(ix.TotalLen) / float64(len(ix.Chunks))
	}
	return
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

// indexChunk appends postings for one new chunk.
func (ix *Index) indexChunk(c *Chunk) {
	if ix.Postings == nil {
		return // a full build will run at the end of the crawl
	}
	tf := map[string]int32{}
	toks := tokenize(c.Breadcrumb + "\n" + c.Path + "\n" + c.Body)
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

func (ix *Index) addFile(rel, hash string, mtime int64, text string) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	meta, start := parseFrontmatter(lines)

	title := ""
	if v := meta["title"]; len(v) > 0 {
		title = v[0]
	}
	if title == "" {
		for i := start; i < len(lines); i++ {
			if lvl, t := headingLevel(lines[i]); lvl == 1 {
				title = t
				break
			}
		}
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	}

	heads, lead := outline(lines, start)
	prev := ix.Files[rel]
	rec := &FileRec{
		Path: rel, Hash: hash, ModTime: mtime, Title: title,
		Meta: meta, Headings: heads, Lead: lead,
	}
	if prev != nil {
		rec.Summary, rec.SumHash = prev.Summary, prev.SumHash
	}
	for _, d := range chunkBody(lines, start, title) {
		c := &Chunk{ID: ix.NextID, Path: rel, Breadcrumb: d.Breadcrumb, Line: d.Line, Body: d.Body}
		ix.NextID++
		ix.Chunks[c.ID] = c
		ix.indexChunk(c)
		rec.ChunkIDs = append(rec.ChunkIDs, c.ID)
	}
	ix.Files[rel] = rec
}

// buildPostings rebuilds the whole inverted index. For a notes-sized corpus this
// costs milliseconds and avoids incremental-deletion bugs entirely.
func (ix *Index) buildPostings() {
	ix.Postings = make(map[string][]Posting, len(ix.Chunks)*8)
	ix.DocLen = make(map[int]int32, len(ix.Chunks))
	total := 0
	ids := make([]int, 0, len(ix.Chunks))
	for id := range ix.Chunks {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		c := ix.Chunks[id]
		tf := map[string]int32{}
		toks := tokenize(c.Breadcrumb + "\n" + c.Path + "\n" + c.Body)
		for _, t := range toks {
			tf[t]++
		}
		ix.DocLen[id] = int32(len(toks))
		total += len(toks)
		for t, n := range tf {
			ix.Postings[t] = append(ix.Postings[t], Posting{Chunk: int32(id), TF: n})
		}
	}
	ix.TotalLen = int64(total)
	ix.Dead = 0
	if len(ix.Chunks) > 0 {
		ix.AvgLen = float64(total) / float64(len(ix.Chunks))
	}
}

func Load(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ix := &Index{}
	if err := gob.NewDecoder(f).Decode(ix); err != nil {
		return nil, err
	}
	if ix.Files == nil {
		ix.Files = map[string]*FileRec{}
	}
	if ix.Chunks == nil {
		ix.Chunks = map[int]*Chunk{}
	}
	return ix, nil
}

func (ix *Index) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(ix); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
