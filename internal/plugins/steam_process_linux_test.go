//go:build linux

package plugins

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSteamProcessesSelectsOnlyCurrentUserSteamNames(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, "101", "steam")
	writeFakeProcess(t, root, "102", "steamwebhelper")
	writeFakeProcess(t, root, "103", "pressure-vessel")
	if err := os.MkdirAll(filepath.Join(root, "not-a-pid"), 0o700); err != nil {
		t.Fatal(err)
	}

	processes, err := steamProcesses(root, os.Geteuid())
	if err != nil {
		t.Fatal(err)
	}
	if len(processes) != 2 || processes[0].PID != 101 || processes[1].PID != 102 {
		t.Fatalf("Steam processes = %#v", processes)
	}
	processes, err = steamProcesses(root, os.Geteuid()+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(processes) != 0 {
		t.Fatalf("another user's processes were selected: %#v", processes)
	}
}

func TestSteamProcessesIgnoresZombieSteamProcess(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, "151", "steam")
	if err := os.WriteFile(filepath.Join(root, "151", "status"), []byte("Name:\tsteam\nState:\tZ (zombie)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	processes, err := steamProcesses(root, os.Geteuid())
	if err != nil {
		t.Fatal(err)
	}
	if len(processes) != 0 {
		t.Fatalf("zombie Steam process blocks setup: %#v", processes)
	}
}

func TestCloseSteamProcessesStopsGracefullyBeforeContinuing(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, "201", "steam")
	writeFakeProcess(t, root, "202", "steamwebhelper")
	var signals []syscall.Signal
	signal := func(pid int, value syscall.Signal) error {
		signals = append(signals, value)
		return os.RemoveAll(filepath.Join(root, processID(pid)))
	}

	err := closeSteamProcesses(context.Background(), root, os.Geteuid(), signal, time.Second, time.Second, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGTERM {
		t.Fatalf("signals = %#v, want graceful termination only", signals)
	}
}

func TestCloseSteamProcessesEscalatesOnlyAfterGracePeriod(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, "301", "steam")
	var signals []syscall.Signal
	signal := func(pid int, value syscall.Signal) error {
		signals = append(signals, value)
		if value == syscall.SIGKILL {
			return os.RemoveAll(filepath.Join(root, processID(pid)))
		}
		return nil
	}

	err := closeSteamProcesses(context.Background(), root, os.Geteuid(), signal, 5*time.Millisecond, time.Second, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGKILL {
		t.Fatalf("signals = %#v, want TERM then KILL", signals)
	}
}

func TestCloseSteamProcessesHonorsCancellationWithoutForceKill(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, "401", "steam")
	ctx, cancel := context.WithCancel(context.Background())
	var signals []syscall.Signal
	signal := func(_ int, value syscall.Signal) error {
		signals = append(signals, value)
		cancel()
		return nil
	}

	err := closeSteamProcesses(ctx, root, os.Geteuid(), signal, time.Second, time.Second, time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("close error = %v, want context cancellation", err)
	}
	if len(signals) != 1 || signals[0] != syscall.SIGTERM {
		t.Fatalf("signals after cancellation = %#v", signals)
	}
}

func TestSignalSteamProcessSetRevalidatesIdentityBeforeSignal(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, "501", "not-steam")
	called := false
	err := signalSteamProcessSet(root, os.Geteuid(), []steamProcess{{PID: 501, Name: "steam"}}, syscall.SIGTERM, func(int, syscall.Signal) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("a reused PID with a non-Steam process name was signaled")
	}
}

func TestCloseSteamProcessesReportsUnreadableProcRoot(t *testing.T) {
	err := closeSteamProcesses(context.Background(), filepath.Join(t.TempDir(), "missing"), os.Geteuid(), func(int, syscall.Signal) error {
		return nil
	}, time.Second, time.Second, time.Millisecond)
	if err == nil {
		t.Fatal("expected an unreadable process table to fail closed")
	}
}

func writeFakeProcess(t *testing.T, root, pid, name string) {
	t.Helper()
	directory := filepath.Join(root, pid)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "comm"), []byte(name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func processID(pid int) string {
	return fmt.Sprintf("%d", pid)
}
