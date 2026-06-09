package tmux

import (
	"reflect"
	"testing"
)

func TestPaneWidthArgs(t *testing.T) {
	got := paneWidthArgs("%3")
	want := []string{"display-message", "-p", "-t", "%3", "#{pane_width}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paneWidthArgs = %v, want %v", got, want)
	}
}

func TestJoinPaneLeftArgs_WithSize(t *testing.T) {
	got := joinPaneLeftArgs("%3", "sess:1.2", "82")
	want := []string{"join-pane", "-h", "-b", "-l", "82", "-s", "%3", "-t", "sess:1.2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("joinPaneLeftArgs = %v, want %v", got, want)
	}
}

func TestJoinPaneLeftArgs_NoSize(t *testing.T) {
	got := joinPaneLeftArgs("%3", "sess:1.2", "")
	want := []string{"join-pane", "-h", "-b", "-s", "%3", "-t", "sess:1.2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("joinPaneLeftArgs = %v, want %v", got, want)
	}
}

func TestSwitchClientArgs(t *testing.T) {
	got := switchClientArgs("sess:1.2")
	want := []string{"switch-client", "-t", "sess:1.2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("switchClientArgs = %v, want %v", got, want)
	}
}

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
