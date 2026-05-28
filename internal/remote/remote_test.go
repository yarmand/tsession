package remote

import (
	"strings"
	"testing"
	"time"
)

func TestParseGatherOutput(t *testing.T) {
	data := []byte(`{
		"sessions":[{"id":"s1","cwd":"/work/a","repository":"git@github.com:o/r.git","summary":"demo","updated_at":"2026-05-17T10:00:00Z"}],
		"state_dirs":[{"id":"s1","cwd":"/work/a","events_tail":"{\"type\":\"assistant.turn_end\",\"timestamp\":\"2026-05-17T10:01:00Z\"}\n","pid":321}],
		"tmux_sessions":[{"name":"proj-a","path":"/work/a"}],
		"tmux_panes":[{"session_name":"proj-a","window_index":"1","pane_index":"0","pid":200}],
		"process_tree":{"321":300,"300":200}
	}`)

	got, err := ParseGatherOutput(data)
	if err != nil {
		t.Fatalf("ParseGatherOutput error: %v", err)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].ID != "s1" {
		t.Fatalf("unexpected sessions: %+v", got.Sessions)
	}
	if len(got.StateDirs) != 1 || got.StateDirs[0].PID != 321 {
		t.Fatalf("unexpected state dirs: %+v", got.StateDirs)
	}
	if len(got.TmuxSessions) != 1 || got.TmuxSessions[0].Name != "proj-a" {
		t.Fatalf("unexpected tmux sessions: %+v", got.TmuxSessions)
	}
	if len(got.TmuxPanes) != 1 || got.TmuxPanes[0].PID != 200 {
		t.Fatalf("unexpected tmux panes: %+v", got.TmuxPanes)
	}
	if got.ProcessTree[321] != 300 || got.ProcessTree[300] != 200 {
		t.Fatalf("unexpected process tree: %+v", got.ProcessTree)
	}
}

func TestParseGatherOutputError(t *testing.T) {
	_, err := ParseGatherOutput([]byte(`{"error":"sqlite3 not found"}`))
	if err == nil || !strings.Contains(err.Error(), "sqlite3 not found") {
		t.Fatalf("want sqlite3 error, got %v", err)
	}
}

func TestParseGatherOutputEmpty(t *testing.T) {
	got, err := ParseGatherOutput([]byte(`{"sessions":[],"state_dirs":[],"tmux_sessions":[],"tmux_panes":[],"process_tree":{}}`))
	if err != nil {
		t.Fatalf("ParseGatherOutput error: %v", err)
	}
	if len(got.Sessions) != 0 || len(got.StateDirs) != 0 || len(got.TmuxSessions) != 0 || len(got.TmuxPanes) != 0 || len(got.ProcessTree) != 0 {
		t.Fatalf("want empty collections, got %+v", got)
	}
}

func TestGatherResultToSessions(t *testing.T) {
	gr := &GatherResult{
		Sessions: []GatherSession{{
			ID:         "s1",
			CWD:        "/work/demo",
			Repository: "git@github.com:yarma/demo.git",
			Summary:    "demo session",
			UpdatedAt:  "2026-05-17T10:00:00Z",
		}},
		StateDirs: []GatherStateDir{{
			ID:  "s1",
			CWD: "/work/demo",
			PID: 321,
			EventsTail: strings.Join([]string{
				`{"type":"assistant.turn_start","timestamp":"2026-05-17T10:00:00Z"}`,
				`{"type":"assistant.turn_end","timestamp":"2026-05-17T10:01:00Z"}`,
			}, "\n") + "\n",
		}},
		TmuxSessions: []GatherTmuxSession{{Name: "unrelated", Path: "/somewhere/else"}},
		TmuxPanes:    []GatherTmuxPane{{SessionName: "pane-match", WindowIndex: "2", PaneIndex: "1", PID: 200}},
		ProcessTree:  map[int]int{321: 300, 300: 200},
	}

	got := gr.ToSessions("devbox", 14*24*time.Hour)
	if len(got) != 1 {
		t.Fatalf("want 1 session, got %d", len(got))
	}
	if got[0].Origin != "devbox" {
		t.Fatalf("want origin devbox, got %q", got[0].Origin)
	}
	if got[0].State.String() != "active" {
		t.Fatalf("want active idle state, got %s", got[0].State)
	}
	if got[0].TmuxName != "pane-match" || got[0].TmuxTarget != "pane-match:2.1" {
		t.Fatalf("want pane-based tmux match, got %+v", got[0])
	}
	if got[0].LastEventAt.IsZero() || got[0].UpdatedAt.IsZero() {
		t.Fatalf("want timestamps parsed, got %+v", got[0])
	}
}
