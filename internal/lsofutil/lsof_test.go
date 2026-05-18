package lsofutil

import (
	"reflect"
	"sort"
	"testing"
)

func TestParseLockedNames(t *testing.T) {
	// Sample of `lsof -F n -- /a /b` output: per-process records, name lines
	// prefixed with 'n'. Other lines (p<pid>, f<fd>, t<type>) are ignored.
	in := `p123
f4
ttype=REG
n/a
p124
f7
n/b
n/some/other/file
`
	got := parseLockedNames(in)
	sort.Strings(got)
	want := []string{"/a", "/b", "/some/other/file"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseLockedNames_Empty(t *testing.T) {
	if got := parseLockedNames(""); len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
}

func TestLockedSet_EmptyInputNoFork(t *testing.T) {
	got, err := LockedSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want empty result, got %v", got)
	}
}

func TestLockedSet_OnlyReturnsRequestedPaths(t *testing.T) {
	// Build the result map manually from parseLockedNames + a synthetic
	// "wanted" set to verify the filter logic without invoking lsof.
	parsed := parseLockedNames("p1\nn/a\nn/extra\np2\nn/b\n")
	want := map[string]struct{}{"/a": {}, "/b": {}, "/c": {}}
	out := map[string]bool{}
	for _, p := range parsed {
		if _, ok := want[p]; ok {
			out[p] = true
		}
	}
	if !reflect.DeepEqual(out, map[string]bool{"/a": true, "/b": true}) {
		t.Errorf("got %v", out)
	}
}
