package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFindMountForPicksMostSpecificMount(t *testing.T) {
	mounts := []mount{
		{point: "/run/media", filesystem: "ntfs3"},
		{point: "/run/media/player/Games", filesystem: "ntfs3"},
		{point: "/home", filesystem: "ext4"},
	}
	got, ok := findMountFor(mounts, "/run/media/player/Games/SteamLibrary")
	if !ok || got.point != "/run/media/player/Games" {
		t.Fatalf("findMountFor() = %#v, ok=%v", got, ok)
	}
}

func TestInspectCompatdataStates(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("symlink behavior varies by environment")
	}
	root := t.TempDir()
	env := pluginEnvironment(root)
	library := SteamLibrary{Path: filepath.Join(root, "SteamLibrary")}
	steamapps := filepath.Join(library.Path, "steamapps")
	if err := os.MkdirAll(steamapps, 0o700); err != nil {
		t.Fatal(err)
	}

	// Missing.
	state, _, err := InspectCompatdata(env, library)
	if err != nil || state != CompatdataMissing {
		t.Fatalf("missing state = %q, err = %v", state, err)
	}

	// Directory.
	compatdata := CompatdataPath(library)
	if err := os.MkdirAll(compatdata, 0o700); err != nil {
		t.Fatal(err)
	}
	state, _, err = InspectCompatdata(env, library)
	if err != nil || state != CompatdataDirectory {
		t.Fatalf("directory state = %q, err = %v", state, err)
	}

	// External link.
	if err := os.RemoveAll(compatdata); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "somewhere-else")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, compatdata); err != nil {
		t.Fatal(err)
	}
	state, _, err = InspectCompatdata(env, library)
	if err != nil || state != CompatdataExternalLink {
		t.Fatalf("external link state = %q, err = %v", state, err)
	}

	// Broken link.
	if err := os.RemoveAll(compatdata); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), compatdata); err != nil {
		t.Fatal(err)
	}
	state, _, err = InspectCompatdata(env, library)
	if err != nil || state != CompatdataBrokenLink {
		t.Fatalf("broken link state = %q, err = %v", state, err)
	}

	// Regular file.
	if err := os.RemoveAll(compatdata); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(compatdata, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, _, err = InspectCompatdata(env, library)
	if err != nil || state != CompatdataInvalid {
		t.Fatalf("invalid state = %q, err = %v", state, err)
	}
}

func TestApplyAndRollbackCompatdataMigration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the migration only mutates files on Linux")
	}
	stubSteamClosed(t)
	root := t.TempDir()
	env := pluginEnvironment(root)

	libraryPath := filepath.Join(root, "mounted", "SteamLibrary")
	if err := os.MkdirAll(filepath.Join(libraryPath, "steamapps"), 0o700); err != nil {
		t.Fatal(err)
	}
	compatdata := filepath.Join(libraryPath, "steamapps", "compatdata")
	appID := filepath.Join(compatdata, "578080", "pfx")
	if err := os.MkdirAll(appID, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appID, "tracked_files"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	library := SteamLibrary{Path: libraryPath, MountPoint: filepath.Join(root, "mounted"), Filesystem: "ntfs3"}

	plan, err := PlanCompatdataMigration(env, library)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CurrentState != CompatdataDirectory || !plan.RequiresCopy || !plan.RequiresBackup {
		t.Fatalf("plan = %#v", plan)
	}

	result, err := ApplyCompatdataMigration(env, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.TransactionID == "" {
		t.Fatalf("result = %#v", result)
	}

	// The compatdata path must be a symlink to the native target.
	info, err := os.Lstat(compatdata)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("compatdata is not a symlink: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(filepath.Join(plan.NativeTarget, "578080", "pfx", "tracked_files")); err != nil {
		t.Fatalf("native target missing copied file: %v", err)
	}

	// Rollback restores the original directory.
	rollback, err := RollbackCompatdataMigration(env, result.TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	info, err = os.Lstat(compatdata)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("compatdata not restored to directory: info=%v err=%v", info, err)
	}
	if rollback.Plan.PreservedNativeTarget == "" {
		t.Fatal("rollback did not report the automatically preserved native target")
	}
	if _, err := os.Lstat(plan.NativeTarget); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stable native target should be free after rollback: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rollback.Plan.PreservedNativeTarget, "578080", "pfx", "tracked_files")); err != nil {
		t.Fatalf("preserved native target missing copied file: %v", err)
	}
}

func stubSteamClosed(t *testing.T) {
	t.Helper()
	previous := steamRunningCheck
	steamRunningCheck = func() bool { return false }
	t.Cleanup(func() { steamRunningCheck = previous })
}

func TestAvailablePreservedNativeTargetAvoidsExistingArchives(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "native")
	first := target + nativeRollbackMarker + "transaction"
	if err := os.MkdirAll(first, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := availablePreservedNativeTarget(target, "transaction"); got != first+"-1" {
		t.Fatalf("preserved target = %q, want %q", got, first+"-1")
	}
}

func TestMoveNativeTargetUsesSameDirectoryAndRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "native")
	destination := filepath.Join(root, "native.rollback")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "prefix"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveNativeTarget(source, destination); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "prefix")); err != nil || string(data) != "data" {
		t.Fatalf("preserved data = %q, %v", data, err)
	}
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source still exists after atomic preservation: %v", err)
	}
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := moveNativeTarget(source, destination); err == nil {
		t.Fatal("expected an existing preservation path to be refused")
	}
	if err := moveNativeTarget(source, filepath.Join(root, "other", "archive")); err == nil {
		t.Fatal("expected a cross-directory preservation path to be refused")
	}
}

func TestRestorePreservedNativeTargetReplacesIncompleteRetry(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "native")
	preserved := filepath.Join(root, "native.rollback")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "partial"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(preserved, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(preserved, "original"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := CompatdataPlan{NativeTarget: target, PreservedNativeTarget: preserved}
	if err := restorePreservedNativeTarget(plan); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(target, "original")); err != nil || string(data) != "original" {
		t.Fatalf("restored native data = %q, %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(target, "partial")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete retry data was not removed: %v", err)
	}
}

func TestPlanRefusesNonLibrary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the migration only runs on Linux")
	}
	env := pluginEnvironment(t.TempDir())
	library := SteamLibrary{Path: filepath.Join(t.TempDir(), "not-a-library"), MountPoint: t.TempDir(), Filesystem: "ntfs3"}
	if _, err := PlanCompatdataMigration(env, library); err == nil {
		t.Fatal("expected non-library to be refused")
	}
}

func TestPlanStillBlocksUnrecognizedNativeData(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the migration only runs on Linux")
	}
	root := t.TempDir()
	env := pluginEnvironment(root)
	library := SteamLibrary{
		Path:       filepath.Join(root, "mounted", "SteamLibrary"),
		MountPoint: filepath.Join(root, "mounted"),
		Filesystem: "ntfs3",
	}
	if err := os.MkdirAll(filepath.Join(library.Path, "steamapps", "compatdata", "620"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := nativeTargetPath(env, library)
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "unknown-prefix"), []byte("do not touch"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanCompatdataMigration(env, library)
	if err != nil {
		t.Fatal(err)
	}
	if plan.BlockedReason != nativeTargetHasData || plan.PreservedNativeTarget != "" {
		t.Fatalf("unrecognized native data plan = %#v", plan)
	}
	data, err := os.ReadFile(filepath.Join(target, "unknown-prefix"))
	if err != nil || string(data) != "do not touch" {
		t.Fatalf("unrecognized native data changed: %q, %v", data, err)
	}
}
