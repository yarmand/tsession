//go:build darwin

package notify

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// fire displays a macOS notification with sound via osascript. It returns a
// non-nil error when the notification could not be shown (e.g. osascript is
// missing, the process is headless, or the user has denied notification
// permission), so callers can surface the failure instead of crashing or
// silently dropping it.
func fire(title, sound string) error {
	script := `display notification "` + escapeAppleScript(title) +
		`" sound name "` + escapeAppleScript(sound) + `"`
	cmd := exec.Command("osascript", "-e", script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("osascript: %s: %w", msg, err)
		}
		return fmt.Errorf("osascript: %w", err)
	}
	return nil
}
