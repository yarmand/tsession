// internal/sessions/statedir_test.go
package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEvents(t *testing.T, dir string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadStateDir_InfersStateFromLastEvent(t *testing.T) {
	root := t.TempDir()

	// Each case is a chronological list of event lines; the classifier
	// considers the recent window, not just the last line.
	turnStart := `{"type":"assistant.turn_start","timestamp":"2026-05-17T10:00:00.000Z"}`
	cases := []struct {
		uuid string
		lines []string
		want State
	}{
		{"u-work", []string{
			turnStart,
			`{"type":"tool.execution_start","timestamp":"2026-05-17T10:00:01.000Z","data":{"toolName":"bash"}}`,
		}, StateWorking},
		{"u-wait", []string{
			turnStart,
			`{"type":"tool.execution_start","timestamp":"2026-05-17T10:00:01.000Z","data":{"toolName":"ask_user"}}`,
		}, StateWaiting},
		// Parallel tools: report_intent completes but ask_user is still pending.
		{"u-wait-parallel", []string{
			turnStart,
			`{"type":"tool.execution_start","timestamp":"2026-05-17T10:00:01.000Z","data":{"toolName":"report_intent"}}`,
			`{"type":"tool.execution_start","timestamp":"2026-05-17T10:00:01.000Z","data":{"toolName":"ask_user"}}`,
			`{"type":"tool.execution_complete","timestamp":"2026-05-17T10:00:01.100Z","data":{}}`,
		}, StateWaiting},
		{"u-perm", []string{
			turnStart,
			`{"type":"tool.user_requested","timestamp":"2026-05-17T10:00:01.000Z"}`,
		}, StateWaiting},
		{"u-exit", []string{
			`{"type":"session.shutdown","timestamp":"2026-05-17T10:00:00.000Z"}`,
		}, StateExited},
		{"u-idle", []string{
			turnStart,
			`{"type":"assistant.message","timestamp":"2026-05-17T10:00:01.000Z"}`,
			`{"type":"assistant.turn_end","timestamp":"2026-05-17T10:00:02.000Z"}`,
		}, StateInactiveIdle},
	}

	for _, c := range cases {
		writeEvents(t, filepath.Join(root, c.uuid), c.lines...)
	}

	got, err := LoadAllStateDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]StateDirInfo{}
	for _, s := range got {
		by[s.ID] = s
	}
	for _, c := range cases {
		info, ok := by[c.uuid]
		if !ok {
			t.Errorf("missing state-dir info for %s", c.uuid)
			continue
		}
		if info.State != c.want {
			t.Errorf("%s: want state %s, got %s", c.uuid, c.want, info.State)
		}
		if info.LastEventAt.IsZero() {
			t.Errorf("%s: timestamp not parsed", c.uuid)
		}
	}
}

func TestLoadStateDir_MissingEventsFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "u-empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAllStateDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	// No events, no lock file, no DB lock → inactive idle.
	if len(got) != 1 || got[0].State != StateInactiveIdle {
		t.Fatalf("want one idle entry, got %+v", got)
	}
}

func TestLoadStateDir_NoEventsButLockFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "u-new")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a lock file to simulate a brand-new session.
	if err := os.WriteFile(filepath.Join(dir, "inuse.12345.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAllStateDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	// Has lock file (PID > 0) but no events → active idle.
	if len(got) != 1 || got[0].State != StateActiveIdle {
		t.Fatalf("want one active entry, got %+v", got)
	}
	if got[0].PID != 12345 {
		t.Errorf("want PID 12345, got %d", got[0].PID)
	}
}

func TestLoadStateDir_ReadsCWDFromWorkspaceYAML(t *testing.T) {
root := t.TempDir()
dir := filepath.Join(root, "u-ws")
writeEvents(t, dir, `{"type":"assistant.message","timestamp":"2026-05-17T10:00:00.000Z"}`)
ws := "id: u-ws\ncwd: /Users/x/some/project\nname: Demo\n"
if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(ws), 0o644); err != nil {
t.Fatal(err)
}
got, err := LoadStateDirsForIDs(root, []string{"u-ws"})
if err != nil {
t.Fatal(err)
}
if len(got) != 1 {
t.Fatalf("want 1 entry, got %d", len(got))
}
if got[0].CWD != "/Users/x/some/project" {
t.Errorf("CWD: want %q, got %q", "/Users/x/some/project", got[0].CWD)
}
}

