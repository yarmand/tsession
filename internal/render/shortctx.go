package render

import (
	"net/url"
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
	legendField    string // single-line with \n escapes: "A:repo\nB:repo"
}

// BuildShortContext assigns origin letters in order of first appearance.
func BuildShortContext(all []sessions.Session) ShortContext {
	originToLetter := make(map[string]string)
	var legend []string
	for _, s := range all {
		origin := strings.TrimSpace(s.Repository)
		key := originKey(origin)
		if key == "" {
			continue
		}
		if _, ok := originToLetter[key]; ok {
			continue
		}
		letter := string(rune('A' + len(originToLetter)))
		originToLetter[key] = letter
		legend = append(legend, letter+":"+originShortName(origin))
	}
	return ShortContext{originToLetter: originToLetter, legendField: strings.Join(legend, "\\n")}
}

// LegendField returns a single-line legend suitable for embedding as an fzf field.
func (c ShortContext) LegendField() string { return c.legendField }

func (c ShortContext) letterForOrigin(origin string) string {
	origin = strings.TrimSpace(origin)
	key := originKey(origin)
	if key == "" {
		return ""
	}
	if c.originToLetter == nil {
		return ""
	}
	if l, ok := c.originToLetter[key]; ok {
		return l
	}
	return ""
}

// OriginShortName is a safe, human-friendly name for a git origin string.
// Examples:
// - ps://github.com/yarmand/tsession.git -> tsession
// - git@github.com:yarmand/tsession.git  -> tsession
func OriginShortName(origin string) string { return originShortName(origin) }

func originKey(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return ""
	}

	// scp-like form: git@github.com:org/repo.git
	if strings.Contains(origin, "@") && strings.Contains(origin, ":") && !strings.Contains(origin, "://") {
		at := strings.LastIndex(origin, "@")
		after := origin
		if at >= 0 {
			after = origin[at+1:]
		}
		parts := strings.SplitN(after, ":", 2)
		if len(parts) == 2 {
			host := strings.ToLower(strings.TrimSpace(parts[0]))
			path := strings.TrimPrefix(strings.TrimSpace(parts[1]), "/")
			path = strings.TrimSuffix(path, ".git")
			if host != "" && path != "" {
				return host + "/" + path
			}
		}
	}

	// URL form: https://host/org/repo(.git)
	if strings.Contains(origin, "://") {
		if u, err := url.Parse(origin); err == nil {
			host := strings.ToLower(strings.TrimSpace(u.Host))
			path := strings.TrimPrefix(strings.TrimSpace(u.Path), "/")
			path = strings.TrimSuffix(path, ".git")
			if host != "" && path != "" {
				return host + "/" + path
			}
		}
	}

	// Fallback: stable key as-is (minus trailing .git and /)
	s := strings.TrimRight(origin, "/")
	s = strings.TrimSuffix(s, ".git")
	return s
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
	if strings.TrimSpace(s.Repository) != "" {
		return originShortName(s.Repository)
	}
	return "-"
}

// buildShortContext is a package-private alias used by tests.
func buildShortContext(all []sessions.Session) ShortContext { return BuildShortContext(all) }
