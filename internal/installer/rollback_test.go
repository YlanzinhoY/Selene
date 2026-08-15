package installer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/selene-linux/selene/internal/transaction"
)

func TestSelectTransactionUsesNewestRestorableInstall(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	old, err := transaction.Begin(state, "install luatools old", []transaction.Target{{Path: filepath.Join(root, "old")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Commit(); err != nil {
		t.Fatal(err)
	}
	latest, err := transaction.Begin(state, "install luatools latest", []transaction.Target{{Path: filepath.Join(root, "latest")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := latest.Commit(); err != nil {
		t.Fatal(err)
	}
	selected, err := selectTransaction(state, "")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Journal.ID != latest.Journal.ID {
		t.Fatalf("selected %s, want %s", selected.Journal.ID, latest.Journal.ID)
	}
	if err := latest.Rollback(); err != nil {
		t.Fatal(err)
	}
	selected, err = selectTransaction(state, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Journal.ID != old.Journal.ID {
		t.Fatalf("selected %s after rollback, want %s", selected.Journal.ID, old.Journal.ID)
	}
}

func TestSuccessfulUninstallBlocksOlderAutomaticRollback(t *testing.T) {
	stateRoot := t.TempDir()
	install, err := transaction.Begin(stateRoot, "install luatools old", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := install.Commit(); err != nil {
		t.Fatal(err)
	}
	uninstall, err := transaction.Begin(stateRoot, "uninstall luatools current", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := uninstall.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := selectTransaction(stateRoot, ""); err == nil || !strings.Contains(err.Error(), "successful complete removal") {
		t.Fatalf("selectTransaction() error = %v", err)
	}
	if _, err := selectTransaction(stateRoot, install.Journal.ID); err == nil || !strings.Contains(err.Error(), "predates") {
		t.Fatalf("explicit selectTransaction() crossed removal boundary: %v", err)
	}
}

func TestUnfinishedUninstallIsRecoverable(t *testing.T) {
	stateRoot := t.TempDir()
	uninstall, err := transaction.Begin(stateRoot, "uninstall luatools interrupted", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := selectTransaction(stateRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Journal.ID != uninstall.Journal.ID {
		t.Fatalf("selected %s, want interrupted uninstall %s", selected.Journal.ID, uninstall.Journal.ID)
	}
}
