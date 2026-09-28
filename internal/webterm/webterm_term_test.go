package webterm

import (
	"os"
	"strings"
	"testing"
)

// The browser terminal is an xterm.js emulator. tmux only emits OSC 52
// clipboard escapes for xterm-compatible TERM values, so the PTY child must
// always get one regardless of how the GUI process itself was launched
// (Finder/Dock launches have no TERM at all).
func TestTerminalEnv_ForcesXtermTerm(t *testing.T) {
	for _, inherited := range []string{"", "dumb", "screen-256color", "xterm-256color"} {
		t.Setenv("TERM", inherited)
		if inherited == "" {
			os.Unsetenv("TERM")
		}
		env := terminalEnv()
		var got string
		found := 0
		for _, kv := range env {
			if strings.HasPrefix(kv, "TERM=") {
				got = strings.TrimPrefix(kv, "TERM=")
				found++
			}
		}
		if found != 1 {
			t.Fatalf("inherited TERM=%q: want exactly one TERM entry, got %d", inherited, found)
		}
		if got != "xterm-256color" {
			t.Fatalf("inherited TERM=%q: got TERM=%q, want xterm-256color", inherited, got)
		}
	}
}
