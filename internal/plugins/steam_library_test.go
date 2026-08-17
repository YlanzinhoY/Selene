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

func pluginEnvironment(root string) planner.Environment {
	return planner.Environment{
		OS:           runtime.GOOS,
		Home:         filepath.Join(root, "home"),
		XDGDataHome:  filepath.Join(root, "data"),
		XDGStateHome: filepath.Join(root, "state"),
	}
}
