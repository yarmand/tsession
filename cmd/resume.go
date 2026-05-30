package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/yarma/tsession/internal/donestate"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

func Resume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	target := fs.String("target", "", "tmux client to switch (/dev/... path, or any value to pick interactively)")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: tsession resume [--target=...] <session-id>")
	}
	id := fs.Arg(0)

	merged, err := loadAll(14*24*time.Hour, false)
	if err != nil {
		return err
	}
	var match *sessions.Session
	for i := range merged {
		if merged[i].ID == id {
			match = &merged[i]
			break
		}
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
	cmd := exec.Command("copilot", "--resume="+id)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
