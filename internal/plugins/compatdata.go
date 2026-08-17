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
	// MountAssessment is independent from the compatdata link. It reports
	// whether the active NTFS mount preserves the filename spelling installed
	// by Steam, which some games require for assets and integrity metadata.
	MountAssessment NTFSMountAssessment `json:"-"`
	// PreservedNativeTarget is populated when a previous rollback left data at
	// the deterministic target. Apply atomically moves that data here before
	// starting a fresh migration, so recovery never requires manual cleanup.
	PreservedNativeTarget string
	BackupPath            string
	ImportSource          string

	CurrentState      CompatdataState
	LinkTarget        string
	BlockedReason     string
	RollbackAvailable bool

	RequiresCopy         bool
	RequiresBackup       bool
	DetachesExistingLink bool
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
	nativeRollbackMarker  = ".selene-rollback-"
	nativeTargetHasData   = "the native compatdata target already contains data; Selene will not merge into it"
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

// managedNativeTargetPaths includes the normalized UUID identity, historical
// case-sensitive/source-hash identities, and the target recorded by the latest
// committed journal. This keeps links created by earlier Selene builds fully
// manageable after identity rules evolve.
func managedNativeTargetPaths(env planner.Environment, library SteamLibrary) []string {
	var paths []string
	for _, id := range []string{
		libraryID(library),
		caseSensitiveLibraryID(library),
		legacyLibraryID(library),
		caseSensitiveLegacyLibraryID(library),
	} {
		candidate := filepath.Join(NativeCompatdataRoot(env), id)
		duplicate := false
		for _, existing := range paths {
			if filepath.Clean(existing) == filepath.Clean(candidate) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			paths = append(paths, candidate)
		}
	}
	if journal, ok := latestCompatdataJournal(env, library); ok {
		if tx, err := transaction.Open(stateRoot(env), journal.ID); err == nil {
			if plan, err := loadCompatdataJournal(tx); err == nil {
				candidate := filepath.Clean(plan.NativeTarget)
				if filepath.Dir(candidate) != filepath.Clean(NativeCompatdataRoot(env)) {
					return paths
				}
				duplicate := false
				for _, existing := range paths {
					if filepath.Clean(existing) == candidate {
						duplicate = true
						break
					}
				}
				if !duplicate {
					paths = append(paths, candidate)
				}
			}
		}
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
		Library:         library,
		Compatdata:      CompatdataPath(library),
		NativeTarget:    nativeTargetPath(env, library),
		MountAssessment: InspectNTFSFilenameCompatibility(library),
		BackupPath:      availableBackupPath(library),
		CurrentState:    state,
		LinkTarget:      linkTarget,
	}

	switch state {
	case CompatdataInvalid:
		plan.BlockedReason = "compatdata is a regular file; Selene will not overwrite it"
	case CompatdataExternalLink:
		plan.DetachesExistingLink = true
		plan.ImportSource = canonicalPath(resolveLinkTarget(plan.Compatdata, linkTarget))
		info, statErr := os.Stat(plan.ImportSource)
		if statErr != nil {
			plan.BlockedReason = fmt.Sprintf("inspect the existing compatdata link target: %v", statErr)
		} else if !info.IsDir() {
			plan.BlockedReason = "the existing compatdata link does not point to a directory"
		} else {
			resolvedNativeTarget, resolveErr := resolvePathForMount(plan.NativeTarget)
			if resolveErr != nil {
				return CompatdataPlan{}, resolveErr
			}
			if pathsOverlap(plan.ImportSource, resolvedNativeTarget) {
				plan.BlockedReason = "the existing compatdata link target overlaps Selene's native target"
			} else {
				plan.RequiresCopy = true
			}
		}
	case CompatdataBrokenLink:
		plan.DetachesExistingLink = true
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
	if canMigrateCompatdataState(state) {
		if mount, ok, resolveErr := findMountForResolvedPath(mounts, library.Path); resolveErr != nil {
			return CompatdataPlan{}, resolveErr
		} else if ok && isNTFS(mount.filesystem) && mount.readOnly {
			plan.BlockedReason = "the NTFS Steam library is mounted read-only"
		}
		if mount, ok, resolveErr := findMountForResolvedPath(mounts, plan.NativeTarget); resolveErr != nil {
			return CompatdataPlan{}, resolveErr
		} else if ok {
			if isNTFS(mount.filesystem) {
				plan.BlockedReason = "the Proton compatdata target is also on NTFS; choose a Linux native filesystem"
			} else if mount.readOnly {
				plan.BlockedReason = "the native compatdata target filesystem is mounted read-only"
			}
		}
		if reason := nativeTargetConflict(plan.NativeTarget); reason != "" && plan.BlockedReason == "" {
			if journal, ok := rolledBackCompatdataJournal(env, library, plan.NativeTarget); ok && reason == nativeTargetHasData {
				plan.PreservedNativeTarget = availablePreservedNativeTarget(plan.NativeTarget, journal.ID)
			} else {
				plan.BlockedReason = reason
			}
		}
	} else if state == CompatdataManagedLink && plan.RollbackAvailable {
		if mount, ok, resolveErr := findMountForResolvedPath(mounts, library.Path); resolveErr != nil {
			return CompatdataPlan{}, resolveErr
		} else if ok && isNTFS(mount.filesystem) && mount.readOnly {
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
	if freshPlan.CurrentState != plan.CurrentState && freshPlan.CurrentState != CompatdataManagedLink {
		return CompatdataResult{}, errors.New("compatdata changed after confirmation; review the setup plan again")
	}
	if plan.CurrentState == CompatdataExternalLink &&
		!samePath(plan.ImportSource, freshPlan.ImportSource) {
		return CompatdataResult{}, errors.New("the external compatdata link changed after confirmation; review the plan again")
	}
	if plan.CurrentState == CompatdataBrokenLink &&
		filepath.Clean(resolveLinkTarget(plan.Compatdata, plan.LinkTarget)) != filepath.Clean(resolveLinkTarget(freshPlan.Compatdata, freshPlan.LinkTarget)) {
		return CompatdataResult{}, errors.New("the broken compatdata link changed after confirmation; review the plan again")
	}

	// Re-check state to keep the operation idempotent and safe.
	state, currentLinkTarget, err := InspectCompatdata(env, freshPlan.Library)
	if err != nil {
		return CompatdataResult{}, err
	}
	switch state {
	case CompatdataManagedLink:
		return CompatdataResult{Plan: freshPlan}, nil
	case CompatdataInvalid:
		return CompatdataResult{}, errors.New("compatdata is a regular file; refusing to configure it")
	}

	if state == CompatdataDirectory && !freshPlan.RequiresCopy {
		return CompatdataResult{}, errors.New("compatdata is a directory but the plan does not require a copy")
	}
	if state == CompatdataExternalLink && (!freshPlan.DetachesExistingLink || !freshPlan.RequiresCopy || freshPlan.ImportSource == "") {
		return CompatdataResult{}, errors.New("the external compatdata link does not have a safe import plan")
	}
	if state == CompatdataExternalLink && !samePath(resolveLinkTarget(freshPlan.Compatdata, currentLinkTarget), freshPlan.ImportSource) {
		return CompatdataResult{}, errors.New("the external compatdata link changed after confirmation; review the plan again")
	}
	if state == CompatdataBrokenLink && !freshPlan.DetachesExistingLink {
		return CompatdataResult{}, errors.New("the broken compatdata link does not have a safe replacement plan")
	}
	if state == CompatdataBrokenLink && filepath.Clean(resolveLinkTarget(freshPlan.Compatdata, currentLinkTarget)) !=
		filepath.Clean(resolveLinkTarget(freshPlan.Compatdata, freshPlan.LinkTarget)) {
		return CompatdataResult{}, errors.New("the broken compatdata link changed after confirmation; review the plan again")
	}

	// Ensure Steam is not running before mutating.
	if steamRunning() {
		return CompatdataResult{}, errors.New("Steam started again; close it before configuring compatdata")
	}

	tx, err := transaction.Begin(stateRoot(env), compatdataMigrationDescription(freshPlan.Library), []transaction.Target{
		{Path: freshPlan.Compatdata, Recursive: true},
	}, nil)
	if err != nil {
		return CompatdataResult{}, fmt.Errorf("create compatdata safety snapshot: %w", err)
	}

	if err := persistCompatdataJournal(tx, freshPlan); err != nil {
		return CompatdataResult{}, abortCompatdataMigration(tx, freshPlan, false, err)
	}

	preservedNativeTarget := false
	if freshPlan.PreservedNativeTarget != "" {
		if err := moveNativeTarget(freshPlan.NativeTarget, freshPlan.PreservedNativeTarget); err != nil {
			return CompatdataResult{}, abortCompatdataMigration(tx, freshPlan, false, err)
		}
		preservedNativeTarget = true
	}

	if err := os.MkdirAll(freshPlan.NativeTarget, 0o700); err != nil {
		return CompatdataResult{}, abortCompatdataMigration(tx, freshPlan, preservedNativeTarget, fmt.Errorf("create native compatdata target: %w", err))
	}

	if freshPlan.RequiresCopy {
		copySource := freshPlan.Compatdata
		if state == CompatdataExternalLink {
			copySource = freshPlan.ImportSource
		}
		if err := copyCompatdata(copySource, freshPlan.NativeTarget); err != nil {
			return CompatdataResult{}, abortCompatdataMigration(tx, freshPlan, preservedNativeTarget, fmt.Errorf("copy compatdata: %w", err))
		}
		if err := verifyCopy(copySource, freshPlan.NativeTarget); err != nil {
			return CompatdataResult{}, abortCompatdataMigration(tx, freshPlan, preservedNativeTarget, fmt.Errorf("verify copied compatdata: %w", err))
		}
	}

	if state == CompatdataDirectory && freshPlan.RequiresBackup {
		if err := os.Rename(freshPlan.Compatdata, freshPlan.BackupPath); err != nil {
			return CompatdataResult{}, abortCompatdataMigration(tx, freshPlan, preservedNativeTarget, fmt.Errorf("back up original compatdata: %w", err))
		}
	}
	if freshPlan.DetachesExistingLink {
		if err := removeExistingCompatdataLink(freshPlan.Compatdata); err != nil {
			return CompatdataResult{}, abortCompatdataMigration(tx, freshPlan, preservedNativeTarget, err)
		}
	}

	if err := os.Symlink(freshPlan.NativeTarget, freshPlan.Compatdata); err != nil {
		return CompatdataResult{}, abortCompatdataMigration(tx, freshPlan, preservedNativeTarget, fmt.Errorf("create compatdata symlink: %w", err))
	}

	if err := verifyLink(freshPlan.Compatdata, freshPlan.NativeTarget); err != nil {
		return CompatdataResult{}, abortCompatdataMigration(tx, freshPlan, preservedNativeTarget, err)
	}

	if err := writeProbe(freshPlan.Compatdata, freshPlan.NativeTarget); err != nil {
		return CompatdataResult{}, abortCompatdataMigration(tx, freshPlan, preservedNativeTarget, fmt.Errorf("verify compatdata write path: %w", err))
	}

	if err := tx.Commit(); err != nil {
		return CompatdataResult{}, abortCompatdataMigration(tx, freshPlan, preservedNativeTarget, fmt.Errorf("commit compatdata setup: %w", err))
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
		return CompatdataResult{}, errors.New("Steam started again; close it before restoring compatdata")
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
		return CompatdataResult{}, errors.New("transaction is not a Selene compatdata setup")
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
		return CompatdataResult{}, errors.New("compatdata no longer points to this Selene setup; refusing restore")
	}
	if plan.RequiresBackup {
		if err := validateCompatdataBackup(plan.BackupPath); err != nil {
			return CompatdataResult{}, err
		}
	}
	if err := tx.Rollback(); err != nil {
		return CompatdataResult{}, fmt.Errorf("restore compatdata setup: %w", err)
	}
	if plan.RequiresBackup {
		if err := restoreCompatdataBackup(plan); err != nil {
			return CompatdataResult{}, err
		}
	}
	preserved := availablePreservedNativeTarget(plan.NativeTarget, tx.Journal.ID)
	if err := moveNativeTarget(plan.NativeTarget, preserved); err != nil {
		return CompatdataResult{}, fmt.Errorf("preserve native compatdata after rollback: %w", err)
	}
	plan.PreservedNativeTarget = preserved
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
		return CompatdataResult{}, errors.New("Steam started again; close it before restoring compatdata")
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
	preserved := availablePreservedNativeTarget(plan.NativeTarget, time.Now().UTC().Format("20060102T150405Z"))
	if err := moveNativeTarget(plan.NativeTarget, preserved); err != nil {
		return CompatdataResult{}, fmt.Errorf("preserve native compatdata after rollback: %w", err)
	}
	plan.PreservedNativeTarget = preserved
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

func availablePreservedNativeTarget(path, rollbackID string) string {
	base := filepath.Clean(path) + nativeRollbackMarker + rollbackID
	for suffix := 0; ; suffix++ {
		candidate := base
		if suffix > 0 {
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		} else if err != nil {
			return candidate
		}
	}
}

// moveNativeTarget archives a native compatdata tree with one same-filesystem
// rename. It deliberately refuses links and cross-directory destinations so a
// stale or edited journal cannot expand the mutation scope.
func moveNativeTarget(source, destination string) error {
	source = filepath.Clean(source)
	destination = filepath.Clean(destination)
	if !filepath.IsAbs(source) || !filepath.IsAbs(destination) || filepath.Dir(source) != filepath.Dir(destination) || source == destination {
		return errors.New("invalid native compatdata preservation path")
	}
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("inspect native compatdata target: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("native compatdata target is not a directory; refusing to preserve it")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("native compatdata preservation path already exists: %s", destination)
		}
		return fmt.Errorf("inspect native compatdata preservation path: %w", err)
	}
	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("archive native compatdata target: %w", err)
	}
	return nil
}

func restorePreservedNativeTarget(plan CompatdataPlan) error {
	if plan.PreservedNativeTarget == "" {
		return nil
	}
	target := filepath.Clean(plan.NativeTarget)
	preserved := filepath.Clean(plan.PreservedNativeTarget)
	if filepath.Dir(target) != filepath.Dir(preserved) || target == preserved {
		return errors.New("invalid preserved native compatdata recovery path")
	}
	if info, err := os.Lstat(target); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("replacement native compatdata target changed type; refusing recovery")
		}
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("remove incomplete native compatdata replacement: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect incomplete native compatdata replacement: %w", err)
	}
	if err := os.Rename(preserved, target); err != nil {
		return fmt.Errorf("restore preserved native compatdata target: %w", err)
	}
	return nil
}

