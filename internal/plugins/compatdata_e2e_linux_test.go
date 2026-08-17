//go:build linux

package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/selene-linux/selene/internal/planner"
)

// These tests use real Linux symbolic links, but keep the NTFS volume fully
// simulated in t.TempDir. The synthetic mount table makes the library look
// like an ntfs3 mount while every mutation remains inside the test directory.

func TestCompatdataE2EMigrateAndRollbackConfiguredNTFSLibrary(t *testing.T) {
	if steamRunning() {
		t.Skip("Steam is running; compatdata migration deliberately refuses to mutate while it is open")
	}
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
	assertFile(t, filepath.Join(result.Plan.NativeTarget, "620", "pfx", "drive_c", "save.dat"), "original prefix")
	if _, statErr := os.Lstat(result.Plan.BackupPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("backup should have been renamed back to compatdata, stat error = %v", statErr)
	}
}

func TestCompatdataE2ERollsBackExistingManagedLinkWithLegacyBackup(t *testing.T) {
	if steamRunning() {
		t.Skip("Steam is running; compatdata rollback deliberately refuses to mutate while it is open")
	}
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

	if _, err := RollbackLatestCompatdataMigration(fixture.env, fixture.library); err != nil {
		t.Fatal(err)
	}
	assertCompatdataDirectory(t, compatdata)
	assertFile(t, filepath.Join(compatdata, "620", "pfx", "drive_c", "save.dat"), "original prefix")
	if info, err := os.Lstat(nativeTarget); err != nil || !info.IsDir() {
		t.Fatalf("native compatdata target should be preserved: %v, %v", info, err)
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
