package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const defaultCopilotDir = "~/.copilot"

type Remote struct {
	Name       string
	Type       string // "ssh" (default), "codespace", "devcontainer"
	Host       string
	CopilotDir string
	SSHCommand string // custom override — defaults based on Type
	Codespace  string // codespace name (type=codespace)
	Container  string // container name (type=devcontainer)
	User       string // user for docker exec (type=devcontainer)
}

type Config struct {
	Remotes []Remote
}

// GatherCommand returns the binary and args for piping the gather script via stdin.
// The caller appends: "bash -s -- <copilotDir> <hours>".
func (r Remote) GatherCommand() (string, []string) {
	switch r.Type {
	case "codespace":
		return "gh", []string{"codespace", "ssh", "--codespace", r.Codespace, "--"}
	case "devcontainer":
		args := []string{"exec", "-i"}
		if r.User != "" {
			args = append(args, "-u", r.User)
		}
		args = append(args, r.Container)
		return "docker", args
	default:
		// ssh or custom ssh_command
		sshCmd := r.SSHCommand
		if sshCmd == "" {
			sshCmd = "ssh"
		}
		parts := strings.Fields(sshCmd)
		args := append([]string{}, parts[1:]...)
		if parts[0] == "ssh" {
			args = append(args, "-o", "BatchMode=yes", "-o", "ConnectTimeout=10")
		}
		if r.Host != "" {
			args = append(args, r.Host)
		}
		return parts[0], args
	}
}

// ResumeCommand returns the binary and args for an interactive session.
// The caller appends the remote command to execute (e.g. "tmux attach -t X").
func (r Remote) ResumeCommand() (string, []string) {
	switch r.Type {
	case "codespace":
		return "gh", []string{"codespace", "ssh", "--codespace", r.Codespace, "-t", "--"}
	case "devcontainer":
		args := []string{"exec", "-it"}
		if r.User != "" {
			args = append(args, "-u", r.User)
		}
		args = append(args, r.Container)
		return "docker", args
	default:
		sshCmd := r.SSHCommand
		if sshCmd == "" {
			sshCmd = "ssh"
		}
		parts := strings.Fields(sshCmd)
		args := append([]string{}, parts[1:]...)
		args = append(args, "-t")
		if r.Host != "" {
			args = append(args, r.Host)
		}
		return parts[0], args
	}
}

// Load reads the default config at ~/.config/tsession/config.yaml.
// Returns an empty config (no error) if the file does not exist.
func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return &Config{}, nil
	}
	return LoadFrom(filepath.Join(home, ".config", "tsession", "config.yaml"))
}

// LoadFrom reads config from a specific path.
// Returns an empty config (no error) if the file does not exist.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, err
	}
	return parse(string(data))
}

// parse does minimal YAML parsing for our flat structure.
// We avoid a YAML dependency since the format is simple and stable.
func parse(s string) (*Config, error) {
	cfg := &Config{}
	lines := strings.Split(s, "\n")

	var current *Remote
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		if trimmed == "remotes:" {
			continue
		}

		if strings.HasPrefix(trimmed, "- name:") {
			if current != nil {
				cfg.Remotes = append(cfg.Remotes, *current)
			}
			current = &Remote{
				Name:       extractValue(trimmed[len("- name:"):]),
				CopilotDir: defaultCopilotDir,
				Type:       "ssh",
			}
			continue
		}

		if current != nil && indent >= 4 {
			switch {
			case strings.HasPrefix(trimmed, "host:"):
				current.Host = extractValue(trimmed[len("host:"):])
			case strings.HasPrefix(trimmed, "type:"):
				if v := extractValue(trimmed[len("type:"):]); v != "" {
					current.Type = v
				}
			case strings.HasPrefix(trimmed, "copilot_dir:"):
				if v := extractValue(trimmed[len("copilot_dir:"):]); v != "" {
					current.CopilotDir = v
				}
			case strings.HasPrefix(trimmed, "ssh_command:"):
				if v := extractValue(trimmed[len("ssh_command:"):]); v != "" {
					current.SSHCommand = v
				}
			case strings.HasPrefix(trimmed, "codespace:"):
				current.Codespace = extractValue(trimmed[len("codespace:"):])
			case strings.HasPrefix(trimmed, "container:"):
				current.Container = extractValue(trimmed[len("container:"):])
			case strings.HasPrefix(trimmed, "user:"):
				current.User = extractValue(trimmed[len("user:"):])
			}
		}
	}
	if current != nil {
		cfg.Remotes = append(cfg.Remotes, *current)
	}
	return cfg, nil
}

func extractValue(s string) string {
	s = strings.TrimSpace(s)
	return strings.Trim(s, `"'`)
}
