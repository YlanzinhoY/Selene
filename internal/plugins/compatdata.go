package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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

	CurrentState      CompatdataState
	LinkTarget        string
	BlockedReason     string
	RollbackAvailable bool

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
	compatdataPluginID    = "steam-compatdata"
	compatdataName        = "compatdata"
	backupPrefix          = "compatdata.selene-backup-"
	compatdataJournalFile = "compatdata-migration.json"
)

type compatdataJournal struct {
	SchemaVersion int            `json:"schema_version"`
	Plan          CompatdataPlan `json:"plan"`
}

// NativeCompatdataRoot returns the Selene-managed native root that holds one
// compatdata directory per migrated NTFS library.
func NativeCompatdataRoot(env planner.Environment) string {
	return filepath.Join(env.XDGDataHome, "selene", "steam-compatdata")
}

// nativeTargetPath returns the per-library native compatdata directory.
func nativeTargetPath(env planner.Environment, library SteamLibrary) string {
	return filepath.Join(NativeCompatdataRoot(env), libraryID(library))
}

// managedNativeTargetPaths includes the current UUID-based identity and the
// source-hash identity used by the first version of this feature. Keeping the
// latter lets Selene recognise and roll back existing managed links after an
// upgrade instead of presenting them as unknown external links.
func managedNativeTargetPaths(env planner.Environment, library SteamLibrary) []string {
	paths := []string{nativeTargetPath(env, library)}
	legacy := filepath.Join(NativeCompatdataRoot(env), legacyLibraryID(library))
	if legacy != paths[0] {
		paths = append(paths, legacy)
	}
	return paths
}

// CompatdataPath returns the steamapps/compatdata path for a library.
func CompatdataPath(library SteamLibrary) string {
	return filepath.Join(library.Path, "steamapps", compatdataName)
}

