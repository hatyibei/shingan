//go:build windows

package parser

import "os/exec"

// setProcessGroup is a no-op on Windows. POSIX process groups don't
// exist there; the Windows kernel has CREATE_NEW_PROCESS_GROUP /
// JobObjects but neither is needed for our use case (langgraph
// shim runs as a single Python process and cmd.Process.Kill() is
// enough on timeout).
func setProcessGroup(_ *exec.Cmd) {}

// killProcessGroup is a stub that always errors so the caller falls
// back to cmd.Process.Kill(). On Windows, killing the parent reliably
// terminates the langgraph python process; we only lose the ability to
// reap orphaned grandchildren, which langgraph does not spawn.
func killProcessGroup(_ int) error {
	return errProcessGroupNotSupported
}
