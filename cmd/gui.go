package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var runtimeGOOS = runtime.GOOS

// Gui runs `tsession gui`: it locates the separately-built native GUI
// application and launches it. This subcommand does not build or embed Wails
// itself; it only finds the app bundle/binary and starts it.
func Gui(args []string) error {
	fs := flag.NewFlagSet("gui", flag.ExitOnError)
	_ = fs.Parse(args)

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("locate home directory: %w", err)
	}

	appPath, err := locateGUIApp(goos(), filepath.Dir(exePath), homeDir)
	if err != nil {
		return err
	}

	return launchGUIApp(goos(), appPath)
}

func goos() string { return runtimeGOOS }

func locateGUIApp(goos string, exeDir string, homeDir string) (string, error) {
	candidates := guiAppCandidates(goos, exeDir, homeDir)
	for _, candidate := range candidates {
		if exists(candidate) {
			return candidate, nil
		}
	}

	var b strings.Builder
	b.WriteString("could not find the tsession native GUI app; searched:\n")
	for _, candidate := range candidates {
		b.WriteString("  - ")
		b.WriteString(candidate)
		b.WriteByte('\n')
	}
	b.WriteString("build it with `wails build` in gui/, or install TSession")
	if goos == "darwin" {
		b.WriteString(".app in /Applications")
	}
	return "", fmt.Errorf("%s", b.String())
}

func guiAppCandidates(goos string, exeDir string, homeDir string) []string {
	switch goos {
	case "darwin":
		return []string{
			filepath.Join(exeDir, "TSession.app"),
			filepath.Join(homeDir, "Applications", "TSession.app"),
			filepath.Join("/Applications", "TSession.app"),
		}
	case "windows":
		return []string{
			filepath.Join(exeDir, "TSession.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "TSession", "TSession.exe"),
		}
	default:
		candidates := []string{
			filepath.Join(exeDir, "TSession"),
			filepath.Join(exeDir, "tsession-gui"),
		}
		if pathEnv := os.Getenv("PATH"); pathEnv != "" {
			for _, dir := range filepath.SplitList(pathEnv) {
				candidates = append(candidates, filepath.Join(dir, "TSession"))
				candidates = append(candidates, filepath.Join(dir, "tsession-gui"))
			}
		}
		return candidates
	}
}

func exists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular()
}

func launchGUIApp(goos string, appPath string) error {
	cmd := guiLaunchCommand(goos, appPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch %s: %w", appPath, err)
	}
	return nil
}

func guiLaunchCommand(goos string, appPath string) *exec.Cmd {
	switch goos {
	case "darwin":
		return exec.Command("open", appPath)
	default:
		cmd := exec.Command(appPath)
		detachGUICommand(cmd)
		return cmd
	}
}
