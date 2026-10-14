package textproc

import (
	"slices"
	"testing"
)

func TestTokenizeSplitsIdentifiers(t *testing.T) {
	cases := map[string][]string{
		"stReplicatedStateFSM": {"streplicatedstatefsm", "replicated", "state", "fsm"},
		"snake_case_name":      {"snake_case_name", "snake", "case", "name"},
		"HTTPServer2":          {"httpserver2", "http", "server"},
		"0xF90410":             {"0xf90410", "f90410"},
	}
	for in, want := range cases {
		got := Tokenize(in)
		for _, w := range want {
			if !slices.Contains(got, w) {
				t.Errorf("Tokenize(%q) = %v, missing %q", in, got, w)
			}
		}
	}
}

func TestStemUnifiesInflections(t *testing.T) {
	pairs := [][2]string{
		{"гипотезы", "гипотеза"},
		{"replicated", "replicates"},
	}
	for _, p := range pairs {
		a, b := Tokenize(p[0]), Tokenize(p[1])
		shared := false
		for _, x := range a {
			if slices.Contains(b, x) {
				shared = true
				break
			}
		}
		if !shared {
			t.Errorf("%q %v and %q %v share no term", p[0], a, p[1], b)
		}
	}
}

func TestIdentLike(t *testing.T) {
	for _, s := range []string{"0xDEAD", "AllocateChannel", "snake_case", "ns::fn"} {
		if !IdentLike(s) {
			t.Errorf("IdentLike(%q) = false", s)
		}
	}
	for _, s := range []string{"hello", "Go", "канал"} {
		if IdentLike(s) {
			t.Errorf("IdentLike(%q) = true", s)
		}
	}
}
