package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocateGUIAppFindsBundleNextToExecutableOnDarwin(t *testing.T) {
	exeDir := t.TempDir()
	appPath := filepath.Join(exeDir, "TSession.app")
	if err := os.MkdirAll(appPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := locateGUIApp("darwin", exeDir, t.TempDir())
	if err != nil {
		t.Fatalf("locateGUIApp: %v", err)
	}
	if got != appPath {
		t.Fatalf("locateGUIApp = %q, want %q", got, appPath)
	}
}

func TestLocateGUIAppFindsBundleInApplicationsOnDarwin(t *testing.T) {
	home := t.TempDir()
	appsDir := filepath.Join(home, "Applications")
	appPath := filepath.Join(appsDir, "TSession.app")
	if err := os.MkdirAll(appPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := locateGUIApp("darwin", t.TempDir(), home)
	if err != nil {
		t.Fatalf("locateGUIApp: %v", err)
	}
	if got != appPath {
		t.Fatalf("locateGUIApp = %q, want %q", got, appPath)
	}
}

func TestLocateGUIAppPrefersExecutableAdjacentOverHomeApplications(t *testing.T) {
	exeDir := t.TempDir()
	home := t.TempDir()
	adjacent := filepath.Join(exeDir, "TSession.app")
	inHome := filepath.Join(home, "Applications", "TSession.app")
	for _, p := range []string{adjacent, inHome} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	got, err := locateGUIApp("darwin", exeDir, home)
	if err != nil {
		t.Fatalf("locateGUIApp: %v", err)
	}
	if got != adjacent {
		t.Fatalf("locateGUIApp = %q, want the executable-adjacent path %q", got, adjacent)
	}
}

func TestLocateGUIAppErrorsWithSearchedPathsWhenNotFound(t *testing.T) {
	exeDir := t.TempDir()
	home := t.TempDir()

	_, err := locateGUIApp("darwin", exeDir, home)
	if err == nil {
		t.Fatal("expected an error when no app bundle exists anywhere searched")
	}
	msg := err.Error()
	for _, want := range []string{
		filepath.Join(exeDir, "TSession.app"),
		filepath.Join(home, "Applications", "TSession.app"),
		filepath.Join("/Applications", "TSession.app"),
	} {
		if !contains(msg, want) {
			t.Fatalf("error message %q does not mention searched path %q", msg, want)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
