package lsofutil

import (
	"os"
	"os/exec"
	"path/filepath"
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

func TestLockedSet_RealLsofIntegration(t *testing.T) {
	// On macOS lsof exits 1 if *any* argument file isn't open, even when
	// other arguments matched. Make sure we still surface the matches.
	// We use /dev/null (a file that always exists) and the lsof binary
	// itself (almost certainly NOT open by anything), guaranteeing the
	// "mixed match + non-match" condition.
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof not on PATH")
	}
	// Find an always-open path: lsof on `os.Executable()` won't help
	// because the test binary isn't held open via fd in the usual sense.
	// Instead, open a temp file ourselves and pass it.
	tmp, err := os.CreateTemp("", "lockedset-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close() // keep it open through the test

	// macOS /var/folders is a symlink to /private/var/folders; lsof
	// reports the resolved path, so normalize both sides for the lookup.
	openPath, err := filepath.EvalSymlinks(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}

	bogus := filepath.Join(t.TempDir(), "definitely-not-open.txt")
	if err := os.WriteFile(bogus, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LockedSet([]string{openPath, bogus})
	if err != nil {
		t.Fatalf("LockedSet errored despite partial match: %v", err)
	}
	if !got[openPath] {
		t.Errorf("expected %s to be reported as locked, got %v", openPath, got)
	}
	if got[bogus] {
		t.Errorf("did not expect %s to be reported as locked", bogus)
	}
}

// (parseLockedNames-only filter test removed; covered by TestParseLockedNames
// and the integration test above.)

