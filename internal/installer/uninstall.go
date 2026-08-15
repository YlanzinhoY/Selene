package installer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/selene-linux/selene/internal/artifact"
	"github.com/selene-linux/selene/internal/catalog"
	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/transaction"
)

const slsteamDesktopTag = "X-SLSteamMoon-Patched=true"

type UninstallPreview struct {
	Detected bool     `json:"detected"`
	Traces   []string `json:"traces"`
}

type UninstallResult struct {
	TransactionID string `json:"transaction_id,omitempty"`
	JournalPath   string `json:"journal_path,omitempty"`
	Bundle        string `json:"bundle"`
	Removed       bool   `json:"removed"`
	UserOnly      bool   `json:"user_only"`
}

// PreviewUninstall finds only the user-scoped traces managed by Selene and the
// pinned slsteam-moon adapter. It does not modify files or start processes.
func PreviewUninstall(env planner.Environment) (UninstallPreview, error) {
	if err := validateEnvironment(env); err != nil {
		return UninstallPreview{}, err
	}
	traces, err := discoverUserStackTraces(env)
	if err != nil {
		return UninstallPreview{}, err
	}
	return UninstallPreview{Detected: len(traces) > 0, Traces: traces}, nil
}

// Uninstall removes the complete LuaTools/Lumen/slsteam-moon user stack. The
// upstream uninstaller is fetched from the pinned catalog and verified before
// execution. A fresh snapshot protects the current installation if any step
// of removal fails.
func Uninstall(ctx context.Context, source catalog.Catalog, env planner.Environment, options Options) (UninstallResult, error) {
	if err := uninstallPreflight(env); err != nil {
		return UninstallResult{}, err
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

	preview, err := PreviewUninstall(env)
	if err != nil {
		return UninstallResult{}, err
	}
	if !preview.Detected {
		return UninstallResult{Bundle: "luatools", Removed: false, UserOnly: true}, nil
	}

	slsteam, ok := source.Component("slsteam-moon")
	if !ok || slsteam.Install.Strategy != "verified-script" || slsteam.Install.Entrypoint == "" {
		return UninstallResult{}, errors.New("verified slsteam-moon uninstall adapter is unavailable")
	}
	lock, err := acquireInstallLock(filepath.Join(env.XDGStateHome, "selene"))
	if err != nil {
		return UninstallResult{}, err
	}
	defer lock.release()

	fmt.Fprintln(options.Output, "Selene: baixando e verificando o desinstalador fixado...")
	cacheDir := filepath.Join(env.XDGCacheHome, "selene", "downloads")
	downloaded, err := options.Fetcher.Fetch(ctx, slsteam, cacheDir)
	if err != nil {
		return UninstallResult{}, err
	}

	targets, patterns, err := userTransactionScope(env)
	if err != nil {
		return UninstallResult{}, err
	}
	fmt.Fprintln(options.Output, "Selene: criando snapshot de segurança antes da remoção...")
	tx, err := transaction.Begin(filepath.Join(env.XDGStateHome, "selene"), "uninstall luatools "+source.Revision, targets, patterns)
	if err != nil {
		return UninstallResult{}, err
	}

	stage := filepath.Join(tx.Journal.Root, "stage", slsteam.ID)
	if err := artifact.Extract(downloaded.Path, slsteam, stage); err != nil {
		return UninstallResult{}, abortUninstall(tx, env, options.Output, err)
	}
	command := ScriptCommand{
		Directory: stage,
		Script:    filepath.Join(stage, filepath.FromSlash(slsteam.Install.Entrypoint)),
		Arguments: []string{"uninstall"},
		Env:       controlledEnvironment(env),
		Output:    options.Output,
	}
	fmt.Fprintln(options.Output, "Selene: restaurando os lançadores da Steam com o desinstalador verificado...")
	if err := options.Runner.Run(ctx, command); err != nil {
		return UninstallResult{}, abortUninstall(tx, env, options.Output, fmt.Errorf("slsteam-moon uninstall failed: %w", err))
	}

	cleanupContext, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	stopGuardian(cleanupContext, env)
	stopLumen(cleanupContext, env)
	fmt.Fprintln(options.Output, "Selene: removendo Lumen, LuaTools e os resíduos da integração do usuário...")
	if err := removeUserStack(env); err != nil {
		return UninstallResult{}, abortUninstall(tx, env, options.Output, err)
	}
	// Reload once more after deleting any units that an older upstream release
	// may have left behind, so the user manager cannot retain stale definitions.
	stopGuardian(cleanupContext, env)
	if err := verifyUserStackRemoved(env); err != nil {
		return UninstallResult{}, abortUninstall(tx, env, options.Output, err)
	}
	if err := tx.Commit(); err != nil {
		return UninstallResult{}, abortUninstall(tx, env, options.Output, err)
	}

	return UninstallResult{
		TransactionID: tx.Journal.ID,
		JournalPath:   filepath.Join(tx.Journal.Root, "journal.json"),
		Bundle:        "luatools",
		Removed:       true,
		UserOnly:      true,
	}, nil
}

func uninstallPreflight(env planner.Environment) error {
	if runtime.GOOS != "linux" || env.OS != "linux" {
		return errors.New("complete removal is supported only on Linux")
	}
	if runtime.GOARCH != "amd64" || env.Arch != "amd64" {
		return errors.New("the current LuaTools adapter requires amd64/x86_64")
	}
	if effectiveUID() == 0 {
		return errors.New("do not run Selene as root")
	}
	if err := validateEnvironment(env); err != nil {
		return err
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		return errors.New("/bin/bash is required for the verified upstream uninstaller")
	}
	for _, command := range []string{"sed", "grep", "find", "cp", "mv", "rm", "pkill"} {
		if _, ok := trustedCommand(command); !ok {
			return fmt.Errorf("required command %s was not found", command)
		}
	}
	return validateUserScope(env)
}

func discoverUserStackTraces(env planner.Environment) ([]string, error) {
	seen := make(map[string]bool)
	add := func(path string) { seen[filepath.Clean(path)] = true }
	for _, path := range removableExactPaths(env) {
		if _, err := os.Lstat(path); err == nil {
			add(path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	for _, path := range shellRCPaths(env) {
		contains, err := fileContainsAny(path, "SLSsteam/path")
		if err != nil {
			return nil, err
		}
		if contains {
			add(path)
		}
	}
	for _, glob := range removablePatterns(env) {
		matches, err := filepath.Glob(glob)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			add(match)
		}
	}
	for _, glob := range desktopEntryPatterns(env) {
		matches, err := filepath.Glob(glob)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			contains, err := fileContainsAny(match, slsteamDesktopTag, "/SLSsteam/path/steam")
			if err != nil {
				return nil, err
			}
			if contains {
				add(match)
			}
		}
	}
	result := make([]string, 0, len(seen))
	for path := range seen {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

func removableExactPaths(env planner.Environment) []string {
	unitDir := filepath.Join(env.XDGConfigHome, "systemd", "user")
	stackData := stackDataHome(env)
	return []string{
		filepath.Join(stackData, "SLSsteam"),
		filepath.Join(stackData, "Lumen"),
		filepath.Join(env.XDGConfigHome, "SLSsteam"),
		filepath.Join(env.XDGStateHome, "slsteam-moon"),
		filepath.Join(unitDir, "slsteam-desktop-guardian.service"),
		filepath.Join(unitDir, "slsteam-desktop-guardian.path"),
		filepath.Join(unitDir, "slsteam-desktop-guardian.timer"),
		filepath.Join(unitDir, "default.target.wants", "slsteam-desktop-guardian.path"),
		filepath.Join(unitDir, "timers.target.wants", "slsteam-desktop-guardian.timer"),
	}
}

func removablePatterns(env planner.Environment) []string {
	unitDir := filepath.Join(env.XDGConfigHome, "systemd", "user")
	stackData := stackDataHome(env)
	patterns := []string{
		filepath.Join(unitDir, "app-*@autostart.service.d", "slsteam-guardian.conf"),
		filepath.Join(stackData, ".selene-Lumen-stage-*"),
		filepath.Join(stackData, ".selene-Lumen-previous-*"),
	}
	for _, directory := range []string{
		filepath.Join(env.XDGDataHome, "applications"),
		filepath.Join(env.XDGConfigHome, "autostart"),
		resolveDesktopDir(env),
	} {
		patterns = append(patterns,
			filepath.Join(directory, "*steam*.desktop.slssteam-backup"),
			filepath.Join(directory, "*steam*.desktop.slsteam-bak"),
		)
	}
	return patterns
}

func desktopEntryPatterns(env planner.Environment) []string {
	return []string{
		filepath.Join(env.XDGDataHome, "applications", "*steam*.desktop"),
		filepath.Join(env.XDGConfigHome, "autostart", "*steam*.desktop"),
		filepath.Join(resolveDesktopDir(env), "*steam*.desktop"),
	}
}

func shellRCPaths(env planner.Environment) []string {
	return []string{
		filepath.Join(env.Home, ".bashrc"),
		filepath.Join(env.Home, ".zshrc"),
		filepath.Join(env.Home, ".profile"),
	}
}

func removeUserStack(env planner.Environment) error {
	for _, path := range shellRCPaths(env) {
		if err := removeShellIntegration(path); err != nil {
			return err
		}
	}
	for _, path := range removableExactPaths(env) {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	for _, glob := range removablePatterns(env) {
		matches, err := filepath.Glob(glob)
		if err != nil {
			return err
		}
		for _, match := range matches {
			if err := os.RemoveAll(match); err != nil {
				return fmt.Errorf("remove %s: %w", match, err)
			}
			if filepath.Base(match) == "slsteam-guardian.conf" {
				// Remove a generated drop-in directory only when the upstream
				// sentinel was its last remaining child.
				_ = os.Remove(filepath.Dir(match))
			}
		}
	}
	return nil
}

func verifyUserStackRemoved(env planner.Environment) error {
	traces, err := discoverUserStackTraces(env)
	if err != nil {
		return err
	}
	if len(traces) == 0 {
		return nil
	}
	return fmt.Errorf("complete removal left managed traces: %s", strings.Join(traces, ", "))
}

func removeShellIntegration(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	lines := strings.Split(string(data), "\n")
	filtered := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(line, "SLSsteam/path") || trimmed == "# SLSsteam: Add wrapper to PATH" {
			changed = true
			continue
		}
		filtered = append(filtered, line)
	}
	if !changed {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".selene-shell-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		temporary.Close()
		return err
	}
	if _, err := io.WriteString(temporary, strings.Join(filtered, "\n")); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
}

func fileContainsAny(path string, needles ...string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	for _, needle := range needles {
		if bytes.Contains(data, []byte(needle)) {
			return true, nil
		}
	}
	return false, nil
}

func stopLumen(ctx context.Context, env planner.Environment) {
	pkill, ok := trustedCommand("pkill")
	if !ok {
		return
	}
	cmd := exec.CommandContext(ctx, pkill, "-TERM", "-f", filepath.Join(stackDataHome(env), "Lumen", "lumen"))
	cmd.Env = controlledEnvironment(env)
	_ = cmd.Run()
}

func abortUninstall(tx *transaction.Transaction, env planner.Environment, output io.Writer, cause error) error {
	_ = tx.MarkFailed(cause)
	cleanupContext, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	stopGuardian(cleanupContext, env)
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		return fmt.Errorf("%w; removal rollback also failed: %v; journal: %s", cause, rollbackErr, filepath.Join(tx.Journal.Root, "journal.json"))
	}
	restoreGuardian(cleanupContext, env)
	fmt.Fprintf(output, "Selene: remoção cancelada; instalação restaurada pela transação %s.\n", tx.Journal.ID)
	return fmt.Errorf("%w; installed state restored by transaction %s", cause, tx.Journal.ID)
}
