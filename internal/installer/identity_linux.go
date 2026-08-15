//go:build linux

package installer

import (
	"os"
	"syscall"
)

func effectiveUID() int {
	return os.Geteuid()
}

func lockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unlockFile(file *os.File) {
	if file != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}
}
