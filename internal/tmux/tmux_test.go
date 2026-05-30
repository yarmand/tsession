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

func TestResolveTarget_Empty(t *testing.T) {
	got, err := ResolveTarget("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

func TestResolveTarget_DevPath(t *testing.T) {
	got, err := ResolveTarget("/dev/ttys003")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/dev/ttys003" {
		t.Errorf("want /dev/ttys003, got %q", got)
	}
}

func TestSplitNonEmpty(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"\n\n", 0},
		{"/dev/ttys001\n/dev/ttys002\n", 2},
		{"  /dev/ttys001  \n", 1},
	}
	for _, tc := range cases {
		got := splitNonEmpty(tc.in)
		if len(got) != tc.want {
			t.Errorf("splitNonEmpty(%q): want %d items, got %d", tc.in, tc.want, len(got))
		}
	}
}
