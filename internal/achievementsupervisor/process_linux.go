//go:build linux

package achievementsupervisor

import (
	"os"
	"os/exec"
	"syscall"
)

func configureChildProcess(command *exec.Cmd, terminateWithParent bool) {
	if terminateWithParent {
		command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	}
}

func terminationSignal() os.Signal {
	return syscall.SIGTERM
}
