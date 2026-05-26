package render

import (
	"path/filepath"
	"strings"

	"github.com/yarma/tsession/internal/sessions"
)

// ShortContext holds per-render derived data used by --short rendering.
//
// It is computed from the full session list so we can assign stable
// origin letters (A, B, ...) within a single list render.
type ShortContext struct {
	originToLetter map[string]string
	legendField    string // single-line: "A:repo|B:repo"
}

// BuildShortContext assigns origin letters in order of first appearance.
func BuildShortContext(all []sessions.Session) ShortContext {
	originToLetter := make(map[string]string)
	var legend []string
	for _, s := range all {
		origin := strings.TrimSpace(s.Repository)
		if origin == "" {
			continue
		}
		if _, ok := originToLetter[origin]; ok {
			continue
		}
		letter := string(rune('A' + len(originToLetter)))
		originToLetter[origin] = letter
		legend = append(legend, letter+":"+originShortName(origin))
	}
	return ShortContext{originToLetter: originToLetter, legendField: strings.Join(legend, "|")}
}

// LegendField returns a single-line legend suitable for embedding as an fzf field.
func (c ShortContext) LegendField() string { return c.legendField }

func (c ShortContext) letterForOrigin(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return "-"
	}
	if c.originToLetter == nil {
		return "-"
	}
	if l, ok := c.originToLetter[origin]; ok {
		return l
	}
	return "-"
}

// OriginShortName is a safe, human-friendly name for a git origin string.
// Examples:
// - ps://github.com/yarmand/tsession.git -> tsession
// - git@github.com:yarmand/tsession.git  -> tsession
func OriginShortName(origin string) string { return originShortName(origin) }

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
	if strings.TrimSpace(s.Repository) != "" {
		return originShortName(s.Repository)
	}
	return "-"
}

// buildShortContext is a package-private alias used by tests.
func buildShortContext(all []sessions.Session) ShortContext { return BuildShortContext(all) }
