// internal/lsofutil/lsof.go
package lsofutil

import (
	"os/exec"
)

// IsFileLocked returns true when some process has an open handle to path.
// Implemented by shelling out to `lsof <path>`; exit 0 = locked, 1 = not.
func IsFileLocked(path string) bool {
	cmd := exec.Command("lsof", path)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}
