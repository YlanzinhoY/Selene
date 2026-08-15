package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/selene-linux/selene/internal/catalog"
)

func TestPrepareLumenCandidatePreservesPluginData(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "home", ".local", "share")
	current := filepath.Join(dataHome, "Lumen")
	lumenStage := filepath.Join(root, "stage", "lumen")
	pluginStage := filepath.Join(root, "stage", "plugin")
	writeInstallerFile(t, filepath.Join(current, "old-runtime"), "remove me")
	writeInstallerFile(t, filepath.Join(current, "luatools", "old-plugin"), "remove me")
	writeInstallerFile(t, filepath.Join(current, "luatools", "backend", "data", "user.json"), "preserved")
	writeInstallerFile(t, filepath.Join(current, "luatools", "backend", "api.json"), "legacy catalog")
	writeInstallerFile(t, filepath.Join(lumenStage, "lumen"), "new runtime")
	writeInstallerFile(t, filepath.Join(lumenStage, "lua", "boot.lua"), "boot")
	writeInstallerFile(t, filepath.Join(pluginStage, "plugin.json"), "{}")
	writeInstallerFile(t, filepath.Join(pluginStage, "backend", "main.lua"), "main")

	source, err := catalog.LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := prepareLumenCandidate(dataHome, filepath.Join(root, "transaction"), current, lumenStage, pluginStage, source)
	if err != nil {
		t.Fatal(err)
	}
	assertInstallerContent(t, filepath.Join(candidate, "lumen"), "new runtime")
	assertInstallerContent(t, filepath.Join(candidate, "luatools", "backend", "data", "user.json"), "preserved")
	assertInstallerContent(t, filepath.Join(candidate, "luatools", "backend", "data", "api.json"), "legacy catalog")
	if _, err := os.Stat(filepath.Join(candidate, "luatools", "old-plugin")); !os.IsNotExist(err) {
		t.Fatalf("old plugin file survived replacement, err=%v", err)
	}
	data, err := os.ReadFile(filepath.Join(candidate, "versions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var versions map[string]any
	if err := json.Unmarshal(data, &versions); err != nil {
		t.Fatalf("versions.json is invalid: %v", err)
	}
}

func TestControlledEnvironmentForcesUserOnlyMode(t *testing.T) {
	root := t.TempDir()
	env := testEnvironment(root)
	wrapper := filepath.Join(env.Home, ".local", "share", "SLSsteam", "path")
	t.Setenv("PATH", strings.Join([]string{wrapper, filepath.Join(root, "bin")}, string(os.PathListSeparator)))
	t.Setenv("LD_AUDIT", "unsafe.so")
	t.Setenv("SLSM_IMMUTABLE", "0")
	values := controlledEnvironment(env)
	got := make(map[string]string)
	for _, value := range values {
		key, item, ok := strings.Cut(value, "=")
		if ok {
			got[key] = item
		}
	}
	if got["SLSM_IMMUTABLE"] != "1" || got["SLSM_SUDO_DENIED"] != "1" {
		t.Fatalf("controlled flags = %#v", got)
	}
	if _, exists := got["LD_AUDIT"]; exists {
		t.Fatal("LD_AUDIT leaked into the verified installer")
	}
	for _, part := range filepath.SplitList(got["PATH"]) {
		if filepath.Clean(part) == filepath.Clean(wrapper) {
			t.Fatal("previous SLSsteam wrapper leaked into PATH")
		}
	}
}

func writeInstallerFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertInstallerContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}
