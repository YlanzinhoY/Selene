package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/selene-linux/selene/internal/artifact"
	"github.com/selene-linux/selene/internal/catalog"
	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/transaction"
)

type Options struct {
	Output  io.Writer
	Fetcher *artifact.Fetcher
	Runner  ScriptRunner
}

type Result struct {
	TransactionID string `json:"transaction_id"`
	JournalPath   string `json:"journal_path"`
	Bundle        string `json:"bundle"`
	UserOnly      bool   `json:"user_only"`
}

type ScriptCommand struct {
	Directory string
	Script    string
	Arguments []string
	Env       []string
	Output    io.Writer
}

type ScriptRunner interface {
	Run(context.Context, ScriptCommand) error
}

type processRunner struct{}

const trustedPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

func (processRunner) Run(ctx context.Context, command ScriptCommand) error {
	arguments := append([]string{command.Script}, command.Arguments...)
	cmd := exec.CommandContext(ctx, "/bin/bash", arguments...)
	cmd.Dir = command.Directory
	cmd.Env = command.Env
	cmd.Stdout = command.Output
	cmd.Stderr = command.Output
	return cmd.Run()
}

// Install runs a pinned upstream setup script inside a Selene transaction.
// The initial implementation deliberately forces user-only integration.
func Install(ctx context.Context, source catalog.Catalog, bundleID string, env planner.Environment, options Options) (Result, error) {
	if err := preflight(env); err != nil {
		return Result{}, err
	}
	if options.Output == nil {
		options.Output = io.Discard
	}
	if options.Fetcher == nil {
		options.Fetcher = artifact.NewFetcher()
	}
	if options.Runner == nil {
		options.Runner = processRunner{}
	}

	bundle, ok := source.Bundle(bundleID)
	if !ok {
		return Result{}, fmt.Errorf("bundle %q not found", bundleID)
	}
	components, err := source.OrderedComponents(bundle)
	if err != nil {
		return Result{}, err
	}
	lock, err := acquireInstallLock(filepath.Join(env.XDGStateHome, "selene"))
	if err != nil {
		return Result{}, err
	}
	defer lock.release()

	fmt.Fprintln(options.Output, "Selene: downloading and verifying artifacts...")
	cacheDir := filepath.Join(env.XDGCacheHome, "selene", "downloads")
	artifacts := make(map[string]artifact.Result, len(components))
	for _, component := range components {
		result, err := options.Fetcher.Fetch(ctx, component, cacheDir)
		if err != nil {
			return Result{}, err
		}
		artifacts[component.ID] = result
	}

	targets, patterns, err := userTransactionScope(env)
	if err != nil {
		return Result{}, err
	}
	fmt.Fprintln(options.Output, "Selene: creating a rollback snapshot...")
	tx, err := transaction.Begin(filepath.Join(env.XDGStateHome, "selene"), "install "+bundleID+" "+source.Revision, targets, patterns)
	if err != nil {
		return Result{}, err
	}

	stageRoot := filepath.Join(tx.Journal.Root, "stage")
	if err := os.MkdirAll(stageRoot, 0o700); err != nil {
		return Result{}, abortTransaction(tx, nil, env, options, err)
	}
	staged := make(map[string]string, len(components))
	for _, component := range components {
		destination := filepath.Join(stageRoot, component.ID)
		if err := artifact.Extract(artifacts[component.ID].Path, component, destination); err != nil {
			return Result{}, abortTransaction(tx, nil, env, options, err)
		}
		staged[component.ID] = destination
	}

	slsteam, ok := source.Component("slsteam-moon")
	if !ok || slsteam.Install.Strategy != "verified-script" {
		return Result{}, abortTransaction(tx, nil, env, options, errors.New("verified slsteam-moon script adapter is unavailable"))
	}
	lumenStage, lumenOK := staged["lumen"]
	pluginStage, pluginOK := staged["luatools-moon"]
	if !lumenOK || !pluginOK {
		return Result{}, abortTransaction(tx, nil, env, options, errors.New("bundle does not contain the required Lumen and LuaTools components"))
	}
	script := filepath.Join(staged[slsteam.ID], filepath.FromSlash(slsteam.Install.Entrypoint))
	command := ScriptCommand{
		Directory: staged[slsteam.ID],
		Script:    script,
		Arguments: append([]string(nil), slsteam.Install.Arguments...),
		Env:       controlledEnvironment(env),
		Output:    options.Output,
	}
	fmt.Fprintln(options.Output, "Selene: running the verified setup.sh in user-only mode...")
	if err := options.Runner.Run(ctx, command); err != nil {
		return Result{}, abortTransaction(tx, &command, env, options, fmt.Errorf("slsteam-moon setup failed: %w", err))
	}

	configTemplate := filepath.Join(staged[slsteam.ID], "res", "config.yaml")
	configDestination := filepath.Join(env.XDGConfigHome, "SLSsteam", "config.yaml")
	if err := seedSLSConfig(configTemplate, configDestination); err != nil {
		return Result{}, abortTransaction(tx, &command, env, options, err)
	}
	stackData := stackDataHome(env)
	candidate, err := prepareLumenCandidate(
		stackData,
		tx.Journal.Root,
		filepath.Join(stackData, "Lumen"),
		lumenStage,
		pluginStage,
		source,
	)
	if err != nil {
		return Result{}, abortTransaction(tx, &command, env, options, err)
	}
	if err := activateDirectory(candidate, filepath.Join(stackData, "Lumen")); err != nil {
		return Result{}, abortTransaction(tx, &command, env, options, err)
	}
	if err := validateInstalled(env); err != nil {
		return Result{}, abortTransaction(tx, &command, env, options, err)
	}
	if err := tx.Commit(); err != nil {
		return Result{}, abortTransaction(tx, &command, env, options, err)
	}

	return Result{
		TransactionID: tx.Journal.ID,
		JournalPath:   filepath.Join(tx.Journal.Root, "journal.json"),
		Bundle:        bundleID,
		UserOnly:      true,
	}, nil
}

