package notify

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestSanitizeLabel(t *testing.T) {
	cases := map[string]string{
		"plain":           "plain",
		"line1\nline2":    "line1 line2",
		"tab\there":       "tab here",
		"cr\rthere":       "cr there",
		"  spaced  out  ": "spaced out",
		"bell\x07del\x7f": "bell del",
		"a\n\n\nb":        "a b",
	}
	for in, want := range cases {
		if got := sanitizeLabel(in); got != want {
			t.Errorf("sanitizeLabel(%q) = %q, want %q", in, got, want)
		}
	}
	// Every label source must be sanitized, not just Summary.
	if got := displayLabel(sessions.Session{Name: "evil\nname"}); got != "evil name" {
		t.Errorf("Name not sanitized: got %q", got)
	}
	if got := displayLabel(sessions.Session{ID: "id\x00x"}); got != "id x" {
		t.Errorf("ID not sanitized: got %q", got)
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

// TestProcessExactlyOnceAcrossObservers simulates two independent observers
// (e.g. the watch daemon and a browse --watch `list` reload) processing the same
// done transition over the shared snapshot. The transition must fire exactly
// once: the first observer fires and persists, the second sees no change.
func TestProcessExactlyOnceAcrossObservers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := withCaptureFire(t)

	// Establish a prior sighting so the done transition is not the silent
	// first-sighting case.
	_ = Process([]sessions.Session{sess("a", sessions.StateWorking)})

	// Observer 1 then observer 2 both see the session as done.
	_ = Process([]sessions.Session{sess("a", sessions.StateDone)})
	_ = Process([]sessions.Session{sess("a", sessions.StateDone)})

	if len(*calls) != 1 {
		t.Fatalf("transition must fire exactly once across observers, got %d: %v", len(*calls), *calls)
	}
}

// TestProcessDuplicateSessionIDsFireOnce guards against spam when a session ID
// appears more than once in a single batch (e.g. the same session reported by
// two data sources). It must fire only once for that batch's transition.
func TestProcessDuplicateSessionIDsFireOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := withCaptureFire(t)

	_ = Process([]sessions.Session{sess("a", sessions.StateWorking), sess("a", sessions.StateWorking)})
	_ = Process([]sessions.Session{sess("a", sessions.StateDone), sess("a", sessions.StateDone)})

	if len(*calls) != 1 {
		t.Fatalf("duplicate IDs in one batch must fire once, got %d: %v", len(*calls), *calls)
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
	fireFunc = func(title, sound string) error {
		calls = append(calls, fired{title, sound})
		return nil
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

// TestProcessFireFailureSurfaced verifies a notification that cannot be shown
// (e.g. permission denied) does not crash, is reported to the caller, and is
// not retried on the next cycle (graceful degradation).
func TestProcessFireFailureSurfaced(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var calls int
	prev := fireFunc
	fireFunc = func(title, sound string) error {
		calls++
		return errors.New("osascript: Not authorized to send Apple events")
	}
	t.Cleanup(func() { fireFunc = prev })

	if err := Process([]sessions.Session{sess("a", sessions.StateWorking)}); err != nil {
		t.Fatalf("first sighting should not error: %v", err)
	}
	err := Process([]sessions.Session{sess("a", sessions.StateDone)})
	if err == nil {
		t.Fatal("expected a surfaced fire error, got nil")
	}
	if !strings.Contains(err.Error(), "could not show") {
		t.Fatalf("error should be user-visible, got %q", err.Error())
	}
	if calls != 1 {
		t.Fatalf("expected exactly one fire attempt, got %d", calls)
	}

	// A subsequent cycle in the same state must not retry the failed
	// notification: the snapshot advanced despite the failure.
	if err := Process([]sessions.Session{sess("a", sessions.StateDone)}); err != nil {
		t.Fatalf("no transition should not error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("failed notification should not be retried, got %d attempts", calls)
	}
}

// TestProcessDedupesIdenticalFireErrors verifies that the same failure across
// many sessions is collapsed into a single reported error message.
func TestProcessDedupesIdenticalFireErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	prev := fireFunc
	fireFunc = func(title, sound string) error {
		return errors.New("osascript: permission denied")
	}
	t.Cleanup(func() { fireFunc = prev })

	working := []sessions.Session{sess("a", sessions.StateWorking), sess("b", sessions.StateWorking)}
	if err := Process(working); err != nil {
		t.Fatalf("first sighting should not error: %v", err)
	}
	done := []sessions.Session{sess("a", sessions.StateDone), sess("b", sessions.StateDone)}
	err := Process(done)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := strings.Count(err.Error(), "permission denied"); got != 1 {
		t.Fatalf("identical errors should be deduped, message mentions it %d times: %q", got, err.Error())
	}
	if !strings.Contains(err.Error(), "2 notification(s)") {
		t.Fatalf("error should report the failure count, got %q", err.Error())
	}
}
