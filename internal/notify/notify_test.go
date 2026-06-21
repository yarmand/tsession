package notify

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yarma/tsession/internal/sessions"
)

func TestNotifiableState(t *testing.T) {
	cases := []struct {
		state sessions.State
		want  string
	}{
		{sessions.StateDone, "done"},
		{sessions.StateWaiting, "question"},
		{sessions.StateWorking, ""},
		{sessions.StateActiveIdle, ""},
		{sessions.StateInactiveIdle, ""},
		{sessions.StateExited, ""},
		{sessions.StateUnknown, ""},
	}
	for _, c := range cases {
		if got := notifiableState(c.state); got != c.want {
			t.Errorf("notifiableState(%v) = %q, want %q", c.state, got, c.want)
		}
	}
}

func TestMessageFor(t *testing.T) {
	m, ok := messageFor("done", "myproj")
	if !ok || m.text != "[myproj] done!" || m.sound != "Tink" {
		t.Errorf("done message = %+v ok=%v", m, ok)
	}
	m, ok = messageFor("question", "myproj")
	if !ok || m.text != "[myproj] needs your input" || m.sound != "Funk" {
		t.Errorf("question message = %+v ok=%v", m, ok)
	}
	if _, ok := messageFor("", "myproj"); ok {
		t.Errorf("empty state should not produce a message")
	}
}

func TestDisplayLabel(t *testing.T) {
	if got := displayLabel(sessions.Session{Name: "n", Summary: "s", CWD: "/a/b"}); got != "n" {
		t.Errorf("Name priority: got %q", got)
	}
	if got := displayLabel(sessions.Session{Summary: "s", CWD: "/a/b"}); got != "s" {
		t.Errorf("Summary fallback: got %q", got)
	}
	if got := displayLabel(sessions.Session{CWD: "/a/b"}); got != "b" {
		t.Errorf("basename fallback: got %q", got)
	}
	if got := displayLabel(sessions.Session{Summary: "line1\nline2"}); got != "line1 line2" {
		t.Errorf("summary newline flatten: got %q", got)
	}
}

func TestEscapeAppleScript(t *testing.T) {
	cases := map[string]string{
		`plain`:      `plain`,
		`say "hi"`:   `say \"hi\"`,
		`back\slash`: `back\\slash`,
		`both "\"`:   `both \"\\\"`,
	}
	for in, want := range cases {
		if got := escapeAppleScript(in); got != want {
			t.Errorf("escapeAppleScript(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notify.json")

	s := loadSnapshot(path)
	if s.Entries == nil {
		t.Fatal("loadSnapshot of missing file returned nil Entries")
	}
	if len(s.Entries) != 0 {
		t.Fatalf("expected empty entries, got %v", s.Entries)
	}

	s.Entries["abc"] = "done"
	if err := saveSnapshot(path, s); err != nil {
		t.Fatalf("saveSnapshot: %v", err)
	}

	got := loadSnapshot(path)
	if got.Entries["abc"] != "done" {
		t.Fatalf("round trip lost data: %v", got.Entries)
	}
}

func TestLoadSnapshotCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notify.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := loadSnapshot(path)
	if s.Entries == nil || len(s.Entries) != 0 {
		t.Fatalf("corrupt file should yield empty map, got %v", s.Entries)
	}
}

type fired struct {
	title string
	sound string
}

// withCaptureFire swaps fireFunc for the duration of the test and restores it.
func withCaptureFire(t *testing.T) *[]fired {
	t.Helper()
	var calls []fired
	prev := fireFunc
	fireFunc = func(title, sound string) {
		calls = append(calls, fired{title, sound})
	}
	t.Cleanup(func() { fireFunc = prev })
	return &calls
}

func sess(id string, st sessions.State) sessions.Session {
	return sessions.Session{ID: id, Name: id, State: st}
}

func TestProcessFirstSightingDoesNotFire(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := withCaptureFire(t)

	if err := Process([]sessions.Session{sess("a", sessions.StateDone)}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("first sighting should not fire, got %v", *calls)
	}
}

func TestProcessFiresOnTransition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := withCaptureFire(t)

	if err := Process([]sessions.Session{sess("a", sessions.StateWorking)}); err != nil {
		t.Fatal(err)
	}
	if err := Process([]sessions.Session{sess("a", sessions.StateDone)}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].title != "[a] done!" || (*calls)[0].sound != "Tink" {
		t.Fatalf("expected one Tink done fire, got %v", *calls)
	}
}

func TestProcessFiresOnQuestion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := withCaptureFire(t)

	_ = Process([]sessions.Session{sess("a", sessions.StateWorking)})
	_ = Process([]sessions.Session{sess("a", sessions.StateWaiting)})
	if len(*calls) != 1 || (*calls)[0].title != "[a] needs your input" || (*calls)[0].sound != "Funk" {
		t.Fatalf("expected one Funk question fire, got %v", *calls)
	}
}

func TestProcessNoRefireWhileDone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := withCaptureFire(t)

	_ = Process([]sessions.Session{sess("a", sessions.StateWorking)})
	_ = Process([]sessions.Session{sess("a", sessions.StateDone)})
	_ = Process([]sessions.Session{sess("a", sessions.StateDone)})
	if len(*calls) != 1 {
		t.Fatalf("done should fire once, got %d: %v", len(*calls), *calls)
	}
}

func TestProcessRefiresAfterLeavingDone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := withCaptureFire(t)

	_ = Process([]sessions.Session{sess("a", sessions.StateWorking)})
	_ = Process([]sessions.Session{sess("a", sessions.StateDone)})
	_ = Process([]sessions.Session{sess("a", sessions.StateActiveIdle)})
	_ = Process([]sessions.Session{sess("a", sessions.StateDone)})
	if len(*calls) != 2 {
		t.Fatalf("expected refire after leaving done, got %d: %v", len(*calls), *calls)
	}
}
