package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/transaction"
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
		filepath.Join(env.XDGDataHome, "applications", "mimeinfo.cache"),
	} {
		if !contains(paths, required) {
			t.Fatalf("scope does not contain %s: %#v", required, paths)
		}
	}
	if len(patterns) < 10 {
		t.Fatalf("patterns = %d, want desktop and systemd coverage", len(patterns))
	}
}

func TestCleanInstallRollbackRemovesEntireCreatedStack(t *testing.T) {
	root := t.TempDir()
	env := testEnvironment(root)
	targets, patterns, err := userTransactionScope(env)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := transaction.Begin(filepath.Join(env.XDGStateHome, "selene"), "install luatools test", targets, patterns)
	if err != nil {
		t.Fatal(err)
	}
	slsteam := filepath.Join(stackDataHome(env), "SLSsteam", "SLSsteam.so")
	lumen := filepath.Join(stackDataHome(env), "Lumen", "lumen")
	writeInstallerFile(t, slsteam, "created")
	writeInstallerFile(t, lumen, "created")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Dir(slsteam), filepath.Dir(lumen)} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("rollback left a stack directory created by install: %s (err=%v)", path, err)
		}
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
