package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/transaction"
)

// CompatdataState describes what steamapps/compatdata currently is.
type CompatdataState string

const (
	CompatdataMissing      CompatdataState = "missing"
	CompatdataDirectory    CompatdataState = "directory"
	CompatdataManagedLink  CompatdataState = "managed-link"
	CompatdataExternalLink CompatdataState = "external-link"
	CompatdataBrokenLink   CompatdataState = "broken-link"
	CompatdataInvalid      CompatdataState = "invalid"
)

// CompatdataPlan is a read-only description of an intended migration. It is
// produced before any filesystem mutation so the TUI can show it first.
type CompatdataPlan struct {
	Library      SteamLibrary
	Compatdata   string
	NativeTarget string
	BackupPath   string

	CurrentState CompatdataState

	RequiresCopy   bool
	RequiresBackup bool
}

// CompatdataResult records the committed transaction for a migration.
type CompatdataResult struct {
	Plan          CompatdataPlan
	TransactionID string
	JournalPath   string
}

const (
	compatdataPluginID = "steam-compatdata"
	compatdataName     = "compatdata"
	backupPrefix       = "compatdata.selene-backup-"
)

// NativeCompatdataRoot returns the Selene-managed native root that holds one
// compatdata directory per migrated NTFS library.
func NativeCompatdataRoot(env planner.Environment) string {
	return filepath.Join(env.XDGDataHome, "selene", "steam-compatdata")
}

// nativeTargetPath returns the per-library native compatdata directory.
func nativeTargetPath(env planner.Environment, library SteamLibrary) string {
	return filepath.Join(NativeCompatdataRoot(env), libraryID(library))
}

// CompatdataPath returns the steamapps/compatdata path for a library.
func CompatdataPath(library SteamLibrary) string {
	return filepath.Join(library.Path, "steamapps", compatdataName)
}

// InspectCompatdata reports the current state of the library's compatdata
// path without changing it.
func InspectCompatdata(library SteamLibrary) (CompatdataState, error) {
	path := CompatdataPath(library)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return CompatdataMissing, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect compatdata: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if _, evalErr := filepath.EvalSymlinks(path); evalErr != nil {
			return CompatdataBrokenLink, nil
		}
		return CompatdataExternalLink, nil
	}
	if info.IsDir() {
		return CompatdataDirectory, nil
	}
	return CompatdataInvalid, nil
}

// PlanCompatdataMigration inspects the environment and library and returns a
// plan. It performs no mutation and validates every preflight requirement.
func PlanCompatdataMigration(env planner.Environment, library SteamLibrary) (CompatdataPlan, error) {
	if err := validateEnvironment(env); err != nil {
		return CompatdataPlan{}, err
	}
	if err := validateLibrary(library); err != nil {
		return CompatdataPlan{}, err
	}
	state, err := InspectCompatdata(library)
	if err != nil {
		return CompatdataPlan{}, err
	}

	plan := CompatdataPlan{
		Library:      library,
		Compatdata:   CompatdataPath(library),
		NativeTarget: nativeTargetPath(env, library),
		BackupPath:   backupPath(library),
		CurrentState: state,
	}

	switch state {
	case CompatdataInvalid:
		return CompatdataPlan{}, errors.New("compatdata is a regular file; refusing to migrate")
	case CompatdataExternalLink, CompatdataBrokenLink:
		return plan, fmt.Errorf("compatdata is already a symlink; migrate or repair it explicitly rather than overwriting")
	case CompatdataManagedLink:
		return plan, nil
	case CompatdataDirectory:
		plan.RequiresCopy = true
		plan.RequiresBackup = true
	case CompatdataMissing:
		plan.RequiresBackup = false
	}

	// Confirm the native target would not live on NTFS.
	mounts, err := readMounts()
	if err != nil {
		return CompatdataPlan{}, err
	}
	if mount, ok := findMountFor(mounts, plan.NativeTarget); ok && isNTFS(mount.filesystem) {
		return CompatdataPlan{}, errors.New("the Proton compatdata target is also on NTFS; select a Linux native filesystem")
	}
	return plan, nil
}

