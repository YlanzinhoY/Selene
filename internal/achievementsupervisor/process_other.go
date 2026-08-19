//go:build !linux

package achievementsupervisor

import (
	"os"
	"os/exec"
)

func configureChildProcess(_ *exec.Cmd, _ bool) {}

func terminationSignal() os.Signal {
	return os.Interrupt
}
