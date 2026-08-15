package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/selene-linux/selene/internal/installer"
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

func TestLatestRestorableStopsAfterCompleteRemoval(t *testing.T) {
	history := []transaction.Journal{
		{ID: "removal", Description: "uninstall luatools current", State: transaction.StateCommitted, CreatedAt: time.Now()},
		{ID: "install", Description: "install luatools old", State: transaction.StateCommitted, CreatedAt: time.Now().Add(-time.Hour)},
	}
	if journal := latestRestorable(history); journal != nil {
		t.Fatalf("latestRestorable() crossed successful uninstall boundary: %#v", journal)
	}
}

func TestLatestRestorableRecoversInterruptedRemoval(t *testing.T) {
	history := []transaction.Journal{
		{ID: "removal", Description: "uninstall luatools interrupted", State: transaction.StateActive, CreatedAt: time.Now()},
	}
	journal := latestRestorable(history)
	if journal == nil || journal.ID != "removal" {
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

func TestHomeUsesSemanticInstallationDetailsLabel(t *testing.T) {
	m := newModel()
	defer m.cancel()
	if got := m.items[1].title; got != "Detalhes da instalação" {
		t.Fatalf("installation details item = %q", got)
	}
}

func TestUninstallConfirmationExplainsCompleteScope(t *testing.T) {
	m := model{removal: &installer.UninstallPreview{
		Detected: true,
		Traces:   []string{"/home/player/.local/share/Lumen"},
	}}
	content := m.uninstallConfirmContent()
	for _, expected := range []string{"LuaTools", "Lumen", "slsteam-moon", "jogos", "snapshot", "Pressione x"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("uninstall confirmation does not mention %q: %q", expected, content)
		}
	}
}
