package shellutil

import (
	"strings"
	"testing"
)

func TestQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"simple", "'simple'"},
		{"", "''"},
		{"it's", `'it'\''s'`},
		{"a'b'c", `'a'\''b'\''c'`},
	}
	for _, tc := range cases {
		if got := Quote(tc.in); got != tc.want {
			t.Errorf("Quote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestJoin(t *testing.T) {
	got := Join([]string{"ssh", "-t", "user@host", "it's fine"})
	want := `'ssh' '-t' 'user@host' 'it'\''s fine'`
	if got != want {
		t.Errorf("Join() = %q, want %q", got, want)
	}
}

func TestJoin_Empty(t *testing.T) {
	if got := Join(nil); got != "" {
		t.Errorf("Join(nil) = %q, want empty", got)
	}
}

func TestInteractiveLoginCommandUsesConfiguredShell(t *testing.T) {
	cmd := InteractiveLoginCommand("command -v tmux")
	for _, want := range []string{
		`remote_shell=${SHELL:-/bin/sh}`,
		`csh|tcsh) shell_flags=-ic`,
		`*) shell_flags=-lic`,
		`exec "$remote_shell" "$shell_flags"`,
		`exec /bin/sh -c`,
		`command -v tmux`,
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("interactive command missing %q:\n%s", want, cmd)
		}
	}
}

func TestCopilotResolverCommand_SetsCopilotBinOnSuccessPath(t *testing.T) {
	cmd := CopilotResolverCommand()
	if cmd == "" {
		t.Fatal("expected non-empty resolver command")
	}
	// The resolver must leave $copilot_bin set for callers to reference, and
	// must fail loudly (exit 127) rather than silently continuing when
	// resolution fails.
	for _, want := range []string{"copilot_bin=", "command -v copilot", "exit 127"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("resolver command missing %q:\n%s", want, cmd)
		}
	}
}
