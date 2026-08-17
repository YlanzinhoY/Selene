//go:build linux

package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/transaction"
)

// These tests use real Linux symbolic links, but keep the NTFS volume fully
// simulated in t.TempDir. The synthetic mount table makes the library look
// like an ntfs3 mount while every mutation remains inside the test directory.

func TestCompatdataE2EMigrateAndRollbackConfiguredNTFSLibrary(t *testing.T) {
	stubSteamClosed(t)
	fixture := newCompatdataE2EFixture(t)

	libraries := discoverSteamLibraries(fixture.env, fixture.mounts)
	if len(libraries) != 1 || libraries[0].Path != fixture.library.Path {
		t.Fatalf("configured NTFS library discovery = %#v", libraries)
	}

	plan, err := PlanCompatdataMigration(fixture.env, libraries[0])
	if err != nil {
		t.Fatal(err)
	}
	if plan.CurrentState != CompatdataDirectory || !plan.RequiresCopy || !plan.RequiresBackup || plan.BlockedReason != "" {
		t.Fatalf("migration plan = %#v", plan)
	}

	result, err := ApplyCompatdataMigration(fixture.env, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.TransactionID == "" {
		t.Fatalf("migration did not create a transaction: %#v", result)
	}
	assertCompatdataLink(t, result.Plan.Compatdata, result.Plan.NativeTarget)
	assertFile(t, filepath.Join(result.Plan.NativeTarget, "620", "pfx", "drive_c", "save.dat"), "original prefix")
	assertLinkTarget(t, filepath.Join(result.Plan.NativeTarget, "620", "pfx", "dosdevices", "c:"), "../drive_c")
	if info, statErr := os.Lstat(result.Plan.BackupPath); statErr != nil || !info.IsDir() {
		t.Fatalf("original NTFS compatdata backup = %v, %v", info, statErr)
	}

	managedPlan, err := PlanCompatdataMigration(fixture.env, libraries[0])
	if err != nil {
		t.Fatal(err)
	}
	if managedPlan.CurrentState != CompatdataManagedLink || !managedPlan.RollbackAvailable {
		t.Fatalf("managed-link plan = %#v", managedPlan)
	}

	rollback, err := RollbackLatestCompatdataMigration(fixture.env, libraries[0])
	if err != nil {
		t.Fatal(err)
	}
	if rollback.TransactionID != result.TransactionID {
		t.Fatalf("rollback transaction = %q, want %q", rollback.TransactionID, result.TransactionID)
	}
	assertCompatdataDirectory(t, result.Plan.Compatdata)
	assertFile(t, filepath.Join(result.Plan.Compatdata, "620", "pfx", "drive_c", "save.dat"), "original prefix")
	if rollback.Plan.PreservedNativeTarget == "" {
		t.Fatal("rollback did not return the preserved native prefix path")
	}
	assertFile(t, filepath.Join(rollback.Plan.PreservedNativeTarget, "620", "pfx", "drive_c", "save.dat"), "original prefix")
	if _, statErr := os.Lstat(result.Plan.NativeTarget); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stable native target should be available after rollback: %v", statErr)
	}
	if _, statErr := os.Lstat(result.Plan.BackupPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("backup should have been renamed back to compatdata, stat error = %v", statErr)
	}

	retryPlan, err := PlanCompatdataMigration(fixture.env, libraries[0])
	if err != nil {
		t.Fatal(err)
	}
	if retryPlan.BlockedReason != "" || retryPlan.PreservedNativeTarget != "" || retryPlan.NativeTarget != result.Plan.NativeTarget {
		t.Fatalf("migration plan after rollback = %#v", retryPlan)
	}
	retry, err := ApplyCompatdataMigration(fixture.env, retryPlan)
	if err != nil {
		t.Fatal(err)
	}
	assertCompatdataLink(t, retry.Plan.Compatdata, retry.Plan.NativeTarget)
	assertFile(t, filepath.Join(retry.Plan.NativeTarget, "620", "pfx", "drive_c", "save.dat"), "original prefix")
}

