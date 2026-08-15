package artifact

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/selene-linux/selene/internal/catalog"
)

func TestExtractZIPUsesSafePermissions(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "package.zip")
	writeZIP(t, archive, map[string]string{
		"release/bin/tool":    "binary",
		"release/config.json": "{}",
		"release/nested/data": "value",
	})
	component := catalog.Component{
		Artifact: catalog.Artifact{Format: "zip"},
		Install: catalog.InstallSpec{
			StripComponents: 1,
			Executables:     []string{"bin/tool"},
			Validate:        []string{"config.json"},
		},
	}
	destination := filepath.Join(root, "stage")
	if err := Extract(archive, component, destination); err != nil {
		t.Fatal(err)
	}
	assertMode(t, filepath.Join(destination, "bin", "tool"), 0o755)
	assertMode(t, filepath.Join(destination, "config.json"), 0o644)
	data, err := os.ReadFile(filepath.Join(destination, "nested", "data"))
	if err != nil || string(data) != "value" {
		t.Fatalf("staged data = %q, err=%v", data, err)
	}
}

func TestExtractRefusesExistingDestination(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "package.zip")
	writeZIP(t, archive, map[string]string{"file": "value"})
	destination := filepath.Join(root, "stage")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	component := catalog.Component{
		Artifact: catalog.Artifact{Format: "zip"},
		Install:  catalog.InstallSpec{Validate: []string{"file"}},
	}
	if err := Extract(archive, component, destination); err == nil {
		t.Fatal("Extract() accepted an existing destination")
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != want {
		t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
	}
}
