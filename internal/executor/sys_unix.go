//go:build unix

package executor

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// CancelGrace is how long a cancelled child gets to exit after SIGTERM before
// the whole process group is killed.
//
// SIGKILL alone leaves a child no chance to flush a partial result, remove a
// temp file, or release a lock — which is how a cancelled routine leaves
// something half-done. SIGTERM first, then a bounded wait, then SIGKILL.
const CancelGrace = 3 * time.Second

func setPgid(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup asks the whole process group to stop, then kills it if it does not.
//
// The group is signalled rather than the process because flocode spawns tool
// children: killing only the parent orphans them, and an orphaned tool keeps
// writing to the workspace after the run is reported cancelled.
func killGroup(p *os.Process) {
	if p == nil || p.Pid <= 0 {
		return
	}
	// Ask first. A child that handles SIGTERM exits cleanly within the grace.
	_ = syscall.Kill(-p.Pid, syscall.SIGTERM)
	_ = p.Signal(syscall.SIGTERM)

	deadline := time.Now().Add(CancelGrace)
	for time.Now().Before(deadline) {
		// Signal 0 probes for existence without delivering anything.
		if err := syscall.Kill(-p.Pid, 0); err != nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	// Still there: take it down.
	_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
	_ = p.Kill()
}
