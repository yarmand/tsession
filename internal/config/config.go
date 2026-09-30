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
	Name        string
	Active      bool
	Type        string // "ssh" (default), "codespace", "devcontainer"
	Host        string
	CopilotDir  string
	SSHCommand  string // custom override — defaults based on Type
	Codespace   string // codespace name (type=codespace)
	Container   string // container name (type=devcontainer)
	User        string // user for docker exec (type=devcontainer)
	CodeCommand string // path to the `code` (VS Code CLI) binary on this remote; overrides Config.CodeCommand
}

type Config struct {
	Remotes []Remote

	// CodeCommand is the default path to the `code` (VS Code CLI) binary
	// used to launch `code serve-web`. Empty means resolve `code` from
	// PATH (locally via exec.LookPath, remotely via
	// shellutil.CodeResolverCommand). A remote may override this with its
	// own code_command.
	CodeCommand string

	// AgentCommand is the shell command `tsession new` runs inside the new
	// tmux session to start the agent (e.g. "copilot", "pi",
	// "agency copilot --hub"). Empty means use DefaultAgentCommand. The
	// `--cmd` flag to `tsession new` overrides this per invocation.
	AgentCommand string
}

// DefaultAgentCommand is the command `tsession new` starts in the session when
// neither the `--cmd` flag nor config's agent_command is set.
const DefaultAgentCommand = "copilot"

// AgentCommandOrDefault returns the configured AgentCommand, or
// DefaultAgentCommand when it is empty (including a nil receiver).
func (c *Config) AgentCommandOrDefault() string {
	if c != nil && strings.TrimSpace(c.AgentCommand) != "" {
		return c.AgentCommand
	}
	return DefaultAgentCommand
}

// CodeBinary returns the `code` binary path to use for this remote: its own
// CodeCommand override if set, otherwise the top-level default from cfg (may
// be empty, meaning "resolve from PATH").
func (r Remote) CodeBinary(cfg *Config) string {
	if r.CodeCommand != "" {
		return r.CodeCommand
	}
	if cfg != nil {
		return cfg.CodeCommand
	}
	return ""
}

// Endpoint returns the configured host/container identifier used to connect
// to this remote, falling back to its display name.
func (r Remote) Endpoint() string {
	switch r.Type {
	case "codespace":
		if r.Codespace != "" {
			return r.Codespace
		}
	case "devcontainer":
		if r.Container != "" {
			return r.Container
		}
	default:
		if r.Host != "" {
			return r.Host
		}
		if r.SSHCommand != "" {
			return r.SSHCommand
		}
	}
	return r.Name
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
// We avoid a YAML dependency since the format is simple and stable. A
// top-level `code_command:` scalar (indent 0) sets Config.CodeCommand; a
// nested `code_command:` under a remote entry overrides it for that remote
// only (see Remote.CodeBinary).
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

		// Top-level scalars (indent 0) apply to Config itself, not the
		// current remote list entry.
		if indent == 0 && strings.HasPrefix(trimmed, "code_command:") {
			cfg.CodeCommand = extractValue(trimmed[len("code_command:"):])
			continue
		}

		if indent == 0 && strings.HasPrefix(trimmed, "agent_command:") {
			cfg.AgentCommand = extractValue(trimmed[len("agent_command:"):])
			continue
		}

		if strings.HasPrefix(trimmed, "- name:") {
			if current != nil {
				cfg.Remotes = append(cfg.Remotes, *current)
			}
			current = &Remote{
				Name:       extractValue(trimmed[len("- name:"):]),
				Active:     true,
				CopilotDir: defaultCopilotDir,
				Type:       "ssh",
			}
			continue
		}

		if current != nil && indent >= 4 {
			switch {
			case strings.HasPrefix(trimmed, "host:"):
				current.Host = extractValue(trimmed[len("host:"):])
			case strings.HasPrefix(trimmed, "active:"):
				v := strings.ToLower(extractValue(trimmed[len("active:"):]))
				current.Active = v != "false"
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
			case strings.HasPrefix(trimmed, "code_command:"):
				current.CodeCommand = extractValue(trimmed[len("code_command:"):])
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
