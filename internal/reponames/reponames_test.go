package reponames

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadGetSetAndClear(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const repo = "git@github.com:org/repo.git"
	const alias = "repo-alias"

	m, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("Load() = %#v, want empty map", m)
	}

	got, err := Get(repo)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "" {
		t.Fatalf("Get() = %q, want empty", got)
	}

	if err := Set(repo, alias); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got, err = Get("https://github.com/org/repo")
	if err != nil {
		t.Fatalf("Get() after Set error = %v", err)
	}
	if got != alias {
		t.Fatalf("Get() after Set = %q, want %q", got, alias)
	}

	m, err = Load()
	if err != nil {
		t.Fatalf("Load() after Set error = %v", err)
	}
	want := map[string]string{"github.com/org/repo": alias}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("Load() after Set = %#v, want %#v", m, want)
	}

	if err := Set("https://github.com/org/repo.git", ""); err != nil {
		t.Fatalf("Set(clear) error = %v", err)
	}
	got, err = Get(repo)
	if err != nil {
		t.Fatalf("Get() after clear error = %v", err)
	}
	if got != "" {
		t.Fatalf("Get() after clear = %q, want empty", got)
	}
}

func TestLoadMalformedJSONReturnsEmptyMap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".tsession")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "repo-names.json"), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	m, err := Load()
	if err != nil {
		t.Fatalf("Load() malformed error = %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("Load() malformed = %#v, want empty map", m)
	}

	got, err := Get("https://github.com/org/repo")
	if err != nil {
		t.Fatalf("Get() malformed error = %v", err)
	}
	if got != "" {
		t.Fatalf("Get() malformed = %q, want empty", got)
	}
}
