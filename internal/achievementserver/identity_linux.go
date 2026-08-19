//go:build linux

package achievementserver

import "os"

func runningAsRoot() bool {
	return os.Geteuid() == 0
}
