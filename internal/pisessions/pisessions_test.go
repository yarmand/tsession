package pisessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

func TestLoadAll_ReadsStateFiles(t *testing.T) {
	dir := t.TempDir()

	state := stateFile{
		ID:          "abc-123",
		State:       "working",
		CWD:         "/Users/x/project",
		Summary:     "test summary",
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		PID:         os.Getpid(), // use our own PID so it's "alive"
		SessionFile: "/path/to/session.jsonl",
	}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, "abc-123.json"), data, 0o644)

	got, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 session, got %d", len(got))
	}
	if got[0].ID != "abc-123" {
		t.Errorf("ID: want abc-123, got %s", got[0].ID)
	}
	if got[0].State != sessions.StateWorking {
		t.Errorf("State: want working, got %s", got[0].State)
	}
	if got[0].CWD != "/Users/x/project" {
		t.Errorf("CWD: want /Users/x/project, got %s", got[0].CWD)
	}
	if got[0].Summary != "test summary" {
		t.Errorf("Summary: want 'test summary', got %s", got[0].Summary)
	}
	if got[0].Source != "pi" {
		t.Errorf("Source: want pi, got %s", got[0].Source)
	}
}

func TestLoadAll_MapsStatesCorrectly(t *testing.T) {
	dir := t.TempDir()
	pid := os.Getpid()

	cases := []struct {
		fileState string
		want      sessions.State
	}{
		{"working", sessions.StateWorking},
		{"question", sessions.StateWaiting},
		{"done", sessions.StateDone},
		{"idle", sessions.StateActiveIdle},
		{"exited", sessions.StateExited},
	}
	for _, c := range cases {
		state := stateFile{
			ID:        "id-" + c.fileState,
			State:     c.fileState,
			CWD:       "/x",
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			PID:       pid,
		}
		data, _ := json.Marshal(state)
		os.WriteFile(filepath.Join(dir, "id-"+c.fileState+".json"), data, 0o644)
	}

	got, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(cases) {
		t.Fatalf("want %d sessions, got %d", len(cases), len(got))
	}
	byID := map[string]sessions.Session{}
	for _, s := range got {
		byID[s.ID] = s
	}
	for _, c := range cases {
		s := byID["id-"+c.fileState]
		if s.State != c.want {
			t.Errorf("state %q: want %s, got %s", c.fileState, c.want, s.State)
		}
	}
}

func TestLoadAll_SkipsNonJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o644)
	os.WriteFile(filepath.Join(dir, ".tmp.json"), []byte("{}"), 0o644)

	got, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

func TestLoadAll_MissingDirReturnsEmpty(t *testing.T) {
	got, err := loadFromDir("/nonexistent/path/xyz")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

func TestLoadAll_DetectsStalePID(t *testing.T) {
	dir := t.TempDir()

	state := stateFile{
		ID:        "stale-1",
		State:     "working",
		CWD:       "/x",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		PID:       999999999, // unlikely to be alive
	}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, "stale-1.json"), data, 0o644)

	got, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].State != sessions.StateExited {
		t.Errorf("stale PID should be exited, got %s", got[0].State)
	}
}

func TestStateDirInfos(t *testing.T) {
	dir := t.TempDir()

	state := stateFile{
		ID:    "test-id",
		State: "working",
		CWD:   "/x",
		PID:   42,
	}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, "test-id.json"), data, 0o644)

	sess := []sessions.Session{{ID: "test-id", Source: "pi"}}
	got := stateDirInfosFromDir(dir, sess)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].ID != "test-id" {
		t.Errorf("ID: want test-id, got %s", got[0].ID)
	}
	if got[0].PID != 42 {
		t.Errorf("PID: want 42, got %d", got[0].PID)
	}
}
