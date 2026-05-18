package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

func Resume(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: tsession resume <session-id>")
	}
	id := args[0]

	merged, err := loadAll(14 * 24 * time.Hour)
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

	if match != nil && match.TmuxName != "" {
		return tmux.SwitchClient(match.TmuxName)
	}

	if _, err := exec.LookPath("copilot"); err != nil {
		fmt.Println(id)
		return nil
	}
	cmd := exec.Command("copilot", "--resume="+id)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
