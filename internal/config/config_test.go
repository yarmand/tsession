package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadRemotes_ActiveDefaultsTrue(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgPath, []byte(`remotes:
  - name: devbox
    host: devbox.local
  - name: standby
    host: standby.local
    active: false
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Remotes[0].Active {
		t.Fatalf("remote[0].Active = %v, want true", cfg.Remotes[0].Active)
	}
	if cfg.Remotes[1].Active {
		t.Fatalf("remote[1].Active = %v, want false", cfg.Remotes[1].Active)
	}
}

func TestLoadRemotes(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgPath, []byte(`remotes:
  - name: devbox
    host: devbox.local
  - name: server
    host: user@server.example.com
    copilot_dir: /home/user/.copilot
  - name: my-codespace
    type: codespace
    codespace: urban-broccoli-abc123
  - name: my-container
    type: devcontainer
    container: myapp_devcontainer
    user: vscode
  - name: custom
    ssh_command: gh codespace ssh
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Remotes) != 5 {
		t.Fatalf("got %d remotes, want 5", len(cfg.Remotes))
	}

	// SSH remote with defaults
	r := cfg.Remotes[0]
	if r.Name != "devbox" {
		t.Errorf("remote[0].Name = %q, want devbox", r.Name)
	}
	if r.Host != "devbox.local" {
		t.Errorf("remote[0].Host = %q, want devbox.local", r.Host)
	}
	if r.CopilotDir != "~/.copilot" {
		t.Errorf("remote[0].CopilotDir = %q, want ~/.copilot", r.CopilotDir)
	}
	if r.Type != "ssh" {
		t.Errorf("remote[0].Type = %q, want ssh", r.Type)
	}

	// SSH remote with custom copilot_dir
	if cfg.Remotes[1].CopilotDir != "/home/user/.copilot" {
		t.Errorf("remote[1].CopilotDir = %q, want /home/user/.copilot", cfg.Remotes[1].CopilotDir)
	}

	// Codespace
	cs := cfg.Remotes[2]
	if cs.Type != "codespace" {
		t.Errorf("remote[2].Type = %q, want codespace", cs.Type)
	}
	if cs.Codespace != "urban-broccoli-abc123" {
		t.Errorf("remote[2].Codespace = %q, want urban-broccoli-abc123", cs.Codespace)
	}

	// Devcontainer
	dc := cfg.Remotes[3]
	if dc.Type != "devcontainer" {
		t.Errorf("remote[3].Type = %q, want devcontainer", dc.Type)
	}
	if dc.Container != "myapp_devcontainer" {
		t.Errorf("remote[3].Container = %q, want myapp_devcontainer", dc.Container)
	}
	if dc.User != "vscode" {
		t.Errorf("remote[3].User = %q, want vscode", dc.User)
	}

	// Custom ssh_command
	if cfg.Remotes[4].SSHCommand != "gh codespace ssh" {
		t.Errorf("remote[4].SSHCommand = %q, want 'gh codespace ssh'", cfg.Remotes[4].SSHCommand)
	}
}

