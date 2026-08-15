package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/transaction"
)

func TestLatestRestorableSkipsCompletedRollback(t *testing.T) {
	history := []transaction.Journal{
		{ID: "new", Description: "install luatools new", State: transaction.StateRolledBack, CreatedAt: time.Now()},
		{ID: "old", Description: "install luatools old", State: transaction.StateCommitted, CreatedAt: time.Now().Add(-time.Hour)},
	}
	journal := latestRestorable(history)
	if journal == nil || journal.ID != "old" {
		t.Fatalf("latestRestorable() = %#v", journal)
	}
}

func TestInstallConfirmationExplainsSafetyBoundary(t *testing.T) {
	m := model{plan: &planner.Plan{Ready: true, BundleName: "LuaTools para Linux"}}
	content := m.installConfirmContent()
	for _, expected := range []string{"SHA-256", "Sudo", "snapshot", "rollback automático", "Pressione i"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("confirmation does not mention %q: %q", expected, content)
		}
	}
}
