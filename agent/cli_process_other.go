//go:build !windows

package agent

import "os/exec"

func configureCLIProcess(cmd *exec.Cmd) {
	_ = cmd
}
