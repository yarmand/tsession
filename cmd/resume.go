package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/donestate"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

var execCommand = exec.Command

// switchToPane focuses the agent at target, docking the navigator pane beside
// it when running inside the navigator (navPane non-empty). Swappable in tests.
var switchToPane = func(navPane, target string) error {
	if navPane == "" {
		return tmux.SwitchClient(target)
	}
	return tmux.NavHop(navPane, target)
}

func Resume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	summary := fs.String("summary", "", "session summary (tiebreaker for multi-pane remotes)")
	origin := fs.String("origin", "", "remote origin name (for instant local pane lookup)")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: tsession resume <session-id>")
	}
	id := fs.Arg(0)
	navPane := os.Getenv("TMUX_PANE")

	// Fast path: if origin is provided, find the local tmux pane connected to
	// that remote by checking pane child processes. No SSH or session loading needed.
	if *origin != "" {
		if paneTarget := findLocalPaneForRemote(*origin, *summary); paneTarget != "" {
			if err := switchToPane(navPane, paneTarget); err != nil {
				return err
			}
			_ = donestate.Clear(id)
			return nil
		}
	}

	local, err := loadAll(14*24*time.Hour, true)
	if err != nil {
		return err
	}
	match := findSessionByID(local, id)
	if match == nil || match.Origin != "" {
		merged, err := loadAll(14*24*time.Hour, false)
		if err != nil {
			return err
		}
		if cached := findSessionByID(merged, id); cached != nil {
			match = cached
		}
	}

	if match != nil && match.Origin != "" {
		// Remote session: resolve local tmux pane by matching pane title to summary.
		// The cache doesn't store this (panes are dynamic), so we resolve at resume time.
		if match.TmuxTarget == "" && match.Summary != "" {
			if panes, err := listPanesWithTitleFn(); err == nil && len(panes) > 0 {
				for _, p := range panes {
					if sessions.MatchTitle(p.Title, match.Summary) {
						match.TmuxTarget = p.Target()
						match.TmuxName = p.SessionName
						break
					}
				}
			}
		}
		// If we found a local tmux pane, switch to it (instant).
		if match.TmuxTarget != "" || match.TmuxName != "" {
			tmuxTarget := match.TmuxTarget
			if tmuxTarget == "" {
				tmuxTarget = match.TmuxName
			}
			if err := switchToPane(navPane, tmuxTarget); err != nil {
				return err
			}
			_ = donestate.Clear(id)
			return nil
		}
		// No local pane found — open a new connection and resume directly.
		remote, err := remoteHost(match.Origin)
		if err != nil {
			return err
		}
		args := remoteResumeArgs(*match, remote)
		cmd := execCommand(args[0], args[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}

	if match != nil && (match.TmuxTarget != "" || match.TmuxName != "") {
		tmuxTarget := match.TmuxTarget
		if tmuxTarget == "" {
			tmuxTarget = match.TmuxName
		}
		if err := switchToPane(navPane, tmuxTarget); err != nil {
			return err
		}
		_ = donestate.Clear(id)
		return nil
	}

	// Pi session without tmux match: use `pi --session`
	if match != nil && match.Source == "pi" {
		cmd := exec.Command("pi", "--session", id)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}

	// Copilot session without tmux match: use `copilot --resume`
	if _, err := exec.LookPath("copilot"); err != nil {
		fmt.Println(id)
		return nil
	}
	cmd := execCommand("copilot", "--resume="+id)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func remoteResumeArgs(s sessions.Session, remote config.Remote) []string {
	resumeCmd := "copilot --resume=" + s.ID
	bin, args := remote.ResumeCommand()
	result := append([]string{bin}, args...)
	result = append(result, resumeCmd)
	return result
}

func findSessionByID(all []sessions.Session, id string) *sessions.Session {
	for i := range all {
		if all[i].ID == id {
			return &all[i]
		}
	}
	return nil
}

func remoteHost(origin string) (config.Remote, error) {
	cfg, err := loadConfig()
	if err != nil {
		return config.Remote{}, err
	}
	for _, remote := range cfg.Remotes {
		if remote.Name == origin {
			switch remote.Type {
			case "codespace":
				if remote.Codespace == "" {
					break
				}
				return remote, nil
			case "devcontainer":
				if remote.Container == "" {
					break
				}
				return remote, nil
			default:
				if remote.Host == "" && remote.SSHCommand == "" {
					break
				}
				return remote, nil
			}
		}
	}
	return config.Remote{}, fmt.Errorf("remote %q not configured", origin)
}

// findLocalPaneForRemote finds a local tmux pane that is connected to the
// given remote by checking child processes of each pane against the remote's
// SSH/docker command pattern. Uses summary as tiebreaker if multiple panes
// match the same remote.
func findLocalPaneForRemote(origin, summary string) string {
	cfg, err := loadConfig()
	if err != nil {
		return ""
	}

	// Find the remote config to build match patterns.
	var remote *config.Remote
	for i := range cfg.Remotes {
		if cfg.Remotes[i].Name == origin {
			remote = &cfg.Remotes[i]
			break
		}
	}
	if remote == nil {
		return ""
	}

	infos := []struct{ Name, Type, Host, SSHCommand, Codespace, Container string }{{
		Name: remote.Name, Type: remote.Type, Host: remote.Host,
		SSHCommand: remote.SSHCommand, Codespace: remote.Codespace, Container: remote.Container,
	}}
	patterns := sessions.MatchPatterns(infos)
	if len(patterns) == 0 {
		return ""
	}

	panes, err := listPanesWithTitleFn()
	if err != nil || len(panes) == 0 {
		return ""
	}

	// Use ResolveRemotePanes machinery: create a dummy session and resolve.
	dummy := map[string][]sessions.Session{
		origin: {{ID: "lookup", Origin: origin, Summary: summary}},
	}
	result := sessions.ResolveRemotePanes(dummy, panes, patterns)
	if sess := result[origin]; len(sess) > 0 && sess[0].TmuxTarget != "" {
		return sess[0].TmuxTarget
	}

	// Fallback: if process-tree matching didn't work but we have a summary,
	// try title matching directly.
	if summary != "" {
		for _, p := range panes {
			if sessions.MatchTitle(p.Title, summary) {
				return p.Target()
			}
		}
	}
	return ""
}