func preflight(env planner.Environment) error {
	if runtime.GOOS != "linux" || env.OS != "linux" {
		return errors.New("installation is supported only on Linux")
	}
	if runtime.GOARCH != "amd64" || env.Arch != "amd64" {
		return errors.New("the current LuaTools artifacts require amd64/x86_64")
	}
	if effectiveUID() == 0 {
		return errors.New("do not run Selene as root")
	}
	if err := validateEnvironment(env); err != nil {
		return err
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		return errors.New("/bin/bash is required for the verified upstream setup")
	}
	for _, command := range []string{"awk", "sed", "grep", "find", "install", "cp", "mv", "pkill"} {
		if _, ok := trustedCommand(command); !ok {
			return fmt.Errorf("required command %s was not found", command)
		}
	}
	if !nativeSteamBootstrapped(env.Home) {
		return errors.New("a bootstrapped native Steam installation is required; open native Steam once before installing (Flatpak-only Steam is not supported yet)")
	}
	return validateUserScope(env)
}

func validateEnvironment(env planner.Environment) error {
	for name, value := range map[string]string{
		"HOME": env.Home, "XDG_DATA_HOME": env.XDGDataHome,
		"XDG_CACHE_HOME": env.XDGCacheHome, "XDG_CONFIG_HOME": env.XDGConfigHome,
		"XDG_STATE_HOME": env.XDGStateHome,
	} {
		if value == "" || !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be an absolute path", name)
		}
		cleaned := filepath.Clean(value)
		root := filepath.VolumeName(cleaned) + string(filepath.Separator)
		if cleaned == root || cleaned == string(filepath.Separator) {
			return fmt.Errorf("%s must not be a filesystem root", name)
		}
	}
	return nil
}

func nativeSteamBootstrapped(home string) bool {
	for _, root := range []string{
		filepath.Join(home, ".local", "share", "Steam"),
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".steam", "debian-installation"),
	} {
		client := filepath.Join(root, "ubuntu12_32", "steamclient.so")
		ui := filepath.Join(root, "ubuntu12_32", "steamui.so")
		clientInfo, clientErr := os.Stat(client)
		uiInfo, uiErr := os.Stat(ui)
		if clientErr == nil && uiErr == nil && clientInfo.Mode().IsRegular() && uiInfo.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func controlledEnvironment(env planner.Environment) []string {
	allow := map[string]bool{
		"LANG": true, "LANGUAGE": true, "LC_ALL": true, "LC_MESSAGES": true,
		"TERM": true, "COLORTERM": true, "NO_COLOR": true,
		"DISPLAY": true, "WAYLAND_DISPLAY": true,
		"XDG_RUNTIME_DIR": true, "DBUS_SESSION_BUS_ADDRESS": true,
		"USER": true, "LOGNAME": true, "SHELL": true,
	}
	var values []string
	for _, value := range os.Environ() {
		key, _, ok := strings.Cut(value, "=")
		if !ok || (!allow[key] && !strings.HasPrefix(key, "LC_")) {
			continue
		}
		values = append(values, value)
	}
	values = append(values,
		"HOME="+env.Home,
		"XDG_DATA_HOME="+env.XDGDataHome,
		"XDG_CACHE_HOME="+env.XDGCacheHome,
		"XDG_CONFIG_HOME="+env.XDGConfigHome,
		"XDG_STATE_HOME="+env.XDGStateHome,
		"PATH="+trustedPath,
		"SLSM_IMMUTABLE=1",
		"SLSM_SUDO_DENIED=1",
		"SLSM_SUDO_PRIMED=0",
	)
	return values
}

func abortTransaction(tx *transaction.Transaction, installCommand *ScriptCommand, env planner.Environment, options Options, cause error) error {
	_ = tx.MarkFailed(cause)
	cleanupContext, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	nativeLauncher, launcherErr := nativeSteamLauncher(env)
	if installCommand != nil {
		fmt.Fprintln(options.Output, "Selene: installation failed; running the verified uninstaller...")
		uninstall := *installCommand
		uninstall.Arguments = []string{"uninstall"}
		_ = options.Runner.Run(cleanupContext, uninstall)
	}
	_ = stopSteam(cleanupContext, env)
	stopGuardian(cleanupContext, env)
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		return fmt.Errorf("%w; rollback also failed: %v; journal: %s", cause, rollbackErr, filepath.Join(tx.Journal.Root, "journal.json"))
	}
	restoreGuardian(cleanupContext, env)
	if launcherErr == nil {
		if restartErr := startSteamAfterRollback(env, nativeLauncher); restartErr != nil {
			return fmt.Errorf("%w; previous state restored by transaction %s, but Steam restart failed: %v", cause, tx.Journal.ID, restartErr)
		}
	} else {
		return fmt.Errorf("%w; previous state restored by transaction %s, but Steam restart failed: %v", cause, tx.Journal.ID, launcherErr)
	}
	return fmt.Errorf("%w; previous state restored by transaction %s", cause, tx.Journal.ID)
}

