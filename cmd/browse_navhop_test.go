package cmd

import (
	"strings"
	"testing"
)

func TestEnterBinding_PersistOmitsAccept(t *testing.T) {
	t.Setenv("TMUX", "/tmp/x,1,0") // force tmux.InTmux() true
	got := enterBinding("/bin/tsession", true)
	if strings.Contains(got, "+accept") {
		t.Fatalf("persist binding must not accept (keep fzf alive): %q", got)
	}
	if !strings.Contains(got, "execute-silent(") || !strings.Contains(got, "resume") {
		t.Fatalf("persist binding should hop via resume: %q", got)
	}
}

func TestEnterBinding_NonPersistAccepts(t *testing.T) {
	t.Setenv("TMUX", "/tmp/x,1,0")
	got := enterBinding("/bin/tsession", false)
	if !strings.Contains(got, "+accept") {
		t.Fatalf("non-persist binding should accept: %q", got)
	}
}
