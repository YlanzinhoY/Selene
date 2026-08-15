package installer

import (
	"path/filepath"
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