func stopGuardian(ctx context.Context, env planner.Environment) {
	systemctl, ok := trustedCommand("systemctl")
	if !ok {
		return
	}
	cmd := exec.CommandContext(ctx, systemctl, "--user", "disable", "--now",
		"slsteam-desktop-guardian.path", "slsteam-desktop-guardian.timer")
	cmd.Env = controlledEnvironment(env)
	_ = cmd.Run()
	cmd = exec.CommandContext(ctx, systemctl, "--user", "stop", "slsteam-desktop-guardian.service")
	cmd.Env = controlledEnvironment(env)
	_ = cmd.Run()
	cmd = exec.CommandContext(ctx, systemctl, "--user", "daemon-reload")
	cmd.Env = controlledEnvironment(env)
	_ = cmd.Run()
}

func restoreGuardian(ctx context.Context, env planner.Environment) {
	systemctl, ok := trustedCommand("systemctl")
	if !ok {
		return
	}
	cmd := exec.CommandContext(ctx, systemctl, "--user", "daemon-reload")
	cmd.Env = controlledEnvironment(env)
	_ = cmd.Run()
	unitDir := filepath.Join(env.XDGConfigHome, "systemd", "user")
	for _, unit := range []struct {
		link string
		name string
	}{
		{filepath.Join(unitDir, "default.target.wants", "slsteam-desktop-guardian.path"), "slsteam-desktop-guardian.path"},
		{filepath.Join(unitDir, "timers.target.wants", "slsteam-desktop-guardian.timer"), "slsteam-desktop-guardian.timer"},
	} {
		if _, err := os.Lstat(unit.link); err == nil {
			cmd := exec.CommandContext(ctx, systemctl, "--user", "start", unit.name)
			cmd.Env = controlledEnvironment(env)
			_ = cmd.Run()
		}
	}
}

func trustedCommand(name string) (string, bool) {
	if name == "" || filepath.Base(name) != name {
		return "", false
	}
	for _, directory := range strings.Split(trustedPath, ":") {
		candidate := filepath.Join(directory, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, true
		}
	}
	return "", false
}

func validateInstalled(env planner.Environment) error {
	root := stackDataHome(env)
	for _, relative := range []string{
		"SLSsteam/SLSsteam.so",
		"SLSsteam/path/steam",
		"Lumen/lumen",
		"Lumen/lua/boot.lua",
		"Lumen/luatools/plugin.json",
		"Lumen/luatools/backend/main.lua",
	} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("post-install validation failed for %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("post-install validation failed: %s is not a regular file", path)
		}
	}
	return nil
}

type installLock struct {
	path string
	file *os.File
}

func acquireInstallLock(stateRoot string) (*installLock, error) {
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(stateRoot, "install.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("another Selene install or rollback is running; lock: %s", path)
	}
	if err := file.Truncate(0); err != nil {
		unlockFile(file)
		_ = file.Close()
		return nil, err
	}
	if _, err := file.Seek(0, 0); err != nil {
		unlockFile(file)
		_ = file.Close()
		return nil, err
	}
	_, writeErr := fmt.Fprintf(file, "%d\n", os.Getpid())
	if writeErr != nil {
		unlockFile(file)
		_ = file.Close()
		return nil, writeErr
	}
	if err := file.Sync(); err != nil {
		unlockFile(file)
		_ = file.Close()
		return nil, err
	}
	return &installLock{path: path, file: file}, nil
}

func (lock *installLock) release() {
	if lock != nil {
		unlockFile(lock.file)
		_ = lock.file.Close()
	}
}
