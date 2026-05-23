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
	if len(got) != 1 || got[0].State != StateUnknown {
		t.Fatalf("want one unknown entry, got %+v", got)
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
