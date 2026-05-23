package sessions

import (
	"os/exec"
	"strconv"
	"strings"

	"github.com/yarma/tsession/internal/tmux"
)

// ResolveTmuxByPID assigns Session.TmuxName for any session whose owning
// process (from inuse.<pid>.lock) is a descendant of a tmux pane PID.
// Sessions that already have a TmuxName set are left untouched, so this
// runs as a refinement step after Merge's CWD/basename-based matching.
//
// Matching by process tree is authoritative: it works even when the tmux
// session's name and path bear no resemblance to the session's cwd.
func ResolveTmuxByPID(sess []Session, sd []StateDirInfo, panes []tmux.Pane) []Session {
	if len(panes) == 0 {
		return sess
	}
	pidBySession := map[string]int{}
	for _, s := range sd {
		if s.PID > 0 {
			pidBySession[s.ID] = s.PID
		}
	}
	if len(pidBySession) == 0 {
		return sess
	}

	paneByPID := map[int]tmux.Pane{}
	for _, p := range panes {
		paneByPID[p.PID] = p
	}

	ppid := buildProcessTree()
	if ppid == nil {
		return sess
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

// walkToPane climbs the parent chain from pid until it finds a PID that
// matches a tmux pane. Returns the matching pane and true; (zero, false)
// if no match before reaching pid 1.
func walkToPane(pid int, ppid map[int]int, paneByPID map[int]tmux.Pane) (tmux.Pane, bool) {
	for steps := 0; pid > 1 && steps < 64; steps++ {
		if pane, ok := paneByPID[pid]; ok {
			return pane, true
		}
		parent, ok := ppid[pid]
		if !ok || parent == pid {
			return tmux.Pane{}, false
		}
		pid = parent
	}
	return tmux.Pane{}, false
}

// buildProcessTree returns a pid → ppid map by parsing `ps -A -o pid=,ppid=`.
// Returns nil if ps fails.
func buildProcessTree() map[int]int {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		return nil
	}
	return parseProcessTree(string(out))
}

func parseProcessTree(s string) map[int]int {
	m := map[int]int{}
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		m[pid] = ppid
	}
	return m
}
