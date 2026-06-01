package sessions

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/yarma/tsession/internal/tmux"
)

// ResolveRemotePanes assigns TmuxName/TmuxTarget to remote sessions by
// matching them to local tmux panes that are connected to the same remote host.
//
// The matching works by:
// 1. Finding local tmux panes whose child process matches a remote's connection command
// 2. Matching the pane's terminal title against remote session summaries
//
// This allows `resume` to switch to the local tmux pane already running
// the remote copilot session, rather than opening a new SSH connection.
func ResolveRemotePanes(remoteSessions map[string][]Session, panes []tmux.Pane, matchPatterns map[string][]string) map[string][]Session {
	if len(panes) == 0 || len(remoteSessions) == 0 || len(matchPatterns) == 0 {
		return remoteSessions
	}

	// Build process tree and command line map.
	processTree := buildProcessTree()
	if processTree == nil {
		return remoteSessions
	}
	cmdLines := buildCommandLines()

	// For each pane, check if it has a child process matching a remote's pattern.
	type paneRemote struct {
		pane       tmux.Pane
		remoteName string
	}
	var matches []paneRemote

	for _, pane := range panes {
		remoteName := findRemoteForPane(pane.PID, processTree, cmdLines, matchPatterns)
		if remoteName != "" {
			matches = append(matches, paneRemote{pane: pane, remoteName: remoteName})
		}
	}

	if len(matches) == 0 {
		return remoteSessions
	}

	// Group matched panes by remote name.
	panesByRemote := map[string][]tmux.Pane{}
	for _, m := range matches {
		panesByRemote[m.remoteName] = append(panesByRemote[m.remoteName], m.pane)
	}

	// Assign tmux targets to remote sessions.
	for remoteName, sessions := range remoteSessions {
		panesForRemote := panesByRemote[remoteName]
		if len(panesForRemote) == 0 {
			continue
		}

		if len(panesForRemote) == 1 && len(sessions) == 1 {
			// 1:1 match — assign directly.
			sessions[0].TmuxName = panesForRemote[0].SessionName
			sessions[0].TmuxTarget = panesForRemote[0].Target()
			remoteSessions[remoteName] = sessions
			continue
		}

		// Multiple panes or sessions — match by pane title vs session summary.
		assignByTitle(sessions, panesForRemote)
		remoteSessions[remoteName] = sessions
	}
	return remoteSessions
}

// findRemoteForPane checks if a pane's child processes match any remote's pattern.
func findRemoteForPane(panePID int, processTree map[int]int, cmdLines map[int]string, matchPatterns map[string][]string) string {
	// Get direct children of the pane PID.
	children := childrenOf(panePID, processTree)
	// Also check grandchildren (pane → shell → ssh).
	var grandchildren []int
	for _, child := range children {
		grandchildren = append(grandchildren, childrenOf(child, processTree)...)
	}
	allDescendants := append(children, grandchildren...)

	for _, pid := range allDescendants {
		cmdLine, ok := cmdLines[pid]
		if !ok {
			continue
		}
		for remoteName, patterns := range matchPatterns {
			for _, pattern := range patterns {
				if strings.Contains(cmdLine, pattern) {
					return remoteName
				}
			}
		}
	}
	return ""
}

// childrenOf returns PIDs whose parent is the given PID.
func childrenOf(parent int, processTree map[int]int) []int {
	var children []int
	for pid, ppid := range processTree {
		if ppid == parent {
			children = append(children, pid)
		}
	}
	return children
}

// assignByTitle matches sessions to panes by comparing the session summary
// with the pane's terminal title. Copilot sets the terminal title to the
// session summary, which propagates through SSH to the local tmux pane.
func assignByTitle(sessions []Session, panes []tmux.Pane) {
	// Build title → pane index.
	usedPanes := map[int]bool{}
	for i := range sessions {
		if sessions[i].TmuxName != "" {
			continue
		}
		if sessions[i].Summary == "" {
			continue
		}
		for j, pane := range panes {
			if usedPanes[j] {
				continue
			}
			if matchTitle(pane.Title, sessions[i].Summary) {
				sessions[i].TmuxName = pane.SessionName
				sessions[i].TmuxTarget = pane.Target()
				usedPanes[j] = true
				break
			}
		}
	}
}

// matchTitle checks if a pane title matches a session summary.
// The pane title may have emoji prefix (e.g. "🤖 ") added by copilot.
func matchTitle(title, summary string) bool {
	if title == "" || summary == "" {
		return false
	}
	// Exact match.
	if title == summary {
		return true
	}
	// Title may have emoji prefix — check if it ends with the summary.
	if strings.HasSuffix(title, summary) {
		return true
	}
	// Summary may be truncated in title or vice versa — check containment.
	if len(summary) > 10 && strings.Contains(title, summary) {
		return true
	}
	if len(title) > 10 && strings.Contains(summary, title) {
		return true
	}
	return false
}

// buildCommandLines returns a PID → command line map by parsing `ps -A -o pid=,command=`.
func buildCommandLines() map[int]string {
	out, err := exec.Command("ps", "-A", "-o", fmt.Sprintf("pid=,%s=", "command")).Output()
	if err != nil {
		return nil
	}
	return parseCommandLines(string(out))
}

func parseCommandLines(s string) map[int]string {
	m := map[int]string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "  PID COMMAND..."
		// PID is right-justified, followed by space and command.
		idx := strings.IndexByte(line, ' ')
		if idx < 0 {
			continue
		}
		pidStr := line[:idx]
		cmd := strings.TrimSpace(line[idx+1:])
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		m[pid] = cmd
	}
	return m
}

// MatchPatterns builds the match patterns for each remote, used to identify
// local tmux panes connected to that remote.
func MatchPatterns(remotes []struct{ Name, Type, Host, SSHCommand, Codespace, Container string }) map[string][]string {
	patterns := map[string][]string{}
	for _, r := range remotes {
		var p []string
		switch r.Type {
		case "codespace":
			if r.Codespace != "" {
				p = append(p, "codespace ssh", r.Codespace)
			}
		case "devcontainer":
			if r.Container != "" {
				p = append(p, "docker exec", r.Container)
			}
		default:
			if r.SSHCommand != "" {
				// Use parts of the custom SSH command.
				parts := strings.Fields(r.SSHCommand)
				if len(parts) > 0 {
					p = append(p, parts[len(parts)-1])
				}
			}
			if r.Host != "" {
				p = append(p, "ssh "+r.Host, r.Host)
			}
		}
		if len(p) > 0 {
			patterns[r.Name] = p
		}
	}
	return patterns
}
