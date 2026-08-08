package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/yarma/tsession/internal/sessions"
)

func TestRenameRepositoryAssignsAliasFromArgs(t *testing.T) {
	oldFindSession := findSessionFn
	oldStdout := renameStdout
	t.Cleanup(func() {
		findSessionFn = oldFindSession
		renameStdout = oldStdout
	})

	findSessionFn = func(id string) *sessions.Session {
		if id != "session-1" {
			t.Fatalf("findSessionFn id = %q, want session-1", id)
		}
		return &sessions.Session{ID: id, Repository: "git@github.com:org/repo.git"}
	}

	var output bytes.Buffer
	renameStdout = &output

	t.Setenv("HOME", t.TempDir())

	if err := RenameRepository([]string{"session-1", "My", "Repo"}); err != nil {
		t.Fatalf("RenameRepository() error = %v", err)
	}

	got, err := loadRepositoryAlias("https://github.com/org/repo")
	if err != nil {
		t.Fatalf("loadRepositoryAlias() error = %v", err)
	}
	if got != "My Repo" {
		t.Fatalf("alias = %q, want %q", got, "My Repo")
	}
	if got := output.String(); !strings.Contains(got, "Repository renamed to: My Repo") {
		t.Fatalf("output = %q, want rename confirmation", got)
	}
}

func TestRenameRepositoryInteractiveEmptyInputClearsAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := storeRepositoryAlias("https://github.com/org/repo", "Existing"); err != nil {
		t.Fatalf("storeRepositoryAlias(setup) error = %v", err)
	}

	oldFindSession := findSessionFn
	oldStdin := renameStdin
	oldStdout := renameStdout
	t.Cleanup(func() {
		findSessionFn = oldFindSession
		renameStdin = oldStdin
		renameStdout = oldStdout
	})

	findSessionFn = func(id string) *sessions.Session {
		return &sessions.Session{
			ID:         id,
			Repository: "https://github.com/org/repo.git",
			Summary:    "Short summary",
		}
	}

	renameStdin = strings.NewReader("   \n")
	var output bytes.Buffer
	renameStdout = &output

	if err := RenameRepository([]string{"session-1"}); err != nil {
		t.Fatalf("RenameRepository() error = %v", err)
	}

	got, err := loadRepositoryAlias("git@github.com:org/repo.git")
	if err != nil {
		t.Fatalf("loadRepositoryAlias() error = %v", err)
	}
	if got != "" {
		t.Fatalf("alias after clear = %q, want empty", got)
	}
	if got := output.String(); !strings.Contains(got, "Alias cleared.") {
		t.Fatalf("output = %q, want clear confirmation", got)
	}
}

func TestRenameRepositoryErrorsWithoutRepositoryIdentity(t *testing.T) {
	oldFindSession := findSessionFn
	t.Cleanup(func() { findSessionFn = oldFindSession })

	findSessionFn = func(id string) *sessions.Session {
		return &sessions.Session{ID: id}
	}

	err := RenameRepository([]string{"session-1", "Alias"})
	if err == nil {
		t.Fatal("RenameRepository() error = nil, want missing repository error")
	}
	if !strings.Contains(err.Error(), "no repository identity") {
		t.Fatalf("RenameRepository() error = %v, want missing repository identity", err)
	}
}

func TestRenameRepositoryPropagatesAliasStoreErrors(t *testing.T) {
	oldFindSession := findSessionFn
	oldLoadAlias := loadRepositoryAlias
	oldStoreAlias := storeRepositoryAlias
	t.Cleanup(func() {
		findSessionFn = oldFindSession
		loadRepositoryAlias = oldLoadAlias
		storeRepositoryAlias = oldStoreAlias
	})

	findSessionFn = func(id string) *sessions.Session {
		return &sessions.Session{ID: id, Repository: "https://github.com/org/repo"}
	}

	sentinel := errors.New("boom")
	loadRepositoryAlias = func(repository string) (string, error) {
		return "", sentinel
	}
	storeRepositoryAlias = func(repository, alias string) error {
		t.Fatal("storeRepositoryAlias() called after load error")
		return nil
	}

	err := RenameRepository([]string{"session-1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("RenameRepository() error = %v, want %v", err, sentinel)
	}
}

func TestRenameRepositoryUsageRequiresSessionID(t *testing.T) {
	err := RenameRepository(nil)
	if err == nil {
		t.Fatal("RenameRepository() error = nil, want usage error")
	}
	if !strings.Contains(err.Error(), "usage: tsession rename-repo <session-id> [alias]") {
		t.Fatalf("RenameRepository() error = %v", err)
	}
}
