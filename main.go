package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const usageText = `mdx — поиск и обзор по корпусу .md заметок.

  mdx index  [-root .] [-db .mdx/index.gob] [-exclude glob]...
  mdx search "запрос" [-k 10] [-subsystem s] [-status s] [-path p] [-since d] [-full]
  mdx digest [-out INDEX.md] [-since d] [-group subsystem] [-summarize] [-model m] [-limit n]
  mdx stats

  -since принимает 2026-01-15, 7d, 3w или 2m.
  Переиндексация инкрементальная: перечитываются только изменённые файлы.
`

func usage() { fmt.Fprint(os.Stderr, usageText) }

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "index":
		err = cmdIndex(os.Args[2:])
	case "search", "s":
		err = cmdSearch(os.Args[2:])
	case "digest":
		err = cmdDigest(os.Args[2:])
	case "stats":
		err = cmdStats(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mdx:", err)
		os.Exit(1)
	}
}

// reorder переставляет флаги вперёд позиционных аргументов.
// Штатный flag.Parse останавливается на первом непохожем на флаг аргументе,
// из-за чего `mdx search запрос -k 5` втягивал бы "-k 5" в текст запроса.
// Булевы флаги распознаются через IsBoolFlag, чтобы не съесть следующий аргумент.
func reorder(fs *flag.FlagSet, args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.ContainsRune(name, '=') {
			continue // -k=5, значение уже внутри
		}
		f := fs.Lookup(name)
		if f == nil {
			continue
		}
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, pos...)
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func parseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if n, err := strconv.Atoi(s[:len(s)-1]); err == nil {
		switch s[len(s)-1] {
		case 'd':
			return time.Now().AddDate(0, 0, -n), nil
		case 'w':
			return time.Now().AddDate(0, 0, -7*n), nil
		case 'm':
			return time.Now().AddDate(0, -n, 0), nil
		}
	}
	return time.Time{}, fmt.Errorf("не понимаю дату %q", s)
}

func openIndex(db string) (*Index, error) {
	ix, err := Load(db)
	if err != nil {
		return nil, fmt.Errorf("нет индекса (%s) — сначала `mdx index`", db)
	}
	return ix, nil
}

func cmdIndex(args []string) error {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	root := fs.String("root", ".", "корень корпуса")
	db := fs.String("db", "", "путь к индексу")
	var ex multiFlag
	fs.Var(&ex, "exclude", "исключить путь/глоб (можно несколько)")
	fs.Parse(reorder(fs, args))

	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	path := *db
	if path == "" {
		path = filepath.Join(abs, ".mdx", "index.gob")
	}
	ix, err := Load(path)
	if err != nil {
		ix = NewIndex(abs)
	}
	ix.Root = abs
	ex = append(ex, "INDEX.md")

	t0 := time.Now()
	add, upd, del, err := ix.Crawl(abs, ex)
	if err != nil {
		return err
	}
	if err := ix.Save(path); err != nil {
		return err
	}
	fmt.Printf("проиндексировано: +%d ~%d -%d · всего %d файлов, %d фрагментов, %d термов · %s\n",
		add, upd, del, len(ix.Files), len(ix.Chunks), len(ix.Postings), time.Since(t0).Round(time.Millisecond))
	return nil
}

func cmdSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	db := fs.String("db", "", "путь к индексу")
	root := fs.String("root", ".", "корень корпуса")
	k := fs.Int("k", 10, "сколько результатов")
	sub := fs.String("subsystem", "", "фильтр по frontmatter subsystem")
	st := fs.String("status", "", "фильтр по frontmatter status")
	pth := fs.String("path", "", "фильтр по подстроке пути")
	since := fs.String("since", "", "только заметки новее даты")
	full := fs.Bool("full", false, "печатать фрагмент целиком")
	fs.Parse(reorder(fs, args))

	q := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if q == "" {
		return fmt.Errorf("пустой запрос")
	}
	abs, _ := filepath.Abs(*root)
	path := *db
	if path == "" {
		path = filepath.Join(abs, ".mdx", "index.gob")
	}
	ix, err := openIndex(path)
	if err != nil {
		return err
	}
	sinceT, err := parseSince(*since)
	if err != nil {
		return err
	}

	res := ix.Search(q, Filters{Subsystem: *sub, Status: *st, PathSub: *pth, Since: sinceT}, *k)
	if len(res) == 0 {
		fmt.Println("ничего не найдено")
		return nil
	}
	for i, r := range res {
		mark := ""
		if r.Exact {
			mark = " ="
		}
		fmt.Printf("\n%d. %s:%d%s\n", i+1, r.Chunk.Path, r.Chunk.Line, mark)
		if r.Chunk.Breadcrumb != "" {
			fmt.Printf("   %s\n", r.Chunk.Breadcrumb)
		}
		if meta := metaLine(r.File); meta != "" {
			fmt.Printf("   %s\n", meta)
		}
		if *full {
			for _, ln := range strings.Split(r.Chunk.Body, "\n") {
				fmt.Printf("   | %s\n", ln)
			}
		} else {
			fmt.Printf("   %s\n", r.Snippet)
		}
	}
	fmt.Println()
	return nil
}

