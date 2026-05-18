// internal/lsofutil/lsof.go
package lsofutil

import (
	"bytes"
	"os/exec"
	"strings"
)

// IsFileLocked returns true when some process has an open handle to path.
// Implemented by shelling out to `lsof <path>`; exit 0 = locked, 1 = not.
//
// Prefer LockedSet for checking many files — it forks lsof once instead of N times.
func IsFileLocked(path string) bool {
	cmd := exec.Command("lsof", "--", path)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// LockedSet runs a single `lsof -F n -- <paths...>` and returns the subset
// of input paths for which lsof reported at least one open handle. Paths
// absent from the result map are NOT locked.
//
// Returns an empty (non-nil) map when paths is empty. lsof exit code 1
// means "no matching files open" and is treated as success with an empty
// result; any other error is returned.
func LockedSet(paths []string) (map[string]bool, error) {
	out := make(map[string]bool, len(paths))
	if len(paths) == 0 {
		return out, nil
	}
	args := append([]string{"-F", "n", "--"}, paths...)
	cmd := exec.Command("lsof", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			// No matching open files; lsof exits 1.
			return out, nil
		}
		return nil, err
	}
	want := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		want[p] = struct{}{}
	}
	for _, p := range parseLockedNames(buf.String()) {
		if _, ok := want[p]; ok {
			out[p] = true
		}
	}
	return out, nil
}

// parseLockedNames extracts the file names from `lsof -F n` output.
// Each line starts with a one-char field identifier; `n` lines carry names.
// Other lines (p<pid>, f<fd>, etc.) are ignored.
func parseLockedNames(s string) []string {
	var names []string
	for _, line := range strings.Split(s, "\n") {
		if len(line) > 1 && line[0] == 'n' {
			names = append(names, line[1:])
		}
	}
	return names
}

