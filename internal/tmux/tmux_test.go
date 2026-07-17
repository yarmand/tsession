package tmux

import (
	"errors"
	"os/exec"
	"testing"
)

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

func TestListSessionsPropagatesUnexpectedCommandError(t *testing.T) {
	oldOutput := listTmuxOutput
	t.Cleanup(func() { listTmuxOutput = oldOutput })
	listTmuxOutput = func(...string) ([]byte, error) {
		return nil, errors.New("permission denied")
	}

	if _, err := ListSessions(); err == nil {
		t.Fatal("expected tmux list-sessions error")
	}
}

func TestListSessionsTreatsMissingServerOutputAsUnavailable(t *testing.T) {
	oldOutput := listTmuxOutput
	t.Cleanup(func() { listTmuxOutput = oldOutput })
	listTmuxOutput = func(...string) ([]byte, error) {
		return []byte("no server running on /tmp/tmux-501/default"), &exec.ExitError{}
	}

	got, err := ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("sessions = %+v, want none", got)
	}
}

func TestListPanesPropagatesUnexpectedCommandError(t *testing.T) {
	oldOutput := listTmuxOutput
	t.Cleanup(func() { listTmuxOutput = oldOutput })
	listTmuxOutput = func(...string) ([]byte, error) {
		return nil, errors.New("permission denied")
	}

	if _, err := ListPanes(); err == nil {
		t.Fatal("expected tmux list-panes error")
	}
}

func TestNoTmuxServerRecognizesMissingServer(t *testing.T) {
	err := &exec.ExitError{Stderr: []byte("no server running on /tmp/tmux-501/default")}
	if !noTmuxServer(nil, err) {
		t.Fatal("missing tmux server was not recognized")
	}
	if noTmuxServer(nil, errors.New("permission denied")) {
		t.Fatal("unexpected command error was treated as a missing server")
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

func TestResolveSessionName(t *testing.T) {
	cases := []struct {
		name       string
		desired    string
		path       string
		existing   []Session
		wantName   string
		wantResume bool
	}{
		{
			name:     "no existing",
			desired:  "foo",
			path:     "/a",
			existing: nil,
			wantName: "foo", wantResume: false,
		},
		{
			name:     "same name same path resumes",
			desired:  "foo",
			path:     "/a",
			existing: []Session{{Name: "foo", Path: "/a"}},
			wantName: "foo", wantResume: true,
		},
		{
			name:     "same name different path suffixes",
			desired:  "foo",
			path:     "/a",
			existing: []Session{{Name: "foo", Path: "/b"}},
			wantName: "foo-2", wantResume: false,
		},
		{
			name:    "skips taken suffixes",
			desired: "foo",
			path:    "/a",
			existing: []Session{
				{Name: "foo", Path: "/b"},
				{Name: "foo-2", Path: "/c"},
			},
			wantName: "foo-3", wantResume: false,
		},
		{
			name:    "resumes suffixed session already at target path",
			desired: "foo",
			path:    "/a",
			existing: []Session{
				{Name: "foo", Path: "/b"},
				{Name: "foo-2", Path: "/a"},
			},
			wantName: "foo-2", wantResume: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotResume := ResolveSessionName(tc.desired, tc.path, tc.existing)
			if gotName != tc.wantName || gotResume != tc.wantResume {
				t.Fatalf("got (%q,%v), want (%q,%v)", gotName, gotResume, tc.wantName, tc.wantResume)
			}
		})
	}
}
