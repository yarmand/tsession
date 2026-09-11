package attachcmd

import (
	"strings"
	"testing"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
)

func TestWebSessionName_DeterministicAndPrefixed(t *testing.T) {
	a := WebSessionName("", "session-1")
	b := WebSessionName("", "session-1")
	if a != b {
		t.Fatalf("expected deterministic name, got %q and %q", a, b)
	}
	if !strings.HasPrefix(a, "tsession-web-") {
		t.Fatalf("expected tsession-web- prefix, got %q", a)
	}
	// 12 hex chars after the prefix.
	hexPart := strings.TrimPrefix(a, "tsession-web-")
	if len(hexPart) != 12 {
		t.Fatalf("expected 12 hex chars, got %q (%d chars)", hexPart, len(hexPart))
	}
}

func TestWebSessionName_DiffersByOriginAndID(t *testing.T) {
	names := map[string]bool{}
	for _, tc := range []struct{ origin, id string }{
		{"", "a"},
		{"", "b"},
		{"host1", "a"},
		{"host2", "a"},
	} {
		name := WebSessionName(tc.origin, tc.id)
		if names[name] {
			t.Fatalf("collision for origin=%q id=%q: %q", tc.origin, tc.id, name)
		}
		names[name] = true
	}
}

func TestBuild_LocalWithTarget_UsesGroupedAttach(t *testing.T) {
	s := sessions.Session{ID: "sess1", TmuxName: "proj", TmuxTarget: "proj:2.1"}
	bin, args, err := Build(s, config.Remote{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bin != "sh" || len(args) != 2 || args[0] != "-c" {
		t.Fatalf("unexpected local command shape: bin=%q args=%v", bin, args)
	}
	script := args[1]
	web := WebSessionName("", "sess1")
	for _, want := range []string{
		"tmux has-session -t " + shQ(web),
		"tmux new-session -d -s " + shQ(web) + " -t " + shQ("proj"),
		"tmux select-window -t " + shQ(web+":2"),
		"tmux select-pane -t " + shQ(web+":2.1"),
		"exec tmux attach-session -t " + shQ(web),
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}
}

func TestBuild_LocalNoTarget_Copilot(t *testing.T) {
	s := sessions.Session{ID: "sess2", Source: "copilot"}
	_, args, err := Build(s, config.Remote{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	script := args[1]
	web := WebSessionName("", "sess2")
	for _, want := range []string{
		"tmux has-session -t " + shQ(web),
		"tmux new-session -d -s " + shQ(web),
		"copilot --resume=",
		"sess2",
		"exec tmux attach-session -t " + shQ(web),
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, " -t 'proj'") {
		t.Errorf("no-target script must not reference a group target:\n%s", script)
	}
}

func TestBuild_LocalNoTarget_Pi(t *testing.T) {
	s := sessions.Session{ID: "sess3", Source: "pi"}
	_, args, err := Build(s, config.Remote{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	script := args[1]
	if !strings.Contains(script, "pi --session") || !strings.Contains(script, "sess3") {
		t.Errorf("expected pi resume command, got:\n%s", script)
	}
	if strings.Contains(script, "copilot") {
		t.Errorf("pi session script should not mention copilot:\n%s", script)
	}
}

func TestBuild_RemoteSSH_WithTarget(t *testing.T) {
	s := sessions.Session{
		ID:                  "rsess1",
		Origin:              "myhost",
		RemoteTmuxAvailable: true,
		RemoteTmuxTarget:    "work:0.0",
	}
	r := config.Remote{Name: "myhost", Type: "ssh", Host: "myhost.example.com"}
	bin, args, err := Build(s, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bin != "ssh" {
		t.Fatalf("expected ssh, got %q", bin)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "myhost.example.com") {
		t.Errorf("expected host in ssh args: %v", args)
	}
	// The whole remote script is one shell-quoted trailing argument, since
	// ssh joins its trailing args with spaces to form the remote command
	// line — see Build's comment on the ssh/codespace case.
	last := args[len(args)-1]
	if !strings.HasPrefix(last, "bash -lc ") {
		t.Fatalf("expected last ssh arg to start with 'bash -lc ', got %q", last)
	}
	web := WebSessionName("myhost", "rsess1")
	if !strings.Contains(last, "tmux new-session -d -s") || !strings.Contains(last, web) {
		t.Errorf("expected grouped attach script embedded in ssh command, got %q", last)
	}
}

func TestBuild_RemoteSSH_NoTarget_ResolvesCopilotBinary(t *testing.T) {
	s := sessions.Session{
		ID:                  "rsess2",
		Origin:              "myhost",
		RemoteTmuxAvailable: true,
	}
	r := config.Remote{Name: "myhost", Type: "ssh", Host: "myhost.example.com"}
	_, args, err := Build(s, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := args[len(args)-1]
	for _, want := range []string{
		"remote_shell=", // resolver logic present
		`TSESSION_COPILOT_BIN="$copilot_bin"`,
		`exec "$copilot_bin" --resume=`,
	} {
		if !strings.Contains(last, want) {
			t.Errorf("missing %q in remote no-target script:\n%s", want, last)
		}
	}
}

func TestBuild_RemoteSSH_NoTmuxAtAll_DirectResume(t *testing.T) {
	s := sessions.Session{
		ID:                  "rsess3",
		Origin:              "myhost",
		RemoteTmuxAvailable: false,
	}
	r := config.Remote{Name: "myhost", Type: "ssh", Host: "myhost.example.com"}
	_, args, err := Build(s, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := args[len(args)-1]
	if strings.Contains(last, "tmux new-session") || strings.Contains(last, "tmux has-session") {
		t.Errorf("no-tmux script must not reference tmux at all:\n%s", last)
	}
	if !strings.Contains(last, `exec "$copilot_bin" --resume=`) {
		t.Errorf("expected direct resume command, got:\n%s", last)
	}
}

func TestBuild_RemoteCodespace(t *testing.T) {
	s := sessions.Session{ID: "rsess4", Origin: "cs1", RemoteTmuxAvailable: true, RemoteTmuxTarget: "w:0.0"}
	r := config.Remote{Name: "cs1", Type: "codespace", Codespace: "my-codespace"}
	bin, args, err := Build(s, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bin != "gh" {
		t.Fatalf("expected gh, got %q", bin)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "my-codespace") {
		t.Errorf("expected codespace name in args: %v", args)
	}
	last := args[len(args)-1]
	if !strings.HasPrefix(last, "bash -lc ") {
		t.Fatalf("expected codespace command to embed script as one bash -lc arg, got %q", last)
	}
}

func TestBuild_RemoteDevcontainer_SeparateExecArgs(t *testing.T) {
	s := sessions.Session{ID: "rsess5", Origin: "dc1", RemoteTmuxAvailable: true, RemoteTmuxTarget: "w:0.0"}
	r := config.Remote{Name: "dc1", Type: "devcontainer", Container: "my-container", User: "vscode"}
	bin, args, err := Build(s, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bin != "docker" {
		t.Fatalf("expected docker, got %q", bin)
	}
	// Unlike ssh/codespace, devcontainer passes bash/-lc/script as three
	// separate argv entries — docker execs directly without an intermediate
	// shell rejoining them, so no extra quoting layer is needed or correct.
	if len(args) < 3 {
		t.Fatalf("expected at least 3 trailing args, got %v", args)
	}
	if args[len(args)-3] != "bash" || args[len(args)-2] != "-lc" {
		t.Fatalf("expected trailing [...,bash,-lc,script], got %v", args[len(args)-3:])
	}
	script := args[len(args)-1]
	if !strings.Contains(script, "tmux new-session") {
		t.Errorf("expected raw script as final arg, got %q", script)
	}
}

func TestBuild_UnsupportedRemoteType(t *testing.T) {
	s := sessions.Session{ID: "r6", Origin: "weird", RemoteTmuxAvailable: false}
	r := config.Remote{Name: "weird", Type: "carrier-pigeon"}
	if _, _, err := Build(s, r); err == nil {
		t.Fatal("expected error for unsupported remote type")
	}
}

func TestBuild_MalformedTarget(t *testing.T) {
	cases := []string{"noColonAtAll", "session:", ":0.1", "session:nodothere", "session:.1", "session:0."}
	for _, target := range cases {
		t.Run(target, func(t *testing.T) {
			s := sessions.Session{ID: "sess1", TmuxTarget: target}
			if _, _, err := Build(s, config.Remote{}); err == nil {
				t.Fatalf("expected error for malformed target %q", target)
			}
		})
	}
}

func TestSplitTarget(t *testing.T) {
	cases := []struct {
		in         string
		session    string
		windowPane string
		ok         bool
	}{
		{"proj:2.1", "proj", "2.1", true},
		{"noColon", "", "", false},
		{":2.1", "", "", false},
		{"proj:", "", "", false},
	}
	for _, tc := range cases {
		session, windowPane, ok := splitTarget(tc.in)
		if session != tc.session || windowPane != tc.windowPane || ok != tc.ok {
			t.Errorf("splitTarget(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.in, session, windowPane, ok, tc.session, tc.windowPane, tc.ok)
		}
	}
}

func TestSplitWindowPane(t *testing.T) {
	cases := []struct {
		in     string
		window string
		pane   string
		ok     bool
	}{
		{"2.1", "2", "1", true},
		{"0.0", "0", "0", true},
		{"nodothere", "", "", false},
		{".1", "", "", false},
		{"0.", "", "", false},
	}
	for _, tc := range cases {
		window, pane, ok := splitWindowPane(tc.in)
		if window != tc.window || pane != tc.pane || ok != tc.ok {
			t.Errorf("splitWindowPane(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.in, window, pane, ok, tc.window, tc.pane, tc.ok)
		}
	}
}

// shQ mirrors shellutil.Quote for test assertions without importing it twice
// under a different name in every test.
func shQ(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestBuildKill_LocalWithTarget(t *testing.T) {
	s := sessions.Session{ID: "sess1", TmuxTarget: "proj:0.0"}
	bin, args, ok, err := BuildKill(s, config.Remote{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a local session with a tmux target")
	}
	if bin != "sh" || len(args) != 2 {
		t.Fatalf("unexpected shape: bin=%q args=%v", bin, args)
	}
	web := WebSessionName("", "sess1")
	if !strings.Contains(args[1], "tmux kill-session -t "+shQ(web)) {
		t.Fatalf("expected kill-session script, got %q", args[1])
	}
}

func TestBuildKill_LocalNoTarget(t *testing.T) {
	s := sessions.Session{ID: "sess2"}
	_, args, ok, err := BuildKill(s, config.Remote{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true: local no-target sessions still create a web tmux session")
	}
	if !strings.Contains(args[1], "tmux kill-session") {
		t.Fatalf("expected kill-session script, got %q", args[1])
	}
}

func TestBuildKill_RemoteNoTmuxAtAll_NotOK(t *testing.T) {
	s := sessions.Session{ID: "r1", Origin: "host1", RemoteTmuxAvailable: false}
	r := config.Remote{Name: "host1", Type: "ssh", Host: "host1.example.com"}
	_, _, ok, err := BuildKill(s, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false: nothing to kill when the remote has no tmux at all")
	}
}

func TestBuildKill_RemoteWithTmux(t *testing.T) {
	s := sessions.Session{ID: "r2", Origin: "host1", RemoteTmuxAvailable: true, RemoteTmuxTarget: "w:0.0"}
	r := config.Remote{Name: "host1", Type: "ssh", Host: "host1.example.com"}
	bin, args, ok, err := BuildKill(s, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if bin != "ssh" {
		t.Fatalf("expected ssh, got %q", bin)
	}
	last := args[len(args)-1]
	if !strings.Contains(last, "tmux kill-session") {
		t.Fatalf("expected kill-session in wrapped script, got %q", last)
	}
}
