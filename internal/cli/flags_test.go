package cli

import (
	"flag"
	"slices"
	"testing"
	"time"
)

func TestReorderMovesFlagsFirst(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.Int("k", 10, "")
	fs.Bool("full", false, "")
	got := reorder(fs, []string{"some", "query", "-k", "5", "-full", "tail"})
	want := []string{"-k", "5", "-full", "some", "query", "tail"}
	if !slices.Equal(got, want) {
		t.Errorf("reorder = %q, want %q", got, want)
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"":           {},
		"2026-01-15": time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		"7d":         now.AddDate(0, 0, -7),
		"2w":         now.AddDate(0, 0, -14),
		"1m":         now.AddDate(0, -1, 0),
	}
	for in, want := range cases {
		got, err := parseSince(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseSince(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"d", "yesterday", "5y"} {
		if _, err := parseSince(bad, now); err == nil {
			t.Errorf("parseSince(%q) succeeded", bad)
		}
	}
}

func TestKVFlag(t *testing.T) {
	m := kvFlag{}
	if err := m.Set("Status = open"); err != nil || m["status"] != "open" {
		t.Errorf("kvFlag = %v, %v", m, err)
	}
	if err := m.Set("novalue"); err == nil {
		t.Error("expected error for missing '='")
	}
}
