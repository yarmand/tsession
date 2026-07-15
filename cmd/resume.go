package cmd

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/donestate"
	"github.com/yarma/tsession/internal/remote"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

var execCommand = exec.Command

func Resume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	target := fs.String("target", "", "tmux client to switch (/dev/... path, or any value to pick interactively)")
	_ = fs.String("summary", "", "session summary (retained for browse command compatibility)")
	origin := fs.String("origin", "", "remote origin name")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: tsession resume [--target=...] <session-id>")
	}
	id := fs.Arg(0)

	var match *sessions.Session
	if *origin != "" {
		merged, err := loadAll(14*24*time.Hour, false)
		if err != nil {
			return err
		}
		match = findSessionByIDAndOrigin(merged, id, *origin)
		if match == nil {
			selectedRemote, err := remoteHost(*origin)
			if err != nil {
				return err
			}
			remoteMap, warnings := fetchRemoteSessions(
				context.Background(),
				[]config.Remote{selectedRemote},
				14*24*time.Hour,
				10*time.Second,
				remote.FetchOptions{},
			)
			match = findSessionByIDAndOrigin(remoteMap[*origin], id, *origin)
			if match == nil && len(warnings) > 0 {
				return fmt.Errorf("remote session %q from %q not found: %s", id, *origin, strings.Join(warnings, "; "))
			}
		}
		if match == nil {
			return fmt.Errorf("remote session %q from %q not found", id, *origin)
		}
	} else {
		local, err := loadAll(14*24*time.Hour, true)
		if err != nil {
			return err
		}
		match = findSessionByID(local, id)
		if match == nil || match.Origin != "" {
			merged, err := loadAll(14*24*time.Hour, false)
			if err != nil {
				return err
			}
			if cached := findSessionByID(merged, id); cached != nil {
				match = cached
			}
		}
	}

	if match != nil && match.Origin != "" {
		remote, err := remoteHost(match.Origin)
		if err != nil {
			return err
		}
		bridge, err := ensureRemoteBridge(*match, remote)
		if err != nil {
			return err
		}
		if err := switchClientTargetFn(bridge, *target); err != nil {
			return err
		}
		_ = donestate.Clear(id)
		return nil
	}

	if match != nil && (match.TmuxTarget != "" || match.TmuxName != "") {
		tmuxTarget := match.TmuxTarget
		if tmuxTarget == "" {
			tmuxTarget = match.TmuxName
		}
		if err := tmux.SwitchClientTarget(tmuxTarget, *target); err != nil {
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

func findSessionByID(all []sessions.Session, id string) *sessions.Session {
	for i := range all {
		if all[i].ID == id {
			return &all[i]
		}
	}
	return nil
}

func findSessionByIDAndOrigin(all []sessions.Session, id, origin string) *sessions.Session {
	for i := range all {
		if all[i].ID == id && all[i].Origin == origin {
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
