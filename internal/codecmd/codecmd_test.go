package codecmd

import (
	"strings"
	"testing"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
)

func TestKey_StableAndDistinct(t *testing.T) {
	k1 := Key("", "abc")
	k2 := Key("", "abc")
	if k1 != k2 {
		t.Fatalf("Key not stable: %q != %q", k1, k2)
	}
	if k1 == Key("box", "abc") {
		t.Error("Key should differ by origin")
	}
	if k1 == Key("", "def") {
		t.Error("Key should differ by session ID")
	}
	if len(k1) != 12 {
		t.Errorf("Key length = %d, want 12 hex chars", len(k1))
	}
}

func TestLocalCodeBinary(t *testing.T) {
	if got := LocalCodeBinary(nil); got != "code" {
		t.Errorf("LocalCodeBinary(nil) = %q, want code", got)
	}
	if got := LocalCodeBinary(&config.Config{}); got != "code" {
		t.Errorf("LocalCodeBinary(empty) = %q, want code", got)
	}
	cfg := &config.Config{CodeCommand: "/usr/local/bin/code"}
	if got := LocalCodeBinary(cfg); got != "/usr/local/bin/code" {
		t.Errorf("LocalCodeBinary(override) = %q, want override", got)
	}
}

func TestBuild_Local(t *testing.T) {
	s := sessions.Session{ID: "sess1", CWD: "/home/user/project"}
	bin, args, err := Build(s, config.Remote{}, nil, "/api/code/abc123", "/tmp/data")
	if err != nil {
		t.Fatal(err)
	}
	if bin != "code" {
		t.Errorf("bin = %q, want code", bin)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"serve-web", "--host 127.0.0.1", "--port 0",
		"--without-connection-token", "--accept-server-license-terms",
		"--server-base-path /api/code/abc123", "--server-data-dir /tmp/data",
		"--default-folder /home/user/project",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
}

func TestBuild_LocalWithConfiguredBinary(t *testing.T) {
	s := sessions.Session{ID: "sess1", CWD: "/home/user/project"}
	cfg := &config.Config{CodeCommand: "/opt/code/bin/code"}
	bin, _, err := Build(s, config.Remote{}, cfg, "/api/code/abc123", "/tmp/data")
	if err != nil {
		t.Fatal(err)
	}
	if bin != "/opt/code/bin/code" {
		t.Errorf("bin = %q, want configured override", bin)
	}
}

func TestBuild_LocalNoCWD(t *testing.T) {
	s := sessions.Session{ID: "sess1"}
	_, args, err := Build(s, config.Remote{}, nil, "/api/code/abc123", "/tmp/data")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range args {
		if a == "--default-folder" {
			t.Errorf("did not expect --default-folder when CWD is empty: %v", args)
		}
	}
}

func TestBuild_RemoteSSHResolvesCodeBinary(t *testing.T) {
	s := sessions.Session{ID: "sess1", Origin: "box", CWD: "/home/user/project"}
	r := config.Remote{Name: "box", Type: "ssh", Host: "box.local"}
	bin, args, err := Build(s, r, nil, "/api/code/abc123", "/tmp/data")
	if err != nil {
		t.Fatal(err)
	}
	if bin != "ssh" {
		t.Errorf("bin = %q, want ssh", bin)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"box.local", "-t",
		"code_bin=", "command -v code", "exit 127",
		`exec "$code_bin"`,
		"serve-web", "--server-base-path", "/api/code/abc123",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
}

func TestBuild_RemoteWithExplicitCodeCommand(t *testing.T) {
	s := sessions.Session{ID: "sess1", Origin: "box", CWD: "/home/user/project"}
	r := config.Remote{Name: "box", Type: "ssh", Host: "box.local", CodeCommand: "/home/me/bin/code"}
	_, args, err := Build(s, r, nil, "/api/code/abc123", "/tmp/data")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "code resolver") || strings.Contains(joined, "command -v code") {
		t.Errorf("did not expect resolver when explicit code_command is set:\n%s", joined)
	}
	if !strings.Contains(joined, "/home/me/bin/code") {
		t.Errorf("args missing explicit code binary path:\n%s", joined)
	}
}

func TestBuild_RemoteCodespace(t *testing.T) {
	s := sessions.Session{ID: "sess1", Origin: "cs", CWD: "/workspaces/proj"}
	r := config.Remote{Name: "cs", Type: "codespace", Codespace: "urban-broccoli"}
	bin, args, err := Build(s, r, nil, "/api/code/def456", "/tmp/data")
	if err != nil {
		t.Fatal(err)
	}
	if bin != "gh" {
		t.Errorf("bin = %q, want gh", bin)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"codespace", "ssh", "--codespace", "urban-broccoli", "-t"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
}

func TestBuild_RemoteDevcontainer(t *testing.T) {
	s := sessions.Session{ID: "sess1", Origin: "dc", CWD: "/workspace"}
	r := config.Remote{Name: "dc", Type: "devcontainer", Container: "myapp", User: "vscode"}
	bin, args, err := Build(s, r, nil, "/api/code/ghi789", "/tmp/data")
	if err != nil {
		t.Fatal(err)
	}
	if bin != "docker" {
		t.Errorf("bin = %q, want docker", bin)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"exec", "-it", "-u vscode", "myapp"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
}

func TestBuild_UnsupportedRemoteType(t *testing.T) {
	s := sessions.Session{ID: "sess1", Origin: "x"}
	r := config.Remote{Name: "x", Type: "bogus"}
	_, _, err := Build(s, r, nil, "/api/code/x", "/tmp/data")
	if err == nil {
		t.Fatal("expected error for unsupported remote type")
	}
}

func TestTunnelCommand_SSH(t *testing.T) {
	r := config.Remote{Name: "box", Type: "ssh", Host: "box.local"}
	bin, args, ok, err := TunnelCommand(r, 12345, 54321)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true for ssh remote")
	}
	if bin != "ssh" {
		t.Errorf("bin = %q, want ssh", bin)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"-N", "-L", "127.0.0.1:12345:127.0.0.1:54321", "box.local", "BatchMode=yes"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
}

func TestTunnelCommand_Codespace(t *testing.T) {
	r := config.Remote{Name: "cs", Type: "codespace", Codespace: "urban-broccoli"}
	bin, args, ok, err := TunnelCommand(r, 12345, 54321)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true for codespace remote")
	}
	if bin != "gh" {
		t.Errorf("bin = %q, want gh", bin)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"ports", "forward", "54321:12345", "--codespace", "urban-broccoli"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
}

func TestTunnelCommand_Devcontainer_NotSupported(t *testing.T) {
	r := config.Remote{Name: "dc", Type: "devcontainer", Container: "myapp"}
	_, _, ok, err := TunnelCommand(r, 12345, 54321)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected ok=false for devcontainer remote (no ssh -L equivalent)")
	}
}

func TestTunnelCommand_UnsupportedType(t *testing.T) {
	r := config.Remote{Name: "x", Type: "bogus"}
	_, _, _, err := TunnelCommand(r, 1, 2)
	if err == nil {
		t.Fatal("expected error for unsupported remote type")
	}
}

func TestTunnelCommand_CustomSSHCommand(t *testing.T) {
	r := config.Remote{Name: "custom", Type: "ssh", SSHCommand: "gh codespace ssh"}
	bin, args, ok, err := TunnelCommand(r, 111, 222)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if bin != "gh" {
		t.Errorf("bin = %q, want gh", bin)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "127.0.0.1:111:127.0.0.1:222") {
		t.Errorf("args missing forward spec:\n%s", joined)
	}
	if strings.Contains(joined, "BatchMode") {
		t.Errorf("did not expect BatchMode for non-literal-ssh custom command:\n%s", joined)
	}
}
