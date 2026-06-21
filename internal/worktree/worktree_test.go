package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureScriptWritesDefaultWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	configHome = func() (string, error) { return dir, nil }

	if err := EnsureScript(); err != nil {
		t.Fatalf("EnsureScript: %v", err)
	}

	path := filepath.Join(dir, "new-worktree.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if string(data) != defaultScript {
		t.Fatalf("content does not match defaultScript")
	}
}

func TestEnsureScriptPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	configHome = func() (string, error) { return dir, nil }
	path := filepath.Join(dir, "new-worktree.sh")
	if err := os.WriteFile(path, []byte("custom\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureScript(); err != nil {
		t.Fatalf("EnsureScript: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "custom\n" {
		t.Fatalf("existing script overwritten: %q", string(data))
	}
}
