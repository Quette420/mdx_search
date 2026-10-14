package cli

import (
	"flag"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Quette420/mdx_search/internal/index"
)

// common holds the flags shared by every command.
type common struct {
	root string
	db   string
}

func (c *common) register(fs *flag.FlagSet) {
	fs.StringVar(&c.root, "root", ".", "corpus root directory")
	fs.StringVar(&c.db, "db", "", "index file (default <root>/.mdx/index.gob)")
}

// resolve returns the absolute corpus root and the index path.
func (c *common) resolve() (root, db string, err error) {
	root, err = filepath.Abs(c.root)
	if err != nil {
		return "", "", err
	}
	db = c.db
	if db == "" {
		db = index.DefaultPath(root)
	}
	return root, db, nil
}

func (c *common) open() (*index.Index, string, error) {
	root, db, err := c.resolve()
	if err != nil {
		return nil, "", err
	}
	ix, err := index.Load(db)
	if err != nil {
		return nil, "", fmt.Errorf("no index at %s — run `mdx index` first", db)
	}
	return ix, root, nil
}

// reorder moves flags ahead of positional arguments. The standard flag.Parse
// stops at the first non-flag argument, so `mdx search query -k 5` would
// otherwise pull "-k 5" into the query text. Boolean flags are detected via
// IsBoolFlag so that they do not swallow the next argument.
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
			continue // -k=5, value is inline
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

// listFlag is a repeatable flag that also accepts comma-separated values.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*l = append(*l, p)
		}
	}
	return nil
}

// kvFlag is a repeatable key=value flag.
type kvFlag map[string]string

func (m kvFlag) String() string {
	var parts []string
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

func (m kvFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || strings.TrimSpace(k) == "" {
		return fmt.Errorf("expected key=value, got %q", v)
	}
	m[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(val)
	return nil
}

func parseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if len(s) >= 2 {
		if n, err := strconv.Atoi(s[:len(s)-1]); err == nil {
			switch s[len(s)-1] {
			case 'd':
				return now.AddDate(0, 0, -n), nil
			case 'w':
				return now.AddDate(0, 0, -7*n), nil
			case 'm':
				return now.AddDate(0, -n, 0), nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date %q (want 2026-01-15, 7d, 3w or 2m)", s)
}
