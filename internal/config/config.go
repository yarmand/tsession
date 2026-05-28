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
	Host       string
	CopilotDir string
}

type Config struct {
	Remotes []Remote
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
			}
			continue
		}

		if current != nil && indent >= 4 {
			switch {
			case strings.HasPrefix(trimmed, "host:"):
				current.Host = extractValue(trimmed[len("host:"):])
			case strings.HasPrefix(trimmed, "copilot_dir:"):
				if v := extractValue(trimmed[len("copilot_dir:"):]); v != "" {
					current.CopilotDir = v
				}
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
