package cmd

import (
	"os"
	"os/exec"
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

func TestGUILaunchCommandUsesOpenForDarwin(t *testing.T) {
	cmd := guiLaunchCommand("darwin", "/Applications/TSession.app")
	if got, want := filepath.Base(cmd.Path), "open"; got != want {
		t.Fatalf("command path basename = %q, want %q", got, want)
	}
	if len(cmd.Args) != 2 || cmd.Args[1] != "/Applications/TSession.app" {
		t.Fatalf("command args = %#v, want open /Applications/TSession.app", cmd.Args)
	}
}

func TestGUILaunchCommandDetachesNonDarwinProcess(t *testing.T) {
	cmd := guiLaunchCommand("linux", "/tmp/tsession-gui")
	if cmd.Path != "/tmp/tsession-gui" {
		t.Fatalf("command path = %q, want /tmp/tsession-gui", cmd.Path)
	}
	assertDetachedGUICommand(t, cmd)
}

func assertDetachedGUICommand(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd.SysProcAttr == nil {
		t.Fatal("expected non-darwin GUI command to have SysProcAttr for detached launch")
	}
}
