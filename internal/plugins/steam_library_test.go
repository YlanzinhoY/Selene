package plugins

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/selene-linux/selene/internal/planner"
)

func TestParseMountInfoFindsOnlyNTFSAndDecodesEscapes(t *testing.T) {
	mounts, err := parseMountInfo([]byte("30 1 8:1 / /run/media/player/Game\\040Disk rw,nosuid - ntfs3 /dev/sda1 rw\n31 1 8:2 / /home/player ext4 rw - ext4 /dev/sda2 rw\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 {
		t.Fatalf("mount count = %d, want 1", len(mounts))
	}
	if mounts[0].point != filepath.FromSlash("/run/media/player/Game Disk") || mounts[0].source != "/dev/sda1" {
		t.Fatalf("mount = %#v", mounts[0])
	}
}

func TestFindSteamLibraryRootsStopsAtTwoLevels(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Games", "SteamLibrary", "steamapps"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Too", "Deep", "Steam", "steamapps"), 0o700); err != nil {
		t.Fatal(err)
	}
	roots := findSteamLibraryRoots(root)
	want := filepath.Join(root, "Games", "SteamLibrary")
	if len(roots) != 1 || roots[0] != want {
		t.Fatalf("roots = %#v, want %#v", roots, []string{want})
	}
}

func TestLibraryIdentityKeyIgnoresCaseOnlyOnNTFS(t *testing.T) {
	mountPoint := filepath.Join(string(filepath.Separator), "run", "media", "player", "disk")
	upper := filepath.Join(mountPoint, "Games", "SteamLibrary")
	lower := filepath.Join(mountPoint, "games", "steamlibrary")

	ntfs := mount{point: mountPoint, filesystem: "ntfs3"}
	if gotUpper, gotLower := libraryIdentityKey(ntfs, upper), libraryIdentityKey(ntfs, lower); gotUpper != gotLower {
		t.Fatalf("NTFS identity differs by case: %q != %q", gotUpper, gotLower)
	}

	ext4 := mount{point: mountPoint, filesystem: "ext4"}
	if gotUpper, gotLower := libraryIdentityKey(ext4, upper), libraryIdentityKey(ext4, lower); gotUpper == gotLower {
		t.Fatalf("case-sensitive filesystem identities unexpectedly match: %q", gotUpper)
	}
}

func TestLibraryIDWithFilesystemIsStableAcrossNTFSPathCase(t *testing.T) {
	mountPoint := filepath.Join(string(filepath.Separator), "run", "media", "player", "disk")
	upper := SteamLibrary{Path: filepath.Join(mountPoint, "Games", "SteamLibrary"), Filesystem: "ntfs3"}
	lower := SteamLibrary{Path: filepath.Join(mountPoint, "games", "steamlibrary"), Filesystem: "fuseblk"}

	upperID := libraryIDWithFilesystem(upper, mountPoint, "volume-uuid")
	lowerID := libraryIDWithFilesystem(lower, mountPoint, "volume-uuid")
	if upperID != lowerID {
		t.Fatalf("stable NTFS ids differ: %q != %q", upperID, lowerID)
	}
	if oldUpper, oldLower := libraryIDWithFilesystemCase(upper, mountPoint, "volume-uuid", true), libraryIDWithFilesystemCase(lower, mountPoint, "volume-uuid", true); oldUpper == oldLower {
		t.Fatalf("compatibility ids should preserve the historical case distinction: %q", oldUpper)
	}
}

func TestSameSteamLibraryMatchesNTFSCaseAliases(t *testing.T) {
	left := SteamLibrary{
		Path:       "/run/media/player/disk/SteamLibrary",
		MountPoint: "/run/media/player/disk",
		Filesystem: "ntfs3",
	}
	right := SteamLibrary{
		Path:       "/run/media/player/disk/steamlibrary",
		MountPoint: "/run/media/player/disk",
		Filesystem: "fuseblk",
	}
	if !sameSteamLibrary(left, right) {
		t.Fatal("NTFS case aliases were not recognized as the same library")
	}
	right.MountPoint = "/run/media/player/other-disk"
	if sameSteamLibrary(left, right) {
		t.Fatal("paths on different mount points were treated as one library")
	}
}

func pluginEnvironment(root string) planner.Environment {
	return planner.Environment{
		OS:           runtime.GOOS,
		Home:         filepath.Join(root, "home"),
		XDGDataHome:  filepath.Join(root, "data"),
		XDGStateHome: filepath.Join(root, "state"),
	}
}
