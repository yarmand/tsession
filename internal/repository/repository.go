package repository

import (
	"net/url"
	"strings"
)

// Normalize returns a stable repository identity for persisted alias keys.
//
// It treats HTTPS and scp-like SSH forms for the same host/path as identical
// and falls back to a trimmed raw repository string when parsing fails.
func Normalize(repository string) string {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return ""
	}

	if strings.Contains(repository, "@") && strings.Contains(repository, ":") && !strings.Contains(repository, "://") {
		at := strings.LastIndex(repository, "@")
		after := repository
		if at >= 0 {
			after = repository[at+1:]
		}
		parts := strings.SplitN(after, ":", 2)
		if len(parts) == 2 {
			host := strings.ToLower(strings.TrimSpace(parts[0]))
			path := strings.TrimSpace(parts[1])
			path = strings.TrimPrefix(path, "/")
			path = strings.TrimSuffix(path, ".git")
			path = strings.TrimRight(path, "/")
			if host != "" && path != "" {
				return host + "/" + path
			}
		}
	}

	if strings.Contains(repository, "://") {
		if u, err := url.Parse(repository); err == nil {
			host := strings.ToLower(strings.TrimSpace(u.Host))
			path := strings.TrimSpace(u.Path)
			path = strings.TrimPrefix(path, "/")
			path = strings.TrimSuffix(path, ".git")
			path = strings.TrimRight(path, "/")
			if host != "" && path != "" {
				return host + "/" + path
			}
		}
	}

	repository = strings.TrimRight(repository, "/")
	repository = strings.TrimSuffix(repository, ".git")
	return repository
}
