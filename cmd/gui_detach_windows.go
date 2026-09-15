//go:build windows

package cmd

import (
	"os/exec"
	"syscall"
)

const windowsDetachedProcess = 0x00000008

func detachGUICommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | windowsDetachedProcess,
	}
}
