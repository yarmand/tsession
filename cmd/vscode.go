package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

func Vscode(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: tsession vscode <session-id>")
	}
	id := args[0]

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
	if match == nil {
		return fmt.Errorf("session %s not found", id)
	}

	dir := match.CWD
	if dir == "" {
		return fmt.Errorf("session %s has no working directory", id)
	}

	cmd := exec.Command("code", dir)
	return cmd.Run()
}
