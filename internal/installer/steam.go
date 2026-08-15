package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/selene-linux/selene/internal/planner"
)

// nativeSteamLauncher resolves a launcher that survives changes to the
// SLSsteam wrapper. Resolve it before rollback so Steam can always be started
// again after the snapshot has been restored.
func nativeSteamLauncher(env planner.Environment) (string, error) {
	for _, candidate := range []string{
		"/usr/bin/steam",
		"/usr/games/steam",
		"/usr/local/bin/steam",
		filepath.Join(env.Home, ".local", "share", "Steam", "steam.sh"),
		filepath.Join(env.Home, ".steam", "steam", "steam.sh"),
		filepath.Join(env.Home, ".steam", "debian-installation", "steam.sh"),
	} {
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", errors.New("a native Steam launcher could not be found; open or reinstall native Steam before rolling back")
}

func stopSteam(ctx context.Context, env planner.Environment) error {
	pkill, ok := trustedCommand("pkill")
	if !ok {
		return errors.New("pkill is required to restart Steam safely")
	}
	for _, command := range [][]string{
		{"-TERM", "-x", "steam"},
		{"-TERM", "-f", "steamwebhelper"},
		{"-TERM", "-f", "/steam$|/steam "},
	} {
		runSteamSignal(ctx, env, pkill, command...)
	}

	// Give the client a short grace period to flush state before forcing any
	// remaining process down. A missing pgrep simply falls back to the timeout.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !steamIsRunning(ctx, env) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	for _, command := range [][]string{
		{"-KILL", "-x", "steam"},
		{"-KILL", "-f", "steamwebhelper"},
		{"-KILL", "-f", "/steam$|/steam "},
	} {
		runSteamSignal(ctx, env, pkill, command...)
	}
	return nil
}

func startSteamAfterRollback(env planner.Environment, nativeFallback string) error {
	launcher := filepath.Join(stackDataHome(env), "SLSsteam", "path", "steam")
	if !isExecutableFile(launcher) {
		launcher = nativeFallback
	}
	if !isExecutableFile(launcher) {
		return fmt.Errorf("Steam launcher disappeared after rollback: %s", launcher)
	}
	command := exec.Command(launcher, "-silent")
	command.Env = controlledEnvironment(env)
	if err := command.Start(); err != nil {
		return fmt.Errorf("restart Steam with %s: %w", launcher, err)
	}
	if err := command.Process.Release(); err != nil {
		return fmt.Errorf("detach restarted Steam process: %w", err)
	}
	return nil
}

func steamIsRunning(ctx context.Context, env planner.Environment) bool {
	pgrep, ok := trustedCommand("pgrep")
	if !ok {
		return true
	}
	for _, command := range [][]string{
		{"-x", "steam"},
		{"-f", "steamwebhelper"},
		{"-f", "/steam$|/steam "},
	} {
		cmd := exec.CommandContext(ctx, pgrep, command...)
		cmd.Env = controlledEnvironment(env)
		if cmd.Run() == nil {
			return true
		}
	}
	return false
}

func runSteamSignal(ctx context.Context, env planner.Environment, pkill string, arguments ...string) {
	cmd := exec.CommandContext(ctx, pkill, arguments...)
	cmd.Env = controlledEnvironment(env)
	_ = cmd.Run()
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}