func abortCompatdataMigration(tx *transaction.Transaction, plan CompatdataPlan, preservedNativeTarget bool, cause error) error {
	_ = tx.MarkFailed(cause)
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		return fmt.Errorf("%w; plugin rollback also failed: %v", cause, rollbackErr)
	}
	if preservedNativeTarget {
		if restoreErr := restorePreservedNativeTarget(plan); restoreErr != nil {
			return fmt.Errorf("%w; preserved native compatdata recovery also failed: %v", cause, restoreErr)
		}
	}
	return cause
}

// libraryID derives a stable identifier from the library's absolute path,
// preferring the filesystem UUID when it can be resolved.
func libraryID(library SteamLibrary) string {
	return filesystemLibraryID(library, false, false)
}

// caseSensitiveLibraryID preserves the UUID-based identifier emitted before
// NTFS paths were normalized. It is recognition-only compatibility for links
// already created with a differently-cased path from libraryfolders.vdf.
func caseSensitiveLibraryID(library SteamLibrary) string {
	return filesystemLibraryID(library, false, true)
}

func filesystemLibraryID(library SteamLibrary, legacySource, preserveCase bool) string {
	mounts, err := readMounts()
	if err == nil {
		if mount, ok := findMountFor(mounts, library.Path); ok {
			id := resolveFilesystemID(mount)
			if legacySource {
				id = sourceHash(mount.source)
			}
			if id != "" {
				return libraryIDWithFilesystemCase(library, mount.point, id, preserveCase)
			}
		}
	}
	path := filepath.Clean(library.Path)
	if isNTFS(library.Filesystem) && !preserveCase {
		path = strings.ToLower(path)
	}
	digest := sha256.Sum256([]byte(path))
	return hex.EncodeToString(digest[:8])
}

