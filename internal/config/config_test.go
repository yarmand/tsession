package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRemotes(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgPath, []byte(`remotes:
  - name: devbox
    host: devbox.local
  - name: server
    host: user@server.example.com
    copilot_dir: /home/user/.copilot
  - name: codespace
    ssh_command: gh codespace ssh
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Remotes) != 3 {
		t.Fatalf("got %d remotes, want 3", len(cfg.Remotes))
	}
	if cfg.Remotes[0].Name != "devbox" {
		t.Errorf("remote[0].Name = %q, want devbox", cfg.Remotes[0].Name)
	}
	if cfg.Remotes[0].Host != "devbox.local" {
		t.Errorf("remote[0].Host = %q, want devbox.local", cfg.Remotes[0].Host)
	}
	if cfg.Remotes[0].CopilotDir != "~/.copilot" {
		t.Errorf("remote[0].CopilotDir = %q, want ~/.copilot (default)", cfg.Remotes[0].CopilotDir)
	}
	if cfg.Remotes[0].SSHCommand != "ssh" {
		t.Errorf("remote[0].SSHCommand = %q, want ssh (default)", cfg.Remotes[0].SSHCommand)
	}
	if cfg.Remotes[1].CopilotDir != "/home/user/.copilot" {
		t.Errorf("remote[1].CopilotDir = %q, want /home/user/.copilot", cfg.Remotes[1].CopilotDir)
	}
	if cfg.Remotes[2].SSHCommand != "gh codespace ssh" {
		t.Errorf("remote[2].SSHCommand = %q, want 'gh codespace ssh'", cfg.Remotes[2].SSHCommand)
	}
	if cfg.Remotes[2].Host != "" {
		t.Errorf("remote[2].Host = %q, want empty", cfg.Remotes[2].Host)
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
