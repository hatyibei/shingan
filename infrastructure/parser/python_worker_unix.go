//go:build !windows

package parser

import (
	"os/exec"
	"syscall"
)

// setProcessGroup configures cmd so its subprocess (and any langgraph-
// spawned children) sit in their own process group. We use this so
// killProcessGroup can SIGKILL the whole tree on Call() timeout.
//
// Windows has no equivalent POSIX process-group concept; the windows
// build of this helper is a no-op.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the whole process group identified
// by pid. Returns the syscall error so the caller can decide whether to
// fall back to cmd.Process.Kill().
func killProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
