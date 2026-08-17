package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/selene-linux/selene/internal/installer"
	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/plugins"
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
	m := model{plan: &planner.Plan{Ready: true, BundleName: "LuaTools for Linux"}}
	content := m.installConfirmContent()
	for _, expected := range []string{"SHA-256", "Sudo", "snapshot", "automatic rollback", "Press i"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("confirmation does not mention %q: %q", expected, content)
		}
	}
}

func TestHomeUsesSemanticInstallationDetailsLabel(t *testing.T) {
	m := newModel()
	defer m.cancel()
	if got := m.items[1].title; got != "Installation details" {
		t.Fatalf("installation details item = %q", got)
	}
}

func TestUninstallConfirmationExplainsCompleteScope(t *testing.T) {
	m := model{removal: &installer.UninstallPreview{
		Detected: true,
		Traces:   []string{"/home/player/.local/share/Lumen"},
	}}
	content := m.uninstallConfirmContent()
	for _, expected := range []string{"LuaTools", "Lumen", "slsteam-moon", "games", "snapshot", "Press x"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("uninstall confirmation does not mention %q: %q", expected, content)
		}
	}
}

func TestHomeMenuHasBreathingRoomBetweenItems(t *testing.T) {
	m := newModel()
	defer m.cancel()
	content := m.homeView()
	if !strings.Contains(content, "\n\n") {
		t.Fatalf("home menu has no blank line between items: %q", content)
	}
	if !strings.Contains(content, "Check Linux, Steam, libraries, and Proton") {
		t.Fatalf("home menu does not show the selected description: %q", content)
	}
}

func TestAboutCreditsCreator(t *testing.T) {
	content := aboutView()
	if !strings.Contains(content, "Created by YlanzinhoY") {
		t.Fatalf("about screen does not credit the creator: %q", content)
	}
}

func TestHomeIncludesSelenePlugins(t *testing.T) {
	m := newModel()
	defer m.cancel()
	var found bool
	for _, item := range m.items {
		if item.title == "Selene Plugins" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("home menu = %#v; Selene Plugins is missing", m.items)
	}
}

func TestCompatdataPlanConfirmationExplainsSafetyBoundary(t *testing.T) {
	m := model{
		selectedPlan: plugins.CompatdataPlan{
			Library: plugins.SteamLibrary{
				Path:       "/run/media/player/Games/SteamLibrary",
				MountPoint: "/run/media/player/Games",
				Filesystem: "ntfs3",
			},
			Compatdata:     "/run/media/player/Games/SteamLibrary/steamapps/compatdata",
			NativeTarget:   "/home/player/.local/share/selene/steam-compatdata/abc123",
			CurrentState:   plugins.CompatdataDirectory,
			RequiresCopy:   true,
			RequiresBackup: true,
		},
	}
	content := m.compatdataPlanContent()
	for _, expected := range []string{"symlink", "Press m", "No files are deleted", "Native target"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("plan confirmation does not mention %q: %q", expected, content)
		}
	}
}