func TestMerge_PrefersStateDirCWDOverStore(t *testing.T) {
store := []Session{{ID: "a", CWD: "/stale/path"}}
sd := []StateDirInfo{{ID: "a", State: StateInactiveIdle, CWD: "/fresh/path"}}
got := Merge(store, sd, nil)
if len(got) != 1 || got[0].CWD != "/fresh/path" {
t.Fatalf("want CWD /fresh/path, got %+v", got)
}
}

func TestMerge_KeepsStoreCWDWhenStateDirEmpty(t *testing.T) {
store := []Session{{ID: "a", CWD: "/store/path"}}
sd := []StateDirInfo{{ID: "a", State: StateInactiveIdle}}
got := Merge(store, sd, nil)
if len(got) != 1 || got[0].CWD != "/store/path" {
t.Fatalf("want CWD /store/path preserved, got %+v", got)
}
}

func TestPreliminaryState_AskUserToolMeansWaiting(t *testing.T) {
if s, _ := preliminaryState("tool.execution_start", "ask_user"); s != StateWaiting {
t.Errorf("ask_user tool: want StateWaiting, got %s", s)
}
if s, _ := preliminaryState("tool.execution_start", "bash"); s != StateWorking {
t.Errorf("bash tool: want StateWorking, got %s", s)
}
if s, _ := preliminaryState("session.shutdown", ""); s != StateExited {
t.Errorf("shutdown: want StateExited, got %s", s)
}
}

func TestDiscoverLiveSessions_FindsLockFiles(t *testing.T) {
	root := t.TempDir()

	// Session with lock file — should be discovered.
	dir1 := filepath.Join(root, "new-session-1")
	if err := os.MkdirAll(dir1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir1, "inuse.12345.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ws := "cwd: /Users/test/project-a\n"
	if err := os.WriteFile(filepath.Join(dir1, "workspace.yaml"), []byte(ws), 0o644); err != nil {
		t.Fatal(err)
	}

	// Session with lock file but already known — should be skipped.
	dir2 := filepath.Join(root, "known-session")
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "inuse.99999.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Session without lock file — should be skipped.
	dir3 := filepath.Join(root, "no-lock-session")
	if err := os.MkdirAll(dir3, 0o755); err != nil {
		t.Fatal(err)
	}

	known := map[string]bool{"known-session": true}
	got := DiscoverLiveSessions(root, known)

	if len(got) != 1 {
		t.Fatalf("want 1 discovered session, got %d: %+v", len(got), got)
	}
	if got[0].ID != "new-session-1" {
		t.Errorf("want ID new-session-1, got %s", got[0].ID)
	}
	if got[0].CWD != "/Users/test/project-a" {
		t.Errorf("want CWD /Users/test/project-a, got %s", got[0].CWD)
	}
	if got[0].UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set from dir mtime")
	}
}

func TestDiscoverLiveSessions_EmptyRoot(t *testing.T) {
	root := t.TempDir()
	got := DiscoverLiveSessions(root, nil)
	if len(got) != 0 {
		t.Fatalf("want 0 sessions for empty root, got %d", len(got))
	}
}

func TestDiscoverLiveSessions_NonexistentRoot(t *testing.T) {
	got := DiscoverLiveSessions("/nonexistent/path/xyz", nil)
	if len(got) != 0 {
		t.Fatalf("want 0 sessions for missing root, got %d", len(got))
	}
}
