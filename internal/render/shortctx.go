package render

import (
	"path/filepath"
	"strings"

	"github.com/yarma/tsession/internal/repository"
	"github.com/yarma/tsession/internal/sessions"
)

type shortRepositoryDisplay struct {
	label   string
	compact string
}

// ShortContext holds per-render derived data used by --short rendering.
//
// It is computed from the full session list so repository labels stay stable
// across sections and reloads.
type ShortContext struct {
	repositoryDisplay map[string]shortRepositoryDisplay
	legendField       string
}

// BuildShortContext assigns repository labels without aliases.
func BuildShortContext(all []sessions.Session) ShortContext {
	return BuildShortContextWithAliases(all, nil)
}

// BuildShortContextWithAliases assigns repository labels using the provided
// alias map keyed by normalized repository identity.
func BuildShortContextWithAliases(all []sessions.Session, aliases map[string]string) ShortContext {
	repositoryDisplay := make(map[string]shortRepositoryDisplay)
	for _, s := range all {
		key := originKey(s.Repository)
		if key == "" {
			continue
		}
		if _, ok := repositoryDisplay[key]; ok {
			continue
		}

		label := originShortName(s.Repository)
		if alias := strings.TrimSpace(aliases[key]); alias != "" {
			label = alias
		}

		repositoryDisplay[key] = shortRepositoryDisplay{
			label:   label,
			compact: truncateFirstRunes(label, 10),
		}
	}

	return ShortContext{
		repositoryDisplay: repositoryDisplay,
		legendField:       "",
	}
}

// LegendField returns a single-line legend suitable for embedding as an fzf field.
func (c ShortContext) LegendField() string { return c.legendField }

func (c ShortContext) repositoryDisplayFor(origin string) (shortRepositoryDisplay, bool) {
	key := originKey(origin)
	if key == "" || c.repositoryDisplay == nil {
		return shortRepositoryDisplay{}, false
	}
	display, ok := c.repositoryDisplay[key]
	return display, ok
}

// OriginShortName is a safe, human-friendly name for a git origin string.
// Examples:
// - ps://github.com/yarmand/tsession.git -> tsession
// - git@github.com:yarmand/tsession.git  -> tsession
func OriginShortName(origin string) string { return originShortName(origin) }

func originKey(origin string) string {
	return repository.Normalize(origin)
}

func originShortName(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return "-"
	}
	s := strings.TrimRight(origin, "/")

	// Handle scp-like form: git@github.com:org/repo.git
	if i := strings.LastIndex(s, ":"); i >= 0 && strings.Contains(s[:i], "@") {
		s = s[i+1:]
	}

	base := filepath.Base(s)
	base = strings.TrimSuffix(base, ".git")
	if base == "" {
		return "-"
	}
	return base
}

func shortWorktreeName(s sessions.Session) string {
	if strings.TrimSpace(s.CWD) != "" {
		return filepath.Base(s.CWD)
	}
	if s.Name != "" {
		return s.Name
	}
	if strings.TrimSpace(s.Repository) != "" {
		return originShortName(s.Repository)
	}
	return "-"
}

func truncateFirstRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// buildShortContext is a package-private alias used by tests.
func buildShortContext(all []sessions.Session) ShortContext { return BuildShortContext(all) }
