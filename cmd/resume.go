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

func Resume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	target := fs.String("target", "", "tmux client to switch (/dev/... path, or any value to pick interactively)")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: tsession resume [--target=...] <session-id>")
	}
	id := fs.Arg(0)

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
		if err := tmux.SwitchClientTarget(tmuxTarget, *target); err != nil {
			return err
		}
		_ = donestate.Clear(id)
		return nil
	}

	if _, err := exec.LookPath("copilot"); err != nil {
		fmt.Println(id)
		return nil
	}
	cmd := execCommand("copilot", "--resume="+id)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func remoteResumeArgs(s sessions.Session, remote config.Remote) []string {
	target := s.TmuxTarget
	if target == "" {
		target = s.TmuxName
	}
	remoteCmd := "copilot --resume=" + s.ID
	if target != "" {
		remoteCmd = "tmux attach -t " + target
	}

	bin, args := remote.ResumeCommand()
	result := append([]string{bin}, args...)
	result = append(result, remoteCmd)
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
