package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/selene-linux/selene/internal/planner"
)

func TestUserTransactionScopeContainsCriticalPaths(t *testing.T) {
	root := t.TempDir()
	env := testEnvironment(root)
	targets, patterns, err := userTransactionScope(env)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, target := range targets {
		paths = append(paths, target.Path)
	}
	for _, required := range []string{
		filepath.Join(env.Home, ".local", "share", "SLSsteam"),
		filepath.Join(env.Home, ".local", "share", "Lumen"),
		filepath.Join(env.Home, ".bashrc"),
		filepath.Join(env.XDGConfigHome, "systemd", "user", "timers.target.wants", "slsteam-desktop-guardian.timer"),
	} {
		if !contains(paths, required) {
			t.Fatalf("scope does not contain %s: %#v", required, paths)
		}
	}
	if len(patterns) < 10 {
		t.Fatalf("patterns = %d, want desktop and systemd coverage", len(patterns))
	}
}

func TestResolveDesktopDirFromXDGConfig(t *testing.T) {
	root := t.TempDir()
	env := testEnvironment(root)
	if err := os.MkdirAll(env.XDGConfigHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.XDGConfigHome, "user-dirs.dirs"), []byte("XDG_DESKTOP_DIR=\"$HOME/Área de Trabalho\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(env.Home, "Área de Trabalho")
	if got := resolveDesktopDir(env); got != want {
		t.Fatalf("resolveDesktopDir() = %q, want %q", got, want)
	}
}

func TestValidateUserScopeRejectsSymlinkedShellRC(t *testing.T) {
	root := t.TempDir()
	env := testEnvironment(root)
	if err := os.MkdirAll(env.Home, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "outside")
	if err := os.WriteFile(target, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(env.Home, ".bashrc")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := validateUserScope(env); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("validateUserScope() error = %v", err)
	}
}

func testEnvironment(root string) planner.Environment {
	home := filepath.Join(root, "home", "player")
	return planner.Environment{
		OS: "linux", Arch: "amd64", Home: home,
		XDGDataHome:   filepath.Join(home, ".local", "share"),
		XDGCacheHome:  filepath.Join(home, ".cache"),
		XDGConfigHome: filepath.Join(home, ".config"),
		XDGStateHome:  filepath.Join(home, ".local", "state"),
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
