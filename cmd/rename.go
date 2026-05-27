package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/yarma/tsession/internal/names"
)

func Rename(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: tsession rename <session-id> [name]")
	}
	id := args[0]

	var name string
	if len(args) >= 2 {
		name = strings.Join(args[1:], " ")
	} else {
		// Interactive prompt
		current := names.Get(id)
		if current != "" {
			fmt.Printf("Current name: %s\n", current)
		}
		fmt.Print("New name (empty to clear): ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			name = strings.TrimSpace(scanner.Text())
		}
	}

	if err := names.Set(id, name); err != nil {
		return err
	}
	if name == "" {
		fmt.Println("Name cleared.")
	} else {
		fmt.Printf("Renamed to: %s\n", name)
	}
	return nil
}
