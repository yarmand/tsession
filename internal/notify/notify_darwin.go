//go:build darwin

package notify

import "os/exec"

// fire displays a macOS notification with sound via osascript. Failures (e.g.
// running headless) are ignored.
func fire(title, sound string) {
	script := `display notification "` + escapeAppleScript(title) +
		`" sound name "` + escapeAppleScript(sound) + `"`
	_ = exec.Command("osascript", "-e", script).Run()
}
