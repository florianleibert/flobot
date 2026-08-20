//go:build windows

package executor

import (
	"os"
	"os/exec"
)

func setPgid(cmd *exec.Cmd) {}

func killGroup(p *os.Process) {
	if p == nil {
		return
	}
	_ = p.Kill()
}
