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

func TestFirstLine(t *testing.T) {
	if got := firstLine("\n  @3 \n@4\n"); got != "@3" {
		t.Fatalf("firstLine = %q, want @3", got)
	}
	if got := firstLine("   \n\n"); got != "" {
		t.Fatalf("firstLine empty = %q, want empty", got)
	}
}

func TestPaneIDArgs(t *testing.T) {
	got := paneIDArgs("work:1.0")
	want := []string{"display-message", "-p", "-t", "work:1.0", "#{pane_id}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paneIDArgs = %v, want %v", got, want)
	}
}

func TestSelectPaneArgs(t *testing.T) {
	got := selectPaneArgs("%5")
	want := []string{"select-pane", "-t", "%5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectPaneArgs = %v, want %v", got, want)
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
