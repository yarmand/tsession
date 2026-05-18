package tmux

import "testing"

func TestParseListSessions(t *testing.T) {
	out := "alpha|/Users/x/alpha\nbeta|/Users/x/beta\n"
	got := parseListSessions(out)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if got[0].Name != "alpha" || got[0].Path != "/Users/x/alpha" {
		t.Errorf("got %+v", got[0])
	}
	if got[1].Name != "beta" || got[1].Path != "/Users/x/beta" {
		t.Errorf("got %+v", got[1])
	}
}

func TestParseListSessions_EmptyAndBlankLines(t *testing.T) {
	if got := parseListSessions(""); len(got) != 0 {
		t.Errorf("want empty, got %+v", got)
	}
	if got := parseListSessions("\n\n  \n"); len(got) != 0 {
		t.Errorf("want empty, got %+v", got)
	}
}
