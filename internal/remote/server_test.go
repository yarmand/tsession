package remote

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestBuildActiveSnapshot_ReturnsOnlyActiveStates(t *testing.T) {
	seedTestHomeSessionStore(t)

	payload, err := BuildActiveSnapshot(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range payload.Sessions {
		if s.State == "exited" || s.State == "unknown" || s.State == "idle" {
			t.Fatalf("unexpected inactive state in payload: %s", s.State)
		}
	}
}

func TestServe_HealthRequest(t *testing.T) {
	in := strings.NewReader(`{"id":"1","method":"health"}` + "\n")
	var out strings.Builder
	if err := Serve(in, &out); err != nil {
		t.Fatalf("Serve error: %v", err)
	}
	var resp RPCResponse
	if err := decodeLine(out.String(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.ID != "1" {
		t.Fatalf("resp = %+v, want ok health response", resp)
	}
}

func TestServe_SnapshotRequest(t *testing.T) {
	seedTestHomeSessionStore(t)

	in := strings.NewReader(`{"id":"2","method":"snapshot"}` + "\n")
	var out strings.Builder
	if err := Serve(in, &out); err != nil {
		t.Fatalf("Serve error: %v", err)
	}
	var resp RPCResponse
	if err := decodeLine(out.String(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.ID != "2" {
		t.Fatalf("resp = %+v, want ok snapshot response", resp)
	}
}

func TestServe_UnknownMethod(t *testing.T) {
	in := strings.NewReader(`{"id":"3","method":"bogus"}` + "\n")
	var out strings.Builder
	if err := Serve(in, &out); err != nil {
		t.Fatalf("Serve error: %v", err)
	}
	var resp RPCResponse
	if err := decodeLine(out.String(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.OK || resp.Error == "" {
		t.Fatalf("resp = %+v, want error response for unknown method", resp)
	}
}

func decodeLine(s string, v *RPCResponse) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("empty output")
	}
	return json.Unmarshal([]byte(s), v)
}

func seedTestHomeSessionStore(t *testing.T) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	copilotDir := filepath.Join(home, ".copilot")
	if err := os.MkdirAll(copilotDir, 0o755); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", filepath.Join(copilotDir, "session-store.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY, cwd TEXT, repository TEXT, branch TEXT,
			summary TEXT, created_at TEXT, updated_at TEXT, host_type TEXT
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
}
