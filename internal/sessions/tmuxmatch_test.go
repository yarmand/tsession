package sessions

import (
	"testing"

	"github.com/yarma/tsession/internal/tmux"
)

func TestResolveTmuxByPID_WalksAncestorsToPane(t *testing.T) {
	// Process tree:
	//   1
	//   └─ 100 (tmux server)
	//       └─ 200 (pane shell — k6-scenarios)
	//           └─ 300 (agency wrapper)
	//               └─ 400 (copilot — session "alpha")
	//       └─ 250 (pane shell — other)
	sess := []Session{{ID: "alpha"}}
	sd := []StateDirInfo{{ID: "alpha", PID: 400}}
	panes := []tmux.Pane{
		{SessionName: "k6-scenarios", WindowIndex: "2", PaneIndex: "1", PID: 200},
		{SessionName: "other", WindowIndex: "0", PaneIndex: "0", PID: 250},
	}
	ppid := map[int]int{400: 300, 300: 200, 200: 100, 250: 100, 100: 1}

	got := resolveWithTree(sess, sd, panes, ppid)
	if got[0].TmuxName != "k6-scenarios" {
		t.Errorf("want TmuxName=k6-scenarios, got %q", got[0].TmuxName)
	}
	if got[0].TmuxTarget != "k6-scenarios:2.1" {
		t.Errorf("want TmuxTarget=k6-scenarios:2.1, got %q", got[0].TmuxTarget)
	}
}

func TestResolveTmuxByPID_PreservesExistingTmuxName(t *testing.T) {
	sess := []Session{{ID: "alpha", TmuxName: "already-set"}}
	sd := []StateDirInfo{{ID: "alpha", PID: 400}}
	panes := []tmux.Pane{{SessionName: "by-pid", PID: 400}}
	ppid := map[int]int{400: 1}

	got := resolveWithTree(sess, sd, panes, ppid)
	if got[0].TmuxName != "already-set" {
		t.Errorf("should not overwrite, got %q", got[0].TmuxName)
	}
}

func TestResolveTmuxByPID_NoMatchLeavesEmpty(t *testing.T) {
	sess := []Session{{ID: "alpha"}}
	sd := []StateDirInfo{{ID: "alpha", PID: 400}}
	panes := []tmux.Pane{{SessionName: "k6", PID: 999}}
	ppid := map[int]int{400: 1}

	got := resolveWithTree(sess, sd, panes, ppid)
	if got[0].TmuxName != "" {
		t.Errorf("want empty TmuxName, got %q", got[0].TmuxName)
	}
}

// resolveWithTree is a test helper mirroring ResolveTmuxByPID but using an
// injected process-tree map, so tests don't depend on `ps`.
func resolveWithTree(sess []Session, sd []StateDirInfo, panes []tmux.Pane, ppid map[int]int) []Session {
	pidBySession := map[string]int{}
	for _, s := range sd {
		if s.PID > 0 {
			pidBySession[s.ID] = s.PID
		}
	}
	paneByPID := map[int]tmux.Pane{}
	for _, p := range panes {
		paneByPID[p.PID] = p
	}
	for i := range sess {
		if sess[i].TmuxName != "" {
			continue
		}
		pid, ok := pidBySession[sess[i].ID]
		if !ok {
			continue
		}
		if pane, ok := walkToPane(pid, ppid, paneByPID); ok {
			sess[i].TmuxName = pane.SessionName
			sess[i].TmuxTarget = pane.Target()
		}
	}
	return sess
}

func TestParseProcessTree(t *testing.T) {
	in := "  100   1\n  200 100\n  badline\n  300 200\n"
	got := parseProcessTree(in)
	if got[100] != 1 || got[200] != 100 || got[300] != 200 {
		t.Errorf("unexpected parse: %v", got)
	}
	if _, ok := got[0]; ok {
		t.Errorf("bad line should not produce entry")
	}
}