func TestLoadRemotes_CodeCommand(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgPath, []byte(`code_command: /usr/local/bin/code
remotes:
  - name: devbox
    host: devbox.local
    code_command: /home/me/.local/bin/code
  - name: standby
    host: standby.local
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CodeCommand != "/usr/local/bin/code" {
		t.Fatalf("cfg.CodeCommand = %q, want /usr/local/bin/code", cfg.CodeCommand)
	}
	if got := cfg.Remotes[0].CodeBinary(cfg); got != "/home/me/.local/bin/code" {
		t.Errorf("remote[0].CodeBinary() = %q, want per-remote override", got)
	}
	if got := cfg.Remotes[1].CodeBinary(cfg); got != "/usr/local/bin/code" {
		t.Errorf("remote[1].CodeBinary() = %q, want top-level default", got)
	}
}

func TestRemoteCodeBinary_EmptyWhenUnset(t *testing.T) {
	r := Remote{Name: "bare"}
	if got := r.CodeBinary(&Config{}); got != "" {
		t.Errorf("CodeBinary() = %q, want empty (resolve from PATH)", got)
	}
	if got := r.CodeBinary(nil); got != "" {
		t.Errorf("CodeBinary(nil) = %q, want empty", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := LoadFrom("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatal("expected no error for missing file")
	}
	if len(cfg.Remotes) != 0 {
		t.Fatalf("got %d remotes, want 0 for missing file", len(cfg.Remotes))
	}
}

func TestLoadEmptyFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(cfgPath, []byte(""), 0o644)

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Remotes) != 0 {
		t.Fatalf("got %d remotes, want 0", len(cfg.Remotes))
	}
}

func TestGatherCommand(t *testing.T) {
	tests := []struct {
		name     string
		remote   Remote
		wantBin  string
		wantArgs []string
	}{
		{
			name:     "ssh default",
			remote:   Remote{Type: "ssh", Host: "devbox.local"},
			wantBin:  "ssh",
			wantArgs: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "devbox.local"},
		},
		{
			name:     "codespace",
			remote:   Remote{Type: "codespace", Codespace: "my-cs"},
			wantBin:  "gh",
			wantArgs: []string{"codespace", "ssh", "--codespace", "my-cs", "--"},
		},
		{
			name:     "devcontainer",
			remote:   Remote{Type: "devcontainer", Container: "myapp", User: "vscode"},
			wantBin:  "docker",
			wantArgs: []string{"exec", "-i", "-u", "vscode", "myapp"},
		},
		{
			name:     "custom ssh_command",
			remote:   Remote{Type: "ssh", SSHCommand: "gh codespace ssh", Host: ""},
			wantBin:  "gh",
			wantArgs: []string{"codespace", "ssh"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin, args := tt.remote.GatherCommand()
			if bin != tt.wantBin {
				t.Errorf("bin = %q, want %q", bin, tt.wantBin)
			}
			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Errorf("args = %v, want %v", args, tt.wantArgs)
			}
		})
	}
}

func TestResumeCommand(t *testing.T) {
	tests := []struct {
		name     string
		remote   Remote
		wantBin  string
		wantArgs []string
	}{
		{
			name:     "ssh default",
			remote:   Remote{Type: "ssh", Host: "devbox.local"},
			wantBin:  "ssh",
			wantArgs: []string{"-t", "devbox.local"},
		},
		{
			name:     "codespace",
			remote:   Remote{Type: "codespace", Codespace: "my-cs"},
			wantBin:  "gh",
			wantArgs: []string{"codespace", "ssh", "--codespace", "my-cs", "-t", "--"},
		},
		{
			name:     "devcontainer",
			remote:   Remote{Type: "devcontainer", Container: "myapp", User: "vscode"},
			wantBin:  "docker",
			wantArgs: []string{"exec", "-it", "-u", "vscode", "myapp"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin, args := tt.remote.ResumeCommand()
			if bin != tt.wantBin {
				t.Errorf("bin = %q, want %q", bin, tt.wantBin)
			}
			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Errorf("args = %v, want %v", args, tt.wantArgs)
			}
		})
	}
}

func TestLoadAgentCommand(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgPath, []byte(`agent_command: "agency copilot --hub"
remotes:
  - name: devbox
    host: devbox.local
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentCommand != "agency copilot --hub" {
		t.Fatalf("cfg.AgentCommand = %q, want %q", cfg.AgentCommand, "agency copilot --hub")
	}
	if got := cfg.AgentCommandOrDefault(); got != "agency copilot --hub" {
		t.Errorf("AgentCommandOrDefault() = %q, want configured value", got)
	}
}

func TestAgentCommandOrDefault(t *testing.T) {
	if got := (&Config{}).AgentCommandOrDefault(); got != DefaultAgentCommand {
		t.Errorf("empty config: got %q, want %q", got, DefaultAgentCommand)
	}
	if got := (*Config)(nil).AgentCommandOrDefault(); got != DefaultAgentCommand {
		t.Errorf("nil config: got %q, want %q", got, DefaultAgentCommand)
	}
	if got := (&Config{AgentCommand: "  "}).AgentCommandOrDefault(); got != DefaultAgentCommand {
		t.Errorf("whitespace config: got %q, want %q", got, DefaultAgentCommand)
	}
}
