package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/yarma/tsession/internal/reponames"
)

var (
	findSessionFn                  = findSession
	renameStdin          io.Reader = os.Stdin
	renameStdout         io.Writer = os.Stdout
	loadRepositoryAlias            = reponames.Get
	storeRepositoryAlias           = reponames.Set
)

func RenameRepository(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: tsession rename-repo <session-id> [alias]")
	}

	id := args[0]
	match := findSessionFn(id)
	if match == nil {
		return fmt.Errorf("session not found: %s", id)
	}
	repository := strings.TrimSpace(match.Repository)
	if repository == "" {
		return fmt.Errorf("session %s has no repository identity", id)
	}
	current, err := loadRepositoryAlias(repository)
	if err != nil {
		return err
	}

	var alias string
	if len(args) >= 2 {
		alias = strings.Join(args[1:], " ")
	} else {
		fmt.Fprintf(renameStdout, "Repository: %s\n", repository)
		fmt.Fprintf(renameStdout, "Current alias: %s\n", current)
		fmt.Fprintln(renameStdout, "New alias (empty to clear):")

		scanner := bufio.NewScanner(renameStdin)
		if scanner.Scan() {
			alias = strings.TrimSpace(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	}

	if err := storeRepositoryAlias(repository, alias); err != nil {
		return err
	}

	if alias == "" {
		fmt.Fprintln(renameStdout, "Alias cleared.")
	} else {
		fmt.Fprintf(renameStdout, "Repository renamed to: %s\n", alias)
	}
	return nil
}
