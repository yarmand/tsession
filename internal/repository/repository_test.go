package repository

import "testing"

func TestNormalizeEquivalentRepositoryForms(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://github.com/org/repo", "github.com/org/repo"},
		{"https://github.com/org/repo.git", "github.com/org/repo"},
		{"git@github.com:org/repo.git", "github.com/org/repo"},
		{"  git@github.com:org/repo.git  ", "github.com/org/repo"},
		{"gh/org/repo", "gh/org/repo"},
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.want {
			t.Fatalf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeMissingRepository(t *testing.T) {
	if got := Normalize("   "); got != "" {
		t.Fatalf("Normalize(empty) = %q, want empty", got)
	}
}
