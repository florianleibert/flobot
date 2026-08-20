//go:build unix

package executor

import (
	"os"
	"os/exec"
	"syscall"
)

func setPgid(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killGroup(p *os.Process) {
	if p == nil || p.Pid <= 0 {
		return
	}
	_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
	_ = p.Kill()
}
