package worktree

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

func TestCreateReturnsLastStdoutLine(t *testing.T) {
	dir := t.TempDir()
	configHome = func() (string, error) { return dir, nil }
	path := filepath.Join(dir, "new-worktree.sh")
	stub := "#!/usr/bin/env bash\n" +
		"echo \"progress\" >&2\n" +
		"echo \"\"\n" +
		"echo \"/tmp/fake/$1\"\n"
	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Create("mybranch", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got != "/tmp/fake/mybranch" {
		t.Fatalf("got %q, want /tmp/fake/mybranch", got)
	}
}

func TestCreateErrorsWhenNoPathPrinted(t *testing.T) {
	dir := t.TempDir()
	configHome = func() (string, error) { return dir, nil }
	path := filepath.Join(dir, "new-worktree.sh")
	stub := "#!/usr/bin/env bash\necho \"only stderr\" >&2\n"
	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Create("b", nil); err == nil {
		t.Fatal("expected error when script prints no path")
	}
}

func TestCreateTeesStdoutToLogWriter(t *testing.T) {
	dir := t.TempDir()
	configHome = func() (string, error) { return dir, nil }
	path := filepath.Join(dir, "new-worktree.sh")
	stub := "#!/usr/bin/env bash\n" +
		"echo \"doing work for $1\"\n" +
		"echo \"/tmp/fake/$1\"\n"
	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	var log bytes.Buffer
	got, err := Create("mybranch", &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got != "/tmp/fake/mybranch" {
		t.Fatalf("got %q, want /tmp/fake/mybranch", got)
	}
	out := log.String()
	if !strings.Contains(out, "doing work for mybranch") {
		t.Errorf("verbose log missing script stdout, got:\n%s", out)
	}
	if !strings.Contains(out, "new-worktree.sh") || !strings.Contains(out, "mybranch") {
		t.Errorf("verbose log missing command line, got:\n%s", out)
	}
}
