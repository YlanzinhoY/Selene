package installer

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/selene-linux/selene/internal/catalog"
	"github.com/selene-linux/selene/internal/planner"
)

type uninstallRunnerFunc func(context.Context, ScriptCommand) error

func (function uninstallRunnerFunc) Run(ctx context.Context, command ScriptCommand) error {
	return function(ctx, command)
}

func TestRemoveUserStackCleansManagedIntegration(t *testing.T) {
	root := t.TempDir()
	env := testEnvironment(root)
	writeInstallerFile(t, filepath.Join(stackDataHome(env), "SLSsteam", "SLSsteam.so"), "binary")
	writeInstallerFile(t, filepath.Join(stackDataHome(env), "Lumen", "luatools", "plugin.json"), "{}")
	writeInstallerFile(t, filepath.Join(env.XDGConfigHome, "SLSsteam", "config.yaml"), "config")
	writeInstallerFile(t, filepath.Join(env.XDGStateHome, "slsteam-moon", "coverage.policy"), "desktop")
	writeInstallerFile(t, filepath.Join(env.Home, ".bashrc"), "keep=this\n# SLSsteam: Add wrapper to PATH\nexport PATH=\"$HOME/.local/share/SLSsteam/path:$PATH\"\n")

	applications := filepath.Join(env.XDGDataHome, "applications")
	desktop := filepath.Join(applications, "steam.desktop")
	writeInstallerFile(t, desktop, "[Desktop Entry]\nExec=/usr/bin/steam\n")
	writeInstallerFile(t, desktop+".slssteam-backup", "old")

	dropin := filepath.Join(env.XDGConfigHome, "systemd", "user", "app-steam@autostart.service.d")
	writeInstallerFile(t, filepath.Join(dropin, "slsteam-guardian.conf"), "managed")
	writeInstallerFile(t, filepath.Join(dropin, "foreign.conf"), "keep")
	writeInstallerFile(t, filepath.Join(env.XDGConfigHome, "systemd", "user", "slsteam-desktop-guardian.service"), "unit")

	preview, err := PreviewUninstall(env)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Detected || len(preview.Traces) == 0 {
		t.Fatalf("PreviewUninstall() = %#v", preview)
	}
	if err := removeUserStack(env); err != nil {
		t.Fatal(err)
	}
	if err := verifyUserStackRemoved(env); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{
		filepath.Join(stackDataHome(env), "SLSsteam"),
		filepath.Join(stackDataHome(env), "Lumen"),
		filepath.Join(env.XDGConfigHome, "SLSsteam"),
		desktop + ".slssteam-backup",
		filepath.Join(dropin, "slsteam-guardian.conf"),
	} {
		if _, err := os.Lstat(removed); !os.IsNotExist(err) {
			t.Fatalf("managed path survived removal: %s (err=%v)", removed, err)
		}
	}
	assertInstallerContent(t, desktop, "[Desktop Entry]\nExec=/usr/bin/steam\n")
	assertInstallerContent(t, filepath.Join(dropin, "foreign.conf"), "keep")
	rc, err := os.ReadFile(filepath.Join(env.Home, ".bashrc"))
	if err != nil {
		t.Fatal(err)
	}
	if string(rc) != "keep=this\n" {
		t.Fatalf("shell rc = %q, want unrelated content only", rc)
	}
}

func TestVerifyUserStackRemovedRejectsPatchedDesktop(t *testing.T) {
	root := t.TempDir()
	env := testEnvironment(root)
	desktop := filepath.Join(env.XDGDataHome, "applications", "steam.desktop")
	writeInstallerFile(t, desktop, "[Desktop Entry]\nExec=/home/player/.local/share/SLSsteam/path/steam\n"+slsteamDesktopTag+"\n")

	err := verifyUserStackRemoved(env)
	if err == nil || !strings.Contains(err.Error(), desktop) {
		t.Fatalf("verifyUserStackRemoved() error = %v", err)
	}
}

