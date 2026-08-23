//go:build unix

package git

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the child in its own process group, so its descendants
// can be killed along with it.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup kills the whole group. The negative pid is what makes the
// signal reach ssh and any credential helper git started, not just git.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
