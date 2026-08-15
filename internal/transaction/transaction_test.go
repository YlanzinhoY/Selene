package transaction

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRollbackRestoresFilesDirectoriesAndPatterns(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state", "selene")
	config := filepath.Join(root, "home", ".bashrc")
	dataDir := filepath.Join(root, "home", ".local", "share", "Lumen")
	absent := filepath.Join(root, "home", ".config", "new.conf")
	apps := filepath.Join(root, "home", ".local", "share", "applications")
	originalDesktop := filepath.Join(apps, "steam.desktop")

	writeTestFile(t, config, "before\n")
	writeTestFile(t, filepath.Join(dataDir, "lumen"), "old binary")
	writeTestFile(t, originalDesktop, "old desktop")

	tx, err := Begin(state, "test", []Target{
		{Path: config},
		{Path: dataDir, Recursive: true},
		{Path: absent},
	}, []Pattern{{Glob: filepath.Join(apps, "*steam*.desktop")}})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(config, []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dataDir); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dataDir, "lumen"), "new binary")
	writeTestFile(t, absent, "created")
	if err := os.WriteFile(originalDesktop, []byte("patched"), 0o600); err != nil {
		t.Fatal(err)
	}
	newDesktop := filepath.Join(apps, "my-steam-shortcut.desktop")
	writeTestFile(t, newDesktop, "created desktop")

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertTestContent(t, config, "before\n")
	assertTestContent(t, filepath.Join(dataDir, "lumen"), "old binary")
	assertTestContent(t, originalDesktop, "old desktop")
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("absent target still exists, err=%v", err)
	}
	if _, err := os.Stat(newDesktop); !os.IsNotExist(err) {
		t.Fatalf("new pattern match still exists, err=%v", err)
	}
	if tx.Journal.State != StateRolledBack {
		t.Fatalf("State = %s", tx.Journal.State)
	}
}

func TestCommitPersistsAndCanBeLoaded(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	file := filepath.Join(root, "home", "config")
	writeTestFile(t, file, "before")
	tx, err := Begin(state, "test", []Target{{Path: file}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(filepath.Join(tx.Journal.Root, "journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Journal.State != StateCommitted || loaded.Journal.ID != tx.Journal.ID {
		t.Fatalf("loaded journal = %#v", loaded.Journal)
	}
}

func TestBeginRejectsFilesystemRoot(t *testing.T) {
	_, err := Begin(filepath.Join(t.TempDir(), "state"), "bad", []Target{{Path: filepath.VolumeName(t.TempDir()) + string(filepath.Separator), Recursive: true}}, nil)
	if err == nil {
		t.Fatal("Begin() accepted filesystem root")
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertTestContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}
