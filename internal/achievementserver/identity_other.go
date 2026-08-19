//go:build !linux

package achievementserver

func runningAsRoot() bool {
	return false
}
