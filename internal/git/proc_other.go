//go:build !unix

package git

import "os/exec"

// setProcessGroup is a no-op where process groups do not exist.
func setProcessGroup(_ *exec.Cmd) {}

// killProcessGroup falls back to killing just the child.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