func TestCompatdataE2ERollsBackExistingManagedLinkWithLegacyBackup(t *testing.T) {
	stubSteamClosed(t)
	fixture := newCompatdataE2EFixture(t)
	compatdata := CompatdataPath(fixture.library)
	backup := filepath.Join(filepath.Dir(compatdata), backupPrefix+"20260817-120000")
	nativeTarget := nativeTargetPath(fixture.env, fixture.library)
	if err := os.MkdirAll(nativeTarget, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(compatdata, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(nativeTarget, compatdata); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanCompatdataMigration(fixture.env, fixture.library)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CurrentState != CompatdataManagedLink || !plan.RollbackAvailable || plan.BackupPath != backup {
		t.Fatalf("existing-link plan = %#v", plan)
	}

	rollback, err := RollbackLatestCompatdataMigration(fixture.env, fixture.library)
	if err != nil {
		t.Fatal(err)
	}
	assertCompatdataDirectory(t, compatdata)
	assertFile(t, filepath.Join(compatdata, "620", "pfx", "drive_c", "save.dat"), "original prefix")
	if rollback.Plan.PreservedNativeTarget == "" {
		t.Fatal("legacy rollback did not report its preserved target")
	}
	if info, err := os.Lstat(rollback.Plan.PreservedNativeTarget); err != nil || !info.IsDir() {
		t.Fatalf("legacy native target was not preserved: %v, %v", info, err)
	}
	if _, err := os.Lstat(nativeTarget); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stable native target should be free after legacy rollback: %v", err)
	}
}

func TestCompatdataE2EImportsManualLinkAndRestoresItOnRollback(t *testing.T) {
	stubSteamClosed(t)
	fixture := newCompatdataE2EFixture(t)
	compatdata := CompatdataPath(fixture.library)
	manualTarget := filepath.Join(fixture.env.Home, "manual-compatdata")
	if err := os.MkdirAll(fixture.env.Home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(compatdata, manualTarget); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(manualTarget, compatdata); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanCompatdataMigration(fixture.env, fixture.library)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CurrentState != CompatdataExternalLink || !plan.DetachesExistingLink ||
		!plan.RequiresCopy || plan.RequiresBackup || plan.ImportSource != manualTarget || plan.BlockedReason != "" {
		t.Fatalf("manual-link plan = %#v", plan)
	}

	result, err := ApplyCompatdataMigration(fixture.env, plan)
	if err != nil {
		t.Fatal(err)
	}
	assertCompatdataLink(t, compatdata, result.Plan.NativeTarget)
	assertFile(t, filepath.Join(result.Plan.NativeTarget, "620", "pfx", "drive_c", "save.dat"), "original prefix")
	assertLinkTarget(t, filepath.Join(result.Plan.NativeTarget, "620", "pfx", "dosdevices", "c:"), "../drive_c")
	assertFile(t, filepath.Join(manualTarget, "620", "pfx", "drive_c", "save.dat"), "original prefix")

	managedPlan, err := PlanCompatdataMigration(fixture.env, fixture.library)
	if err != nil {
		t.Fatal(err)
	}
	if managedPlan.CurrentState != CompatdataManagedLink || !managedPlan.RollbackAvailable {
		t.Fatalf("managed manual-link plan = %#v", managedPlan)
	}

	rollback, err := RollbackLatestCompatdataMigration(fixture.env, fixture.library)
	if err != nil {
		t.Fatal(err)
	}
	assertCompatdataLink(t, compatdata, manualTarget)
	assertFile(t, filepath.Join(manualTarget, "620", "pfx", "drive_c", "save.dat"), "original prefix")
	assertFile(t, filepath.Join(rollback.Plan.PreservedNativeTarget, "620", "pfx", "drive_c", "save.dat"), "original prefix")
	if _, err := os.Lstat(result.Plan.NativeTarget); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stable native target should be free after manual-link rollback: %v", err)
	}
}

func TestCompatdataE2EReplacesBrokenManualLinkAndRestoresItOnRollback(t *testing.T) {
	stubSteamClosed(t)
	fixture := newCompatdataE2EFixture(t)
	compatdata := CompatdataPath(fixture.library)
	brokenTarget := filepath.Join(fixture.env.Home, "missing-compatdata")
	if err := os.RemoveAll(compatdata); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(brokenTarget, compatdata); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanCompatdataMigration(fixture.env, fixture.library)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CurrentState != CompatdataBrokenLink || !plan.DetachesExistingLink || plan.RequiresCopy || plan.BlockedReason != "" {
		t.Fatalf("broken-link plan = %#v", plan)
	}
	result, err := ApplyCompatdataMigration(fixture.env, plan)
	if err != nil {
		t.Fatal(err)
	}
	assertCompatdataLink(t, compatdata, result.Plan.NativeTarget)

	rollback, err := RollbackLatestCompatdataMigration(fixture.env, fixture.library)
	if err != nil {
		t.Fatal(err)
	}
	readTarget, err := os.Readlink(compatdata)
	if err != nil || readTarget != brokenTarget {
		t.Fatalf("restored broken link target = %q, %v; want %q", readTarget, err, brokenTarget)
	}
	if _, err := filepath.EvalSymlinks(compatdata); err == nil {
		t.Fatal("rollback should restore the original broken link exactly")
	}
	if info, err := os.Lstat(rollback.Plan.PreservedNativeTarget); err != nil || !info.IsDir() {
		t.Fatalf("native target from broken-link migration was not preserved: %v, %v", info, err)
	}
}

func TestCompatdataE2EAutomaticallyRecoversTargetLeftByOlderRollback(t *testing.T) {
	stubSteamClosed(t)
	fixture := newCompatdataE2EFixture(t)
	plan, err := PlanCompatdataMigration(fixture.env, fixture.library)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyCompatdataMigration(fixture.env, plan)
	if err != nil {
		t.Fatal(err)
	}

	// Reproduce the behavior of Selene versions that restored compatdata but
	// deliberately left the native tree at its deterministic path.
	tx, err := transaction.Open(stateRoot(fixture.env), result.TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := restoreCompatdataBackup(result.Plan); err != nil {
		t.Fatal(err)
	}
	newerNativeFile := filepath.Join(result.Plan.NativeTarget, "620", "pfx", "drive_c", "save.dat")
	if err := os.WriteFile(newerNativeFile, []byte("newer preserved prefix"), 0o600); err != nil {
		t.Fatal(err)
	}

	recoveryPlan, err := PlanCompatdataMigration(fixture.env, fixture.library)
	if err != nil {
		t.Fatal(err)
	}
	if recoveryPlan.BlockedReason != "" || recoveryPlan.PreservedNativeTarget == "" {
		t.Fatalf("automatic legacy recovery plan = %#v", recoveryPlan)
	}
	recovery, err := ApplyCompatdataMigration(fixture.env, recoveryPlan)
	if err != nil {
		t.Fatal(err)
	}
	assertCompatdataLink(t, recovery.Plan.Compatdata, recovery.Plan.NativeTarget)
	assertFile(t, filepath.Join(recovery.Plan.NativeTarget, "620", "pfx", "drive_c", "save.dat"), "original prefix")
	assertFile(t, filepath.Join(recovery.Plan.PreservedNativeTarget, "620", "pfx", "drive_c", "save.dat"), "newer preserved prefix")

	secondRollback, err := RollbackLatestCompatdataMigration(fixture.env, fixture.library)
	if err != nil {
		t.Fatal(err)
	}
	assertCompatdataDirectory(t, recovery.Plan.Compatdata)
	assertFile(t, filepath.Join(recovery.Plan.PreservedNativeTarget, "620", "pfx", "drive_c", "save.dat"), "newer preserved prefix")
	assertFile(t, filepath.Join(secondRollback.Plan.PreservedNativeTarget, "620", "pfx", "drive_c", "save.dat"), "original prefix")
	if secondRollback.Plan.PreservedNativeTarget == recovery.Plan.PreservedNativeTarget {
		t.Fatal("the new rollback overwrote the native copy preserved from the older rollback")
	}
}

func TestFindMountForResolvedPathUsesPhysicalExistingAncestor(t *testing.T) {
	root := t.TempDir()
	physicalHome := filepath.Join(root, "var", "home")
	userHome := filepath.Join(physicalHome, "player")
	if err := os.MkdirAll(userHome, 0o700); err != nil {
		t.Fatal(err)
	}
	logicalHome := filepath.Join(root, "home")
	if err := os.Symlink(physicalHome, logicalHome); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(logicalHome, "player", ".local", "share", "selene", "steam-compatdata", "new-target")
	mounts := []mount{
		{point: root, filesystem: "ostree", readOnly: true},
		{point: physicalHome, filesystem: "ext4"},
	}

	found, ok, err := findMountForResolvedPath(mounts, target)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || found.point != physicalHome || found.readOnly {
		t.Fatalf("resolved target mount = %#v, ok=%v", found, ok)
	}
	resolved, err := resolvePathForMount(target)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(physicalHome, "player", ".local", "share", "selene", "steam-compatdata", "new-target")
	if resolved != want {
		t.Fatalf("resolved target = %q, want %q", resolved, want)
	}
}

func TestCompatdataPlanBlocksPhysicalOverlapThroughLogicalHomeLink(t *testing.T) {
	fixture := newCompatdataE2EFixture(t)
	compatdata := CompatdataPath(fixture.library)
	root := filepath.Dir(fixture.env.Home)
	physicalHome := filepath.Join(root, "var", "home", "player")
	manualTarget := filepath.Join(physicalHome, "manual-compatdata")
	if err := os.MkdirAll(physicalHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(compatdata, manualTarget); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(manualTarget, compatdata); err != nil {
		t.Fatal(err)
	}
	logicalHome := filepath.Join(root, "logical-home")
	if err := os.Symlink(physicalHome, logicalHome); err != nil {
		t.Fatal(err)
	}
	fixture.env.XDGDataHome = filepath.Join(logicalHome, "manual-compatdata", "selene-data")

	plan, err := PlanCompatdataMigration(fixture.env, fixture.library)
	if err != nil {
		t.Fatal(err)
	}
	if plan.BlockedReason != "the existing compatdata link target overlaps Selene's native target" {
		t.Fatalf("overlap blocker = %q", plan.BlockedReason)
	}
}

type compatdataE2EFixture struct {
	env     planner.Environment
	mounts  []mount
	library SteamLibrary
}

func newCompatdataE2EFixture(t *testing.T) compatdataE2EFixture {
	t.Helper()
	root := t.TempDir()
	env := planner.Environment{
		OS:           "linux",
		Home:         filepath.Join(root, "home"),
		XDGDataHome:  filepath.Join(root, "data"),
		XDGStateHome: filepath.Join(root, "state"),
	}
	ntfsMount := filepath.Join(root, "mounted-ntfs")
	libraryPath := filepath.Join(ntfsMount, "Games", "SteamLibrary")
	compatdata := filepath.Join(libraryPath, "steamapps", "compatdata")
	prefixFile := filepath.Join(compatdata, "620", "pfx", "drive_c", "save.dat")
	if err := os.MkdirAll(filepath.Dir(prefixFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prefixFile, []byte("original prefix"), 0o600); err != nil {
		t.Fatal(err)
	}
	dosdevices := filepath.Join(compatdata, "620", "pfx", "dosdevices")
	if err := os.MkdirAll(dosdevices, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../drive_c", filepath.Join(dosdevices, "c:")); err != nil {
		t.Fatal(err)
	}

	steamRoot := filepath.Join(env.Home, ".local", "share", "Steam")
	vdfPath := filepath.Join(steamRoot, "steamapps", "libraryfolders.vdf")
	if err := os.MkdirAll(filepath.Dir(vdfPath), 0o700); err != nil {
		t.Fatal(err)
	}
	vdf := `"libraryfolders"
{
  "0"
  {
    "path" "` + libraryPath + `"
  }
}`
	if err := os.WriteFile(vdfPath, []byte(vdf), 0o600); err != nil {
		t.Fatal(err)
	}

	return compatdataE2EFixture{
		env: env,
		mounts: []mount{
			{point: string(filepath.Separator), filesystem: "ext4"},
			{point: ntfsMount, source: "/dev/selene-e2e-ntfs", filesystem: "ntfs3"},
		},
		library: SteamLibrary{
			Path:       libraryPath,
			MountPoint: ntfsMount,
			Source:     "/dev/selene-e2e-ntfs",
			Filesystem: "ntfs3",
		},
	}
}

func assertCompatdataLink(t *testing.T, link, target string) {
	t.Helper()
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("compatdata link = %v, %v", info, err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil || !samePath(resolved, target) {
		t.Fatalf("compatdata target = %q, %v; want %q", resolved, err, target)
	}
}

func assertCompatdataDirectory(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("compatdata directory = %v, %v", info, err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("file %s = %q, %v; want %q", path, data, err, want)
	}
}

func assertLinkTarget(t *testing.T, path, want string) {
	t.Helper()
	target, err := os.Readlink(path)
	if err != nil || target != want {
		t.Fatalf("link %s = %q, %v; want %q", path, target, err, want)
	}
}