func metaLine(f *FileRec) string {
	if f == nil {
		return ""
	}
	var parts []string
	for _, k := range []string{"subsystem", "status"} {
		if v := metaFirst(f.Meta, k); v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, " ")
}

func cmdDigest(args []string) error {
	fs := flag.NewFlagSet("digest", flag.ExitOnError)
	db := fs.String("db", "", "путь к индексу")
	root := fs.String("root", ".", "корень корпуса")
	out := fs.String("out", "INDEX.md", "куда писать (- = stdout)")
	since := fs.String("since", "", "только заметки новее даты")
	group := fs.String("group", "subsystem", "поле frontmatter для группировки")
	sum := fs.Bool("summarize", false, "сгенерировать резюме через Anthropic API")
	model := fs.String("model", "claude-sonnet-5", "модель для -summarize")
	limit := fs.Int("limit", 0, "максимум файлов на резюме за запуск")
	fs.Parse(reorder(fs, args))

	abs, _ := filepath.Abs(*root)
	path := *db
	if path == "" {
		path = filepath.Join(abs, ".mdx", "index.gob")
	}
	ix, err := openIndex(path)
	if err != nil {
		return err
	}
	sinceT, err := parseSince(*since)
	if err != nil {
		return err
	}
	if *sum {
		n, err := ix.Summarize(*model, *limit, true)
		if n > 0 {
			if serr := ix.Save(path); serr != nil {
				return serr
			}
			fmt.Fprintf(os.Stderr, "резюме обновлено: %d\n", n)
		}
		if err != nil {
			return err
		}
	}
	text := ix.BuildDigest(sinceT, *group)
	if *out == "-" {
		fmt.Print(text)
		return nil
	}
	dst := *out
	if !filepath.IsAbs(dst) {
		dst = filepath.Join(abs, dst)
	}
	if err := os.WriteFile(dst, []byte(text), 0o644); err != nil {
		return err
	}
	fmt.Printf("записано %s\n", dst)
	return nil
}

func cmdStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	db := fs.String("db", "", "путь к индексу")
	root := fs.String("root", ".", "корень корпуса")
	fs.Parse(reorder(fs, args))

	abs, _ := filepath.Abs(*root)
	path := *db
	if path == "" {
		path = filepath.Join(abs, ".mdx", "index.gob")
	}
	ix, err := openIndex(path)
	if err != nil {
		return err
	}
	fmt.Printf("файлов %d · фрагментов %d · термов %d · средняя длина %.0f\n",
		len(ix.Files), len(ix.Chunks), len(ix.Postings), ix.AvgLen)

	for _, key := range []string{"subsystem", "status"} {
		counts := map[string]int{}
		miss := 0
		for _, f := range ix.Files {
			v := metaFirst(f.Meta, key)
			if v == "" {
				miss++
				continue
			}
			counts[v]++
		}
		if len(counts) == 0 {
			continue
		}
		keys := make([]string, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return counts[keys[i]] > counts[keys[j]] })
		fmt.Printf("\n%s:\n", key)
		for _, k := range keys {
			fmt.Printf("  %-24s %d\n", k, counts[k])
		}
		if miss > 0 {
			fmt.Printf("  %-24s %d\n", "(не указан)", miss)
		}
	}
	return nil
}
