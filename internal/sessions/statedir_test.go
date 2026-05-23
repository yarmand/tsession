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

	cases := []struct {
		uuid string
		last string
		want State
	}{
		{"u-work", `{"type":"tool.execution_start","timestamp":"2026-05-17T10:00:00.000Z"}`, StateWorking},
		{"u-wait", `{"type":"ask_question","timestamp":"2026-05-17T10:00:00.000Z"}`, StateWaiting},
		{"u-perm", `{"type":"permission_request","timestamp":"2026-05-17T10:00:00.000Z"}`, StateWaiting},
		{"u-exit", `{"type":"session.shutdown","timestamp":"2026-05-17T10:00:00.000Z"}`, StateExited},
		{"u-idle", `{"type":"assistant.message","timestamp":"2026-05-17T10:00:00.000Z"}`, StateInactiveIdle},
	}

	for _, c := range cases {
		writeEvents(t, filepath.Join(root, c.uuid), c.last)
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