// InspectCompatdata reports the current state of the library's compatdata
// path without changing it. A link is managed only when it resolves to this
// library's deterministic Selene target.
func InspectCompatdata(env planner.Environment, library SteamLibrary) (CompatdataState, string, error) {
	path := CompatdataPath(library)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return CompatdataMissing, "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("inspect compatdata: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, readErr := os.Readlink(path)
		if readErr != nil {
			return "", "", fmt.Errorf("read compatdata link: %w", readErr)
		}
		if _, evalErr := filepath.EvalSymlinks(path); evalErr != nil {
			return CompatdataBrokenLink, target, nil
		}
		resolvedTarget := resolveLinkTarget(path, target)
		for _, managedTarget := range managedNativeTargetPaths(env, library) {
			if samePath(resolvedTarget, managedTarget) {
				return CompatdataManagedLink, target, nil
			}
		}
		return CompatdataExternalLink, target, nil
	}
	if info.IsDir() {
		return CompatdataDirectory, "", nil
	}
	return CompatdataInvalid, "", nil
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
	state, linkTarget, err := InspectCompatdata(env, library)
	if err != nil {
		return CompatdataPlan{}, err
	}

	plan := CompatdataPlan{
		Library:      library,
		Compatdata:   CompatdataPath(library),
		NativeTarget: nativeTargetPath(env, library),
		BackupPath:   availableBackupPath(library),
		CurrentState: state,
		LinkTarget:   linkTarget,
	}

	switch state {
	case CompatdataInvalid:
		plan.BlockedReason = "compatdata is a regular file; Selene will not overwrite it"
	case CompatdataExternalLink:
		plan.BlockedReason = "compatdata already points to another location; Selene will not overwrite an external link"
	case CompatdataBrokenLink:
		plan.BlockedReason = "compatdata is a broken link; repair or remove it manually before migrating"
	case CompatdataManagedLink:
		plan.NativeTarget = resolveLinkTarget(plan.Compatdata, linkTarget)
		if backup, ok := compatdataRollbackBackup(env, library); ok {
			plan.BackupPath = backup
			plan.RollbackAvailable = true
		}
	case CompatdataDirectory:
		plan.RequiresCopy = true
		plan.RequiresBackup = true
	case CompatdataMissing:
		plan.RequiresBackup = false
	}

	// A discovered NTFS library can be listed even when its mount is read-only;
	// the plan explains why it cannot be changed instead of hiding it.
	mounts, err := readMounts()
	if err != nil {
		return CompatdataPlan{}, err
	}
	if state == CompatdataDirectory || state == CompatdataMissing {
		if mount, ok := findMountFor(mounts, library.Path); ok && mount.point == library.MountPoint && isNTFS(mount.filesystem) && mount.readOnly {
			plan.BlockedReason = "the NTFS Steam library is mounted read-only"
		}
		if mount, ok := findMountFor(mounts, plan.NativeTarget); ok {
			if isNTFS(mount.filesystem) {
				plan.BlockedReason = "the Proton compatdata target is also on NTFS; choose a Linux native filesystem"
			} else if mount.readOnly {
				plan.BlockedReason = "the native compatdata target filesystem is mounted read-only"
			}
		}
		if reason := nativeTargetConflict(plan.NativeTarget); reason != "" {
			plan.BlockedReason = reason
		}
	} else if state == CompatdataManagedLink && plan.RollbackAvailable {
		if mount, ok := findMountFor(mounts, library.Path); ok && mount.point == library.MountPoint && isNTFS(mount.filesystem) && mount.readOnly {
			plan.BlockedReason = "the NTFS Steam library is mounted read-only, so its backup cannot be restored"
		}
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
	freshPlan, err := PlanCompatdataMigration(env, plan.Library)
	if err != nil {
		return CompatdataResult{}, err
	}
	if freshPlan.BlockedReason != "" {
		return CompatdataResult{}, errors.New(freshPlan.BlockedReason)
	}

	// Re-check state to keep the operation idempotent and safe.
	state, _, err := InspectCompatdata(env, freshPlan.Library)
	if err != nil {
		return CompatdataResult{}, err
	}
	switch state {
	case CompatdataManagedLink:
		return CompatdataResult{Plan: freshPlan}, nil
	case CompatdataInvalid:
		return CompatdataResult{}, errors.New("compatdata is a regular file; refusing to migrate")
	case CompatdataExternalLink, CompatdataBrokenLink:
		return CompatdataResult{}, errors.New("compatdata is already a symlink to another destination")
	}

	if state == CompatdataDirectory && !freshPlan.RequiresCopy {
		return CompatdataResult{}, errors.New("compatdata is a directory but the plan does not require a copy")
	}

	// Ensure Steam is not running before mutating.
	if steamRunning() {
		return CompatdataResult{}, errors.New("close Steam completely before migrating compatdata")
	}

	tx, err := transaction.Begin(stateRoot(env), compatdataMigrationDescription(freshPlan.Library), []transaction.Target{
		{Path: freshPlan.Compatdata, Recursive: true},
	}, nil)
	if err != nil {
		return CompatdataResult{}, fmt.Errorf("create compatdata safety snapshot: %w", err)
	}

	if err := persistCompatdataJournal(tx, freshPlan); err != nil {
		return CompatdataResult{}, abort(tx, err)
	}

	if err := os.MkdirAll(freshPlan.NativeTarget, 0o700); err != nil {
		return CompatdataResult{}, abort(tx, fmt.Errorf("create native compatdata target: %w", err))
	}

	if state == CompatdataDirectory && freshPlan.RequiresCopy {
		if err := copyCompatdata(freshPlan.Compatdata, freshPlan.NativeTarget); err != nil {
			return CompatdataResult{}, abort(tx, fmt.Errorf("copy compatdata: %w", err))
		}
		if err := verifyCopy(freshPlan.Compatdata, freshPlan.NativeTarget); err != nil {
			return CompatdataResult{}, abort(tx, fmt.Errorf("verify copied compatdata: %w", err))
		}
		if err := os.Rename(freshPlan.Compatdata, freshPlan.BackupPath); err != nil {
			return CompatdataResult{}, abort(tx, fmt.Errorf("back up original compatdata: %w", err))
		}
	}

	if err := os.Symlink(freshPlan.NativeTarget, freshPlan.Compatdata); err != nil {
		return CompatdataResult{}, abort(tx, fmt.Errorf("create compatdata symlink: %w", err))
	}

	if err := verifyLink(freshPlan.Compatdata, freshPlan.NativeTarget); err != nil {
		return CompatdataResult{}, abort(tx, err)
	}

	if err := writeProbe(freshPlan.Compatdata, freshPlan.NativeTarget); err != nil {
		return CompatdataResult{}, abort(tx, fmt.Errorf("verify compatdata write path: %w", err))
	}

	if err := tx.Commit(); err != nil {
		return CompatdataResult{}, abort(tx, fmt.Errorf("commit compatdata migration: %w", err))
	}

	return CompatdataResult{
		Plan:          freshPlan,
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
	plan, err := loadCompatdataJournal(tx)
	if err != nil {
		return CompatdataResult{}, err
	}
	if tx.Journal.Description != compatdataMigrationDescription(plan.Library) {
		return CompatdataResult{}, errors.New("transaction is not a Selene compatdata migration")
	}
	if err := validateLibrary(plan.Library); err != nil {
		return CompatdataResult{}, err
	}
	if err := ensureCompatdataLibraryWritable(plan.Library); err != nil {
		return CompatdataResult{}, err
	}
	state, linkTarget, err := InspectCompatdata(env, plan.Library)
	if err != nil {
		return CompatdataResult{}, err
	}
	if state != CompatdataManagedLink || !samePath(resolveLinkTarget(plan.Compatdata, linkTarget), plan.NativeTarget) {
		return CompatdataResult{}, errors.New("compatdata no longer points to this Selene migration; refusing rollback")
	}
	if plan.RequiresBackup {
		if err := validateCompatdataBackup(plan.BackupPath); err != nil {
			return CompatdataResult{}, err
		}
	}
	if err := tx.Rollback(); err != nil {
		return CompatdataResult{}, fmt.Errorf("roll back compatdata migration: %w", err)
	}
	if plan.RequiresBackup {
		if err := restoreCompatdataBackup(plan); err != nil {
			return CompatdataResult{}, err
		}
	}
	return CompatdataResult{
		Plan:          plan,
		TransactionID: tx.Journal.ID,
		JournalPath:   filepath.Join(tx.Journal.Root, "journal.json"),
	}, nil
}

// RollbackLatestCompatdataMigration rolls back the newest recorded migration
// for a library. It also supports a narrowly-scoped legacy rollback when an
// older Selene link has exactly one adjacent backup directory.
func RollbackLatestCompatdataMigration(env planner.Environment, library SteamLibrary) (CompatdataResult, error) {
	if err := validateEnvironment(env); err != nil {
		return CompatdataResult{}, err
	}
	if steamRunning() {
		return CompatdataResult{}, errors.New("close Steam completely before rolling back compatdata")
	}
	if journal, ok := latestCompatdataJournal(env, library); ok {
		return RollbackCompatdataMigration(env, journal.ID)
	}
	plan, err := PlanCompatdataMigration(env, library)
	if err != nil {
		return CompatdataResult{}, err
	}
	if plan.BlockedReason != "" {
		return CompatdataResult{}, errors.New(plan.BlockedReason)
	}
	if plan.CurrentState != CompatdataManagedLink {
		return CompatdataResult{}, errors.New("compatdata is not a Selene-managed link")
	}
	backup, ok := legacyCompatdataBackup(library)
	if !ok {
		return CompatdataResult{}, errors.New("no unambiguous Selene compatdata backup was found for rollback")
	}
	plan.BackupPath = backup
	plan.RequiresBackup = true
	if err := validateCompatdataBackup(plan.BackupPath); err != nil {
		return CompatdataResult{}, err
	}
	state, _, err := InspectCompatdata(env, library)
	if err != nil {
		return CompatdataResult{}, err
	}
	if state != CompatdataManagedLink {
		return CompatdataResult{}, errors.New("compatdata no longer points to a Selene-managed link; refusing rollback")
	}
	if err := restoreLegacyCompatdataBackup(plan); err != nil {
		return CompatdataResult{}, err
	}
	return CompatdataResult{Plan: plan}, nil
}

func availableBackupPath(library SteamLibrary) string {
	base := filepath.Join(library.Path, "steamapps", backupPrefix+time.Now().UTC().Format("20060102-150405"))
	for suffix := 0; ; suffix++ {
		candidate := base
		if suffix > 0 {
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		} else if err != nil {
			// Apply will re-check this exact path before renaming. Returning it
			// here keeps planning read-only while avoiding an unbounded retry on
			// an inaccessible directory.
			return candidate
		}
	}
}

// libraryID derives a stable identifier from the library's absolute path,
// preferring the filesystem UUID when it can be resolved.
func libraryID(library SteamLibrary) string {
	mounts, err := readMounts()
	if err == nil {
		if mount, ok := findMountFor(mounts, library.Path); ok {
			if id := resolveFilesystemID(mount); id != "" {
				return libraryIDWithFilesystem(library, mount.point, id)
			}
		}
	}
	digest := sha256.Sum256([]byte(filepath.Clean(library.Path)))
	return hex.EncodeToString(digest[:8])
}

// legacyLibraryID preserves the identity format used before Selene looked up
// the filesystem UUID. It is only used to recognise already-created links.
func legacyLibraryID(library SteamLibrary) string {
	mounts, err := readMounts()
	if err == nil {
		if mount, ok := findMountFor(mounts, library.Path); ok {
			if sourceID := sourceHash(mount.source); sourceID != "" {
				return libraryIDWithFilesystem(library, mount.point, sourceID)
			}
		}
	}
	digest := sha256.Sum256([]byte(filepath.Clean(library.Path)))
	return hex.EncodeToString(digest[:8])
}

func libraryIDWithFilesystem(library SteamLibrary, mountPoint, filesystemID string) string {
	relative, err := filepath.Rel(mountPoint, library.Path)
	if err != nil {
		digest := sha256.Sum256([]byte(filepath.Clean(library.Path)))
		return hex.EncodeToString(digest[:8])
	}
	digest := sha256.Sum256([]byte(filepath.Clean(relative)))
	return filesystemID + "-" + hex.EncodeToString(digest[:4])
}

// resolveFilesystemID looks up a stable UUID for a mounted device. Device
// paths such as /dev/sdb2 are not stable across boots, so they are used only
// as the last-resort source hash below.
func resolveFilesystemID(mount mount) string {
	if strings.HasPrefix(mount.source, "UUID=") {
		return safeFilesystemID(strings.TrimPrefix(mount.source, "UUID="))
	}
	source := filepath.Clean(mount.source)
	if source == "" || source == "none" {
		return ""
	}
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err == nil {
		source = filepath.Clean(resolvedSource)
	}
	entries, err := os.ReadDir("/dev/disk/by-uuid")
	if err == nil {
		for _, entry := range entries {
			candidate, resolveErr := filepath.EvalSymlinks(filepath.Join("/dev/disk/by-uuid", entry.Name()))
			if resolveErr == nil && filepath.Clean(candidate) == source {
				return safeFilesystemID(entry.Name())
			}
		}
	}
	return sourceHash(source)
}

func sourceHash(source string) string {
	if source == "" || source == "none" {
		return ""
	}
	digest := sha256.Sum256([]byte(filepath.Clean(source)))
	return hex.EncodeToString(digest[:8])
}

func safeFilesystemID(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func readMounts() ([]mount, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, fmt.Errorf("read mounted disks: %w", err)
	}
	return parseMountInfoLines(data)
}

func resolveLinkTarget(link, target string) string {
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(link), target))
}

func samePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if resolved, err := filepath.EvalSymlinks(left); err == nil {
		left = filepath.Clean(resolved)
	}
	if resolved, err := filepath.EvalSymlinks(right); err == nil {
		right = filepath.Clean(resolved)
	}
	return left == right
}

func nativeTargetConflict(path string) string {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		return fmt.Sprintf("inspect native compatdata target: %v", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "the native compatdata target already exists but is not a directory"
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Sprintf("read native compatdata target: %v", err)
	}
	if len(entries) > 0 {
		return "the native compatdata target already contains data; Selene will not merge into it"
	}
	return ""
}

func ensureCompatdataLibraryWritable(library SteamLibrary) error {
	mounts, err := readMounts()
	if err != nil {
		return err
	}
	if mount, ok := findMountFor(mounts, library.Path); ok && mount.point == library.MountPoint && isNTFS(mount.filesystem) && mount.readOnly {
		return errors.New("the NTFS Steam library is mounted read-only")
	}
	return nil
}

func compatdataMigrationDescription(library SteamLibrary) string {
	return "plugin steam-compatdata migrate " + CompatdataPath(library)
}

func persistCompatdataJournal(tx *transaction.Transaction, plan CompatdataPlan) error {
	data, err := json.MarshalIndent(compatdataJournal{SchemaVersion: 1, Plan: plan}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode compatdata migration metadata: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(tx.Journal.Root, compatdataJournalFile)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("write compatdata migration metadata: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("activate compatdata migration metadata: %w", err)
	}
	return nil
}

func loadCompatdataJournal(tx *transaction.Transaction) (CompatdataPlan, error) {
	data, err := os.ReadFile(filepath.Join(tx.Journal.Root, compatdataJournalFile))
	if err != nil {
		return CompatdataPlan{}, fmt.Errorf("read compatdata migration metadata: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var journal compatdataJournal
	if err := decoder.Decode(&journal); err != nil {
		return CompatdataPlan{}, fmt.Errorf("decode compatdata migration metadata: %w", err)
	}
	if journal.SchemaVersion != 1 || journal.Plan.Library.Path == "" || journal.Plan.Compatdata == "" || journal.Plan.NativeTarget == "" {
		return CompatdataPlan{}, errors.New("invalid compatdata migration metadata")
	}
	if filepath.Clean(journal.Plan.Compatdata) != filepath.Clean(CompatdataPath(journal.Plan.Library)) {
		return CompatdataPlan{}, errors.New("compatdata migration metadata has an invalid source path")
	}
	return journal.Plan, nil
}

func latestCompatdataJournal(env planner.Environment, library SteamLibrary) (transaction.Journal, bool) {
	journals, err := transaction.List(stateRoot(env))
	if err != nil {
		return transaction.Journal{}, false
	}
	for _, journal := range journals {
		if journal.State != transaction.StateCommitted || journal.Description != compatdataMigrationDescription(library) {
			continue
		}
		tx, openErr := transaction.Open(stateRoot(env), journal.ID)
		if openErr != nil {
			continue
		}
		if _, metadataErr := loadCompatdataJournal(tx); metadataErr == nil {
			return journal, true
		}
	}
	return transaction.Journal{}, false
}

func compatdataRollbackBackup(env planner.Environment, library SteamLibrary) (string, bool) {
	if journal, ok := latestCompatdataJournal(env, library); ok {
		tx, err := transaction.Open(stateRoot(env), journal.ID)
		if err == nil {
			plan, metadataErr := loadCompatdataJournal(tx)
			if metadataErr == nil && plan.RequiresBackup && plan.BackupPath != "" {
				return plan.BackupPath, true
			}
			if metadataErr == nil && !plan.RequiresBackup {
				return "", true
			}
		}
	}
	return legacyCompatdataBackup(library)
}

func legacyCompatdataBackup(library SteamLibrary) (string, bool) {
	paths, err := filepath.Glob(filepath.Join(library.Path, "steamapps", backupPrefix+"*"))
	if err != nil {
		return "", false
	}
	var directories []string
	for _, path := range paths {
		info, statErr := os.Lstat(path)
		if statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			directories = append(directories, path)
		}
	}
	if len(directories) != 1 {
		return "", false
	}
	return directories[0], true
}

func validateCompatdataBackup(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect original compatdata backup: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("original compatdata backup is not a directory; refusing rollback")
	}
	return nil
}

func restoreCompatdataBackup(plan CompatdataPlan) error {
	info, err := os.Lstat(plan.Compatdata)
	if err != nil {
		return fmt.Errorf("inspect restored compatdata snapshot: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("restored compatdata snapshot is not a directory; refusing backup replacement")
	}
	if err := os.RemoveAll(plan.Compatdata); err != nil {
		return fmt.Errorf("remove restored compatdata snapshot: %w", err)
	}
	if err := os.Rename(plan.BackupPath, plan.Compatdata); err != nil {
		return fmt.Errorf("restore original compatdata backup: %w", err)
	}
	return nil
}

func restoreLegacyCompatdataBackup(plan CompatdataPlan) error {
	info, err := os.Lstat(plan.Compatdata)
	if err != nil {
		return fmt.Errorf("inspect compatdata link: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return errors.New("compatdata is no longer a symlink; refusing legacy rollback")
	}
	if err := os.Remove(plan.Compatdata); err != nil {
		return fmt.Errorf("remove managed compatdata link: %w", err)
	}
	if err := os.Rename(plan.BackupPath, plan.Compatdata); err != nil {
		return fmt.Errorf("restore original compatdata backup: %w", err)
	}
	return nil
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
	if runtime.GOOS != "linux" {
		return false
	}
	info, err := os.Stat("/proc/" + pid)
	if err != nil {
		return false
	}
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	uidField := value.FieldByName("Uid")
	if !uidField.IsValid() || !uidField.CanUint() {
		return false
	}
	return int(uidField.Uint()) == uid
}