// ApplyCompatdataMigration executes the planned migration transactionally.
func ApplyCompatdataMigration(env planner.Environment, plan CompatdataPlan) (CompatdataResult, error) {
	if err := validateEnvironment(env); err != nil {
		return CompatdataResult{}, err
	}
	if err := validateLibrary(plan.Library); err != nil {
		return CompatdataResult{}, err
	}

	// Re-check state to keep the operation idempotent and safe.
	state, err := InspectCompatdata(plan.Library)
	if err != nil {
		return CompatdataResult{}, err
	}
	switch state {
	case CompatdataManagedLink:
		return CompatdataResult{Plan: plan}, nil
	case CompatdataInvalid:
		return CompatdataResult{}, errors.New("compatdata is a regular file; refusing to migrate")
	case CompatdataExternalLink, CompatdataBrokenLink:
		return CompatdataResult{}, errors.New("compatdata is already a symlink to another destination")
	}

	if state == CompatdataDirectory && !plan.RequiresCopy {
		return CompatdataResult{}, errors.New("compatdata is a directory but the plan does not require a copy")
	}

	// Ensure Steam is not running before mutating.
	if steamRunning() {
		return CompatdataResult{}, errors.New("close Steam completely before migrating compatdata")
	}

	tx, err := transaction.Begin(stateRoot(env), "plugin steam-compatdata migrate "+plan.Compatdata, []transaction.Target{
		{Path: plan.Compatdata, Recursive: true},
	}, nil)
	if err != nil {
		return CompatdataResult{}, fmt.Errorf("create compatdata safety snapshot: %w", err)
	}

	if err := os.MkdirAll(plan.NativeTarget, 0o700); err != nil {
		return CompatdataResult{}, abort(tx, fmt.Errorf("create native compatdata target: %w", err))
	}

	if state == CompatdataDirectory && plan.RequiresCopy {
		if err := copyCompatdata(plan.Compatdata, plan.NativeTarget); err != nil {
			return CompatdataResult{}, abort(tx, fmt.Errorf("copy compatdata: %w", err))
		}
		if err := verifyCopy(plan.Compatdata, plan.NativeTarget); err != nil {
			return CompatdataResult{}, abort(tx, fmt.Errorf("verify copied compatdata: %w", err))
		}
		if err := os.Rename(plan.Compatdata, plan.BackupPath); err != nil {
			return CompatdataResult{}, abort(tx, fmt.Errorf("back up original compatdata: %w", err))
		}
	}

	if err := os.Symlink(plan.NativeTarget, plan.Compatdata); err != nil {
		return CompatdataResult{}, abort(tx, fmt.Errorf("create compatdata symlink: %w", err))
	}

	if err := verifyLink(plan.Compatdata, plan.NativeTarget); err != nil {
		return CompatdataResult{}, abort(tx, err)
	}

	if err := writeProbe(plan.Compatdata, plan.NativeTarget); err != nil {
		return CompatdataResult{}, abort(tx, fmt.Errorf("verify compatdata write path: %w", err))
	}

	if err := tx.Commit(); err != nil {
		return CompatdataResult{}, abort(tx, fmt.Errorf("commit compatdata migration: %w", err))
	}

	return CompatdataResult{
		Plan:          plan,
		TransactionID: tx.Journal.ID,
		JournalPath:   filepath.Join(tx.Journal.Root, "journal.json"),
	}, nil
}

// RollbackCompatdataMigration restores the original NTFS compatdata directory
// if the migration's backup is still present. The native target is preserved.
func RollbackCompatdataMigration(env planner.Environment, transactionID string) (CompatdataResult, error) {
	if err := validateEnvironment(env); err != nil {
		return CompatdataResult{}, err
	}
	if steamRunning() {
		return CompatdataResult{}, errors.New("close Steam completely before rolling back compatdata")
	}
	tx, err := transaction.Open(stateRoot(env), transactionID)
	if err != nil {
		return CompatdataResult{}, err
	}
	if err := tx.Rollback(); err != nil {
		return CompatdataResult{}, fmt.Errorf("roll back compatdata migration: %w", err)
	}
	return CompatdataResult{
		TransactionID: tx.Journal.ID,
		JournalPath:   filepath.Join(tx.Journal.Root, "journal.json"),
	}, nil
}

func backupPath(library SteamLibrary) string {
	return filepath.Join(library.Path, "steamapps", backupPrefix+time.Now().UTC().Format("20060102-150405"))
}