func TestPreviewUninstallReportsCleanEnvironment(t *testing.T) {
	preview, err := PreviewUninstall(testEnvironment(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if preview.Detected || len(preview.Traces) != 0 {
		t.Fatalf("PreviewUninstall() = %#v", preview)
	}
}

func TestUninstallCommitsCompleteRemoval(t *testing.T) {
	requireLinuxUserTest(t)
	env := testEnvironment(t.TempDir())
	writeInstallerFile(t, filepath.Join(stackDataHome(env), "SLSsteam", "SLSsteam.so"), "installed")
	writeInstallerFile(t, filepath.Join(stackDataHome(env), "Lumen", "lumen"), "installed")
	source := cachedUninstallCatalog(t, env)
	called := false
	runner := uninstallRunnerFunc(func(_ context.Context, command ScriptCommand) error {
		called = true
		if len(command.Arguments) != 1 || command.Arguments[0] != "uninstall" {
			t.Fatalf("uninstall arguments = %#v", command.Arguments)
		}
		if !containsEnvironment(command.Env, "SLSM_SUDO_DENIED=1") {
			t.Fatalf("controlled environment missing sudo denial: %#v", command.Env)
		}
		return nil
	})

	result, err := Uninstall(context.Background(), source, env, Options{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if !called || !result.Removed || result.TransactionID == "" {
		t.Fatalf("Uninstall() = %#v, called=%t", result, called)
	}
	if _, err := os.Lstat(filepath.Join(stackDataHome(env), "SLSsteam")); !os.IsNotExist(err) {
		t.Fatalf("SLSsteam survived complete removal: %v", err)
	}
	history, err := History(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].State != "committed" || !strings.HasPrefix(history[0].Description, "uninstall ") {
		t.Fatalf("uninstall journal = %#v", history)
	}
}

func TestUninstallFailureRestoresInstalledStack(t *testing.T) {
	requireLinuxUserTest(t)
	env := testEnvironment(t.TempDir())
	installed := filepath.Join(stackDataHome(env), "SLSsteam", "SLSsteam.so")
	writeInstallerFile(t, installed, "installed")
	source := cachedUninstallCatalog(t, env)
	runner := uninstallRunnerFunc(func(_ context.Context, _ ScriptCommand) error {
		if err := os.RemoveAll(filepath.Dir(installed)); err != nil {
			t.Fatal(err)
		}
		return errors.New("simulated upstream failure")
	})

	_, err := Uninstall(context.Background(), source, env, Options{Runner: runner})
	if err == nil || !strings.Contains(err.Error(), "installed state restored") {
		t.Fatalf("Uninstall() error = %v", err)
	}
	assertInstallerContent(t, installed, "installed")
}

func cachedUninstallCatalog(t *testing.T, env planner.Environment) catalog.Catalog {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("setup.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("#!/bin/bash\nexit 0\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive.Bytes())
	hash := hex.EncodeToString(digest[:])
	component := catalog.Component{
		ID: "slsteam-moon", Name: "slsteam-moon", Version: "test",
		Artifact: catalog.Artifact{
			Name: "slsteam-test.zip", URL: "https://example.invalid/slsteam-test.zip",
			Size: int64(archive.Len()), SHA256: hash, Format: "zip",
		},
		Install: catalog.InstallSpec{
			Strategy: "verified-script", Destination: "${HOME}/.local/share/SLSsteam",
			Entrypoint: "setup.sh", Arguments: []string{"install"}, Validate: []string{"setup.sh"},
		},
	}
	cachePath := filepath.Join(env.XDGCacheHome, "selene", "downloads", hash+"-"+component.Artifact.Name)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return catalog.Catalog{Revision: "test", Components: []catalog.Component{component}}
}

func requireLinuxUserTest(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux")
	}
	if effectiveUID() == 0 {
		t.Skip("requires a non-root Linux user")
	}
}

func containsEnvironment(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
