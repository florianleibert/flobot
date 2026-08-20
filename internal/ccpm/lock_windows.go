//go:build windows

package ccpm

import (
	"os"
	"syscall"
)

// lockFile takes an exclusive advisory lock on f.
func lockFile(f *os.File) error {
	return syscall.LockFileEx(syscall.Handle(f.Fd()), syscall.LOCKFILE_EXCLUSIVE_LOCK, 0, nil)
}

// unlockFile releases the advisory lock on f.
func unlockFile(f *os.File) error {
	return syscall.UnlockFileEx(syscall.Handle(f.Fd()), 0, nil)
}