// legacyLibraryID preserves the identity format used before Selene looked up
// the filesystem UUID. It is only used to recognise already-created links.
func legacyLibraryID(library SteamLibrary) string {
	return filesystemLibraryID(library, true, false)
}

func caseSensitiveLegacyLibraryID(library SteamLibrary) string {
	return filesystemLibraryID(library, true, true)
}

func libraryIDWithFilesystem(library SteamLibrary, mountPoint, filesystemID string) string {
	return libraryIDWithFilesystemCase(library, mountPoint, filesystemID, false)
}

func libraryIDWithFilesystemCase(library SteamLibrary, mountPoint, filesystemID string, preserveCase bool) string {
	relative, err := filepath.Rel(mountPoint, library.Path)
	if err != nil {
		path := filepath.Clean(library.Path)
		if isNTFS(library.Filesystem) && !preserveCase {
			path = strings.ToLower(path)
		}
		digest := sha256.Sum256([]byte(path))
		return hex.EncodeToString(digest[:8])
	}
	if isNTFS(library.Filesystem) && !preserveCase {
		relative = strings.ToLower(relative)
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

func canMigrateCompatdataState(state CompatdataState) bool {
	return state == CompatdataDirectory || state == CompatdataMissing ||
		state == CompatdataExternalLink || state == CompatdataBrokenLink
}

// findMountForResolvedPath classifies the filesystem using the physical path.
// This matters on immutable distributions where /home is commonly a symlink to
// /var/home: mountinfo describes /var/home, while XDG paths still use /home.
// The destination may not exist yet, so resolvePathForMount first resolves the
// nearest existing ancestor and then appends the missing suffix again.
func findMountForResolvedPath(mounts []mount, path string) (mount, bool, error) {
	resolved, err := resolvePathForMount(path)
	if err != nil {
		return mount{}, false, err
	}
	found, ok := findMountFor(mounts, resolved)
	return found, ok, nil
}

func resolvePathForMount(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("resolve mount path %q: path is not absolute", path)
	}
	current := filepath.Clean(path)
	var missing []string
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", fmt.Errorf("resolve mount path %s: %w", path, err)
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect mount path ancestor %s: %w", current, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("resolve mount path %s: no existing ancestor", path)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func resolveLinkTarget(link, target string) string {
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(link), target))
}

