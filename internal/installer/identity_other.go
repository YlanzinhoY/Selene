//go:build !linux

package installer

import "os"

func effectiveUID() int {
	return -1
}

func lockFile(_ *os.File) error {
	return nil
}

func unlockFile(_ *os.File) {}
