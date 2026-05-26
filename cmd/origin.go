package cmd

import (
	"os/exec"
	"strings"

	"github.com/yarma/tsession/internal/sessions"
)

// enrichOrigins fills Session.Repository from git remote.origin.url when it is
// missing. This enables origin-letter rendering for local worktrees.
//
// Best-effort: errors are ignored and Repository is left empty.
func enrichOrigins(all []sessions.Session) {
	cache := make(map[string]string)
	for i := range all {
		if strings.TrimSpace(all[i].Repository) != "" {
			continue
		}
		cwd := strings.TrimSpace(all[i].CWD)
		if cwd == "" {
			continue
		}
		if v, ok := cache[cwd]; ok {
			all[i].Repository = v
			continue
		}
		v := gitOriginURL(cwd)
		cache[cwd] = v
		if v != "" {
			all[i].Repository = v
		}
	}
}

func gitOriginURL(cwd string) string {
	out, err := exec.Command("git", "-C", cwd, "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
