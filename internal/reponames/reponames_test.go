package reponames

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/repository"
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

func TestSetPropagatesUnexpectedLoadError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sentinel := errors.New("boom")

	oldReadFile := readFile
	oldWriteFile := writeFile
	oldRenameFile := renameFile
	t.Cleanup(func() {
		readFile = oldReadFile
		writeFile = oldWriteFile
		renameFile = oldRenameFile
	})

	readFile = func(string) ([]byte, error) {
		return nil, sentinel
	}

	wrote := false
	writeFile = func(string, []byte, os.FileMode) error {
		wrote = true
		return nil
	}
	renameFile = func(string, string) error {
		t.Fatal("renameFile() called after unexpected load error")
		return nil
	}

	err := Set("https://github.com/org/repo", "repo-alias")
	if !errors.Is(err, sentinel) {
		t.Fatalf("Set() error = %v, want %v", err, sentinel)
	}
	if wrote {
		t.Fatal("Set() wrote alias file after unexpected load error")
	}
}

func TestSetTreatsMalformedJSONAsEmptyMap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".tsession")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := Set("https://github.com/org/repo", "repo-alias"); err != nil {
		t.Fatalf("Set() malformed error = %v", err)
	}

	got, err := Get("git@github.com:org/repo.git")
	if err != nil {
		t.Fatalf("Get() malformed after Set error = %v", err)
	}
	if got != "repo-alias" {
		t.Fatalf("Get() malformed after Set = %q, want %q", got, "repo-alias")
	}
}

func TestSetPreservesConcurrentProcessUpdates(t *testing.T) {
	if os.Getenv("TS_REPONAMES_CHILD") == "1" {
		if err := os.Setenv("HOME", os.Getenv("TS_REPONAMES_HOME")); err != nil {
			t.Fatalf("Setenv(HOME) error = %v", err)
		}
		if err := os.WriteFile(os.Getenv("TS_REPONAMES_READY"), []byte("ready"), 0o644); err != nil {
			t.Fatalf("WriteFile(ready) error = %v", err)
		}
		if err := Set(os.Getenv("TS_REPONAMES_REPO"), os.Getenv("TS_REPONAMES_ALIAS")); err != nil {
			t.Fatalf("child Set() error = %v", err)
		}
		return
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	d, err := dir()
	if err != nil {
		t.Fatalf("dir() error = %v", err)
	}

	unlock, err := lock(filepath.Join(d, lockFileName))
	if err != nil {
		t.Fatalf("lock() error = %v", err)
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()

	const (
		childRepo  = "git@github.com:org/repo-one.git"
		childAlias = "one"
		mainRepo   = "https://github.com/org/repo-two"
		mainAlias  = "two"
	)

	ready := filepath.Join(home, "child-ready")
	cmd := exec.Command(os.Args[0], "-test.run=TestSetPreservesConcurrentProcessUpdates$")
	cmd.Env = append(os.Environ(),
		"TS_REPONAMES_CHILD=1",
		"TS_REPONAMES_HOME="+home,
		"TS_REPONAMES_READY="+ready,
		"TS_REPONAMES_REPO="+childRepo,
		"TS_REPONAMES_ALIAS="+childAlias,
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	waitForFile(t, ready)

	select {
	case err := <-done:
		t.Fatalf("child Set() completed while lock held: %v\n%s", err, output.String())
	case <-time.After(200 * time.Millisecond):
	}

	aliases := map[string]string{repository.Normalize(mainRepo): mainAlias}
	data, err := json.MarshalIndent(aliases, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(d, fileName), data, 0o644); err != nil {
		t.Fatalf("WriteFile(alias file) error = %v", err)
	}

	unlock()
	locked = false

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child Set() wait error = %v\n%s", err, output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for child Set() to finish")
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := map[string]string{
		repository.Normalize(childRepo): childAlias,
		repository.Normalize(mainRepo):  mainAlias,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() after concurrent Set = %#v, want %#v", got, want)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", path)
}
