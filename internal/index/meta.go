package index

import (
	"strings"
	"time"
)

// MetaFirst returns the first value of a frontmatter field, or "".
func (f *FileRec) MetaFirst(key string) string {
	if v := f.Meta[strings.ToLower(key)]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// MetaHas reports whether a frontmatter field contains want (case-insensitive).
func (f *FileRec) MetaHas(key, want string) bool {
	for _, v := range f.Meta[strings.ToLower(key)] {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}

var dateLayouts = []string{"2006-01-02", time.RFC3339, "2006/01/02", "02.01.2006"}

// Date returns the note date from frontmatter (date, updated, created),
// falling back to the file modification time.
func (f *FileRec) Date() time.Time {
	for _, k := range []string{"date", "updated", "created"} {
		for _, v := range f.Meta[k] {
			for _, layout := range dateLayouts {
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
