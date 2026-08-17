//go:build linux

package plugins

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	steamTerminateGrace = 8 * time.Second
	steamKillGrace      = 2 * time.Second
	steamPollInterval   = 100 * time.Millisecond
)

type steamProcess struct {
	PID  int
	Name string
}

type steamSignalFunc func(pid int, signal syscall.Signal) error

// SteamRunning reports whether the current user owns a Steam client process.
// It never considers another user's processes.
func SteamRunning() bool {
	processes, err := steamProcesses("/proc", os.Geteuid())
	return err == nil && len(processes) > 0
}

// CloseSteam gracefully closes Steam processes owned by the current user.
// SIGKILL is used only after the grace period and only for a process that is
// still identifiable as Steam at signal time.
func CloseSteam(ctx context.Context) error {
	return closeSteamProcesses(
		ctx,
		"/proc",
		os.Geteuid(),
		signalSteamProcess,
		steamTerminateGrace,
		steamKillGrace,
		steamPollInterval,
	)
}

func closeSteamProcesses(
	ctx context.Context,
	procRoot string,
	uid int,
	signal steamSignalFunc,
	terminateGrace time.Duration,
	killGrace time.Duration,
	pollInterval time.Duration,
) error {
	processes, err := steamProcesses(procRoot, uid)
	if err != nil {
		return fmt.Errorf("inspect Steam processes: %w", err)
	}
	if len(processes) == 0 {
		return nil
	}
	if err := signalSteamProcessSet(procRoot, uid, processes, syscall.SIGTERM, signal); err != nil {
		return err
	}
	closed, err := waitForSteamExit(ctx, procRoot, uid, terminateGrace, pollInterval)
	if err != nil {
		return err
	}
	if closed {
		return nil
	}

	remaining, err := steamProcesses(procRoot, uid)
	if err != nil {
		return fmt.Errorf("recheck Steam processes: %w", err)
	}
	if err := signalSteamProcessSet(procRoot, uid, remaining, syscall.SIGKILL, signal); err != nil {
		return err
	}
	closed, err = waitForSteamExit(ctx, procRoot, uid, killGrace, pollInterval)
	if err != nil {
		return err
	}
	if !closed {
		return errors.New("Steam is still running after the close request")
	}
	return nil
}

func steamProcesses(procRoot string, uid int) ([]steamProcess, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}
	var processes []steamProcess
	for _, entry := range entries {
		if !entry.IsDir() || !allDigits(entry.Name()) {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !processOwnedByUser(procRoot, entry.Name(), uid) {
			continue
		}
		name, err := processName(procRoot, entry.Name())
		if err != nil || !isSteamProcessName(name) || processIsZombie(procRoot, entry.Name()) {
			continue
		}
		processes = append(processes, steamProcess{PID: pid, Name: name})
	}
	return processes, nil
}

func signalSteamProcessSet(procRoot string, uid int, processes []steamProcess, signal syscall.Signal, send steamSignalFunc) error {
	for _, process := range processes {
		pid := strconv.Itoa(process.PID)
		if !processOwnedByUser(procRoot, pid, uid) {
			continue
		}
		name, err := processName(procRoot, pid)
		if err != nil || !isSteamProcessName(name) {
			continue
		}
		if err := send(process.PID, signal); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("signal Steam process %d: %w", process.PID, err)
		}
	}
	return nil
}

func waitForSteamExit(ctx context.Context, procRoot string, uid int, timeout, pollInterval time.Duration) (bool, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		processes, err := steamProcesses(procRoot, uid)
		if err != nil {
			return false, fmt.Errorf("recheck Steam processes: %w", err)
		}
		if len(processes) == 0 {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline.C:
			return false, nil
		case <-ticker.C:
		}
	}
}

func signalSteamProcess(pid int, signal syscall.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(signal)
}

func processName(procRoot, pid string) (string, error) {
	data, err := os.ReadFile(filepath.Join(procRoot, pid, "comm"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func processIsZombie(procRoot, pid string) bool {
	data, err := os.ReadFile(filepath.Join(procRoot, pid, "status"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "State:") {
			fields := strings.Fields(line)
			return len(fields) >= 2 && fields[1] == "Z"
		}
	}
	return false
}

func isSteamProcessName(name string) bool {
	return name == "steam" || name == "steamwebhelper"
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func processOwnedByUser(procRoot, pid string, uid int) bool {
	info, err := os.Stat(filepath.Join(procRoot, pid))
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == uid
}
