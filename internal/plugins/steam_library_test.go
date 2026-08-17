package plugins

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestManagedLinksListsBrokenLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symbolic-link permissions vary by test environment")
	}
	env := pluginEnvironment(t.TempDir())
	directory := linksDirectory(env)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "broken")
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), path); err != nil {
		t.Fatal(err)
	}
	links, err := ManagedLinks(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Path != path || !strings.Contains(links[0].Target, "missing") {
		t.Fatalf("links = %#v", links)
	}
}

func TestCreateAndRemoveSteamLibraryLinkAreTransactional(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the plugin only mutates files on Linux")
	}
	root := t.TempDir()
	env := pluginEnvironment(root)
	libraryPath := filepath.Join(root, "mounted", "SteamLibrary")
	if err := os.MkdirAll(filepath.Join(libraryPath, "steamapps"), 0o700); err != nil {
		t.Fatal(err)
	}
	library := SteamLibrary{Path: libraryPath, MountPoint: filepath.Join(root, "mounted"), Filesystem: "ntfs3"}
	created, err := CreateSteamLibraryLink(env, library)
	if err != nil {
		t.Fatal(err)
	}
	if created.TransactionID == "" || created.JournalPath == "" {
		t.Fatalf("creation result = %#v", created)
	}
	target, err := os.Readlink(created.Link.Path)
	if err != nil || target != libraryPath {
		t.Fatalf("link target = %q, err = %v", target, err)
	}
	removed, err := RemoveSteamLibraryLink(env, created.Link)
	if err != nil {
		t.Fatal(err)
	}
	if removed.TransactionID == "" {
		t.Fatalf("removal result = %#v", removed)
	}
	if _, err := os.Lstat(created.Link.Path); !os.IsNotExist(err) {
		t.Fatalf("link survived removal, err = %v", err)
	}
}

func TestRemoveSteamLibraryLinkRefusesPathsOutsidePluginDirectory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the plugin only mutates files on Linux")
	}
	env := pluginEnvironment(t.TempDir())
	_, err := RemoveSteamLibraryLink(env, Link{Path: filepath.Join(env.Home, "not-managed")})
	if err == nil || !strings.Contains(err.Error(), "outside Selene") {
		t.Fatalf("RemoveSteamLibraryLink() error = %v", err)
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