func pathsOverlap(left, right string) bool {
	return isWithin(left, right) || isWithin(right, left)
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
		return nativeTargetHasData
	}
	return ""
}

func removeExistingCompatdataLink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect existing compatdata link: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return errors.New("compatdata changed and is no longer a symlink; refusing to replace it")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("detach existing compatdata link: %w", err)
	}
	return nil
}

func ensureCompatdataLibraryWritable(library SteamLibrary) error {
	mounts, err := readMounts()
	if err != nil {
		return err
	}
	if mount, ok, err := findMountForResolvedPath(mounts, library.Path); err != nil {
		return err
	} else if ok && isNTFS(mount.filesystem) && mount.readOnly {
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
		return fmt.Errorf("encode compatdata setup metadata: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(tx.Journal.Root, compatdataJournalFile)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("write compatdata setup metadata: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("activate compatdata setup metadata: %w", err)
	}
	return nil
}

func loadCompatdataJournal(tx *transaction.Transaction) (CompatdataPlan, error) {
	data, err := os.ReadFile(filepath.Join(tx.Journal.Root, compatdataJournalFile))
	if err != nil {
		return CompatdataPlan{}, fmt.Errorf("read compatdata setup metadata: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var journal compatdataJournal
	if err := decoder.Decode(&journal); err != nil {
		return CompatdataPlan{}, fmt.Errorf("decode compatdata setup metadata: %w", err)
	}
	if journal.SchemaVersion != 1 || journal.Plan.Library.Path == "" || journal.Plan.Compatdata == "" || journal.Plan.NativeTarget == "" {
		return CompatdataPlan{}, errors.New("invalid compatdata setup metadata")
	}
	if filepath.Clean(journal.Plan.Compatdata) != filepath.Clean(CompatdataPath(journal.Plan.Library)) {
		return CompatdataPlan{}, errors.New("compatdata setup metadata has an invalid source path")
	}
	return journal.Plan, nil
}

func latestCompatdataJournal(env planner.Environment, library SteamLibrary) (transaction.Journal, bool) {
	journals, err := transaction.List(stateRoot(env))
	if err != nil {
		return transaction.Journal{}, false
	}
	for _, journal := range journals {
		if journal.State != transaction.StateCommitted {
			continue
		}
		tx, openErr := transaction.Open(stateRoot(env), journal.ID)
		if openErr != nil {
			continue
		}
		plan, metadataErr := loadCompatdataJournal(tx)
		if metadataErr == nil && journal.Description == compatdataMigrationDescription(plan.Library) && sameSteamLibrary(plan.Library, library) {
			return journal, true
		}
	}
	return transaction.Journal{}, false
}

func rolledBackCompatdataJournal(env planner.Environment, library SteamLibrary, nativeTarget string) (transaction.Journal, bool) {
	journals, err := transaction.List(stateRoot(env))
	if err != nil {
		return transaction.Journal{}, false
	}
	for _, journal := range journals {
		if journal.State != transaction.StateRolledBack {
			continue
		}
		tx, openErr := transaction.Open(stateRoot(env), journal.ID)
		if openErr != nil {
			continue
		}
		plan, metadataErr := loadCompatdataJournal(tx)
		if metadataErr == nil && journal.Description == compatdataMigrationDescription(plan.Library) &&
			sameSteamLibrary(plan.Library, library) && samePath(plan.NativeTarget, nativeTarget) {
			return journal, true
		}
	}
	return transaction.Journal{}, false
}

func sameSteamLibrary(left, right SteamLibrary) bool {
	leftMount := filepath.Clean(left.MountPoint)
	rightMount := filepath.Clean(right.MountPoint)
	if left.MountPoint != "" && right.MountPoint != "" && leftMount != rightMount {
		return false
	}
	if leftMount == rightMount {
		leftRelative, leftErr := filepath.Rel(leftMount, filepath.Clean(left.Path))
		rightRelative, rightErr := filepath.Rel(rightMount, filepath.Clean(right.Path))
		if leftErr == nil && rightErr == nil {
			if isNTFS(left.Filesystem) && isNTFS(right.Filesystem) {
				return strings.EqualFold(leftRelative, rightRelative)
			}
			return leftRelative == rightRelative
		}
	}
	if isNTFS(left.Filesystem) && isNTFS(right.Filesystem) {
		return strings.EqualFold(filepath.Clean(left.Path), filepath.Clean(right.Path))
	}
	return filepath.Clean(left.Path) == filepath.Clean(right.Path)
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
			// Wine prefixes contain links such as dosdevices/c:. Recreate the
			// link itself without following or reading from its destination.
			linkTarget, err := os.Readlink(current)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
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
		switch {
		case srcInfo.Mode()&os.ModeSymlink != 0:
			if dstInfo.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("type mismatch at %s", target)
			}
			sourceLink, err := os.Readlink(current)
			if err != nil {
				return err
			}
			targetLink, err := os.Readlink(target)
			if err != nil {
				return err
			}
			if sourceLink != targetLink {
				return fmt.Errorf("symlink target mismatch at %s", target)
			}
		case srcInfo.IsDir():
			if !dstInfo.IsDir() || dstInfo.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("type mismatch at %s", target)
			}
		case srcInfo.Mode().IsRegular():
			if !dstInfo.Mode().IsRegular() {
				return fmt.Errorf("type mismatch at %s", target)
			}
			if srcInfo.Size() != dstInfo.Size() {
				return fmt.Errorf("size mismatch at %s", target)
			}
		default:
			return fmt.Errorf("unsupported file type inside compatdata: %s", current)
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
		return errors.New("compatdata is not a symlink after setup")
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