// libraryID derives a stable identifier from the library's absolute path,
// preferring the filesystem UUID when it can be resolved.
func libraryID(library SteamLibrary) string {
	mounts, err := readMounts()
	if err == nil {
		if mount, ok := findMountFor(mounts, library.Path); ok {
			if id := resolveFilesystemID(mount); id != "" {
				relative, relErr := filepath.Rel(mount.point, library.Path)
				if relErr == nil {
					digest := sha256.Sum256([]byte(relative))
					return id + "-" + hex.EncodeToString(digest[:4])
				}
			}
		}
	}
	digest := sha256.Sum256([]byte(filepath.Clean(library.Path)))
	return hex.EncodeToString(digest[:8])
}

// resolveFilesystemID returns a stable device identifier from a mount source.
// It uses the device name as a best effort since reading UUIDs requires
// platform-specific syscalls; path hashing is the fallback.
func resolveFilesystemID(mount mount) string {
	source := filepath.Clean(mount.source)
	if source == "" || source == "none" {
		return ""
	}
	digest := sha256.Sum256([]byte(source))
	return hex.EncodeToString(digest[:8])
}

func readMounts() ([]mount, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, fmt.Errorf("read mounted disks: %w", err)
	}
	return parseMountInfoLines(data)
}

func copyCompatdata(source, destination string) error {
	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			// Do not follow external symlinks during the copy.
			return fmt.Errorf("refusing to copy symlink inside compatdata: %s", current)
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode().IsRegular():
			return copyRegularFileAtomic(current, target, info.Mode())
		default:
			return fmt.Errorf("unsupported file type inside compatdata: %s", current)
		}
	})
}

func verifyCopy(source, destination string) error {
	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == source {
			return nil
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		srcInfo, err := os.Lstat(current)
		if err != nil {
			return err
		}
		dstInfo, err := os.Lstat(target)
		if err != nil {
			return fmt.Errorf("missing copied path %s: %w", target, err)
		}
		if srcInfo.Mode().IsRegular() != dstInfo.Mode().IsRegular() {
			return fmt.Errorf("type mismatch at %s", target)
		}
		if srcInfo.Mode().IsRegular() && srcInfo.Size() != dstInfo.Size() {
			return fmt.Errorf("size mismatch at %s", target)
		}
		return nil
	})
}

func verifyLink(link, target string) error {
	info, err := os.Lstat(link)
	if err != nil {
		return fmt.Errorf("inspect compatdata link: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return errors.New("compatdata is not a symlink after migration")
	}
	read, err := os.Readlink(link)
	if err != nil {
		return fmt.Errorf("read compatdata link: %w", err)
	}
	if filepath.Clean(read) != filepath.Clean(target) {
		return fmt.Errorf("compatdata link points to %s, want %s", read, target)
	}
	resolvedLink, err := filepath.EvalSymlinks(link)
	if err != nil {
		return fmt.Errorf("resolve compatdata link: %w", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return fmt.Errorf("resolve native target: %w", err)
	}
	if filepath.Clean(resolvedLink) != filepath.Clean(resolvedTarget) {
		return errors.New("compatdata link does not resolve to the native target")
	}
	return nil
}

func writeProbe(link, target string) error {
	probe := filepath.Join(link, ".selene-write-test")
	if err := os.WriteFile(probe, []byte("selene"), 0o600); err != nil {
		return fmt.Errorf("write through compatdata link: %w", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".selene-write-test")); err != nil {
		return fmt.Errorf("write probe did not appear in native target: %w", err)
	}
	return os.Remove(probe)
}

func copyRegularFileAtomic(source, destination string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

// steamRunning reports whether a Steam process belonging to the current user
// is running, as a best effort check.
func steamRunning() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	uid := os.Geteuid()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !allDigits(entry.Name()) {
			continue
		}
		if !processOwnedByUser(entry.Name(), uid) {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(cmdline))
		if name == "steam" || name == "steamwebhelper" {
			return true
		}
	}
	return false
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

// processOwnedByUser reports whether /proc/<pid> is owned by uid.
func processOwnedByUser(pid string, uid int) bool {
	info, err := os.Stat("/proc/" + pid)
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return int(stat.Uid) == uid
}
