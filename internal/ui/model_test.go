package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/selene-linux/selene/internal/achievementsupervisor"
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

func TestHomeIncludesPluginsWithNewBadge(t *testing.T) {
	m := newModel()
	defer m.cancel()
	var found bool
	for _, item := range m.items {
		if item.title == "Plugins" {
			if item.badge != "NEW" {
				t.Fatalf("Plugins badge = %q, want NEW", item.badge)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("home menu = %#v; Plugins is missing", m.items)
	}
	content := m.homeView()
	if !strings.Contains(content, badgeStyle.Render("NEW")) {
		t.Fatalf("home menu does not render the colored NEW badge: %q", content)
	}
	if strings.Contains(content, "Selene Plugins") {
		t.Fatalf("home menu still contains the old Selene Plugins label: %q", content)
	}
}

func TestHomeStartsSupervisedAchievementSession(t *testing.T) {
	m := newModel()
	defer m.cancel()
	m.cursor = 7

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	if cmd == nil || !got.checking || !got.mutating || !got.achievementActive || got.activity != textActivityAchievementSession {
		t.Fatalf("achievement transition = checking %v, mutating %v, active %v, activity %q, cmd %v", got.checking, got.mutating, got.achievementActive, got.activity, cmd)
	}
}

func TestAchievementSessionResultShowsRecoveryAndDegradedOperation(t *testing.T) {
	m := newModel()
	defer m.cancel()
	m.checking = true
	m.mutating = true
	m.achievementActive = true

	updated, cmd := m.Update(achievementSessionMsg{result: achievementsupervisor.Result{
		SteamStarted:       true,
		BackendBecameReady: true,
		Degraded:           true,
		BackendRestarts:    2,
		LastBackendError:   "temporary failure",
	}})
	got := updated.(model)
	if cmd != nil || got.screen != screenAchievementSessionResult || got.checking || got.mutating || got.achievementActive {
		t.Fatalf("achievement result transition = screen %v, checking %v, mutating %v, active %v", got.screen, got.checking, got.mutating, got.achievementActive)
	}
	content := got.achievementSessionResultContent()
	for _, expected := range []string{"session completed", "became ready", "Backend recoveries: 2", "degraded mode", "temporary failure"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("achievement result does not mention %q: %q", expected, content)
		}
	}
}

func TestAchievementSessionLogKeepsBoundedTail(t *testing.T) {
	var buffer synchronizedBuffer
	input := strings.Repeat("a", maximumAchievementSessionLog) + "tail"
	if written, err := buffer.Write([]byte(input)); err != nil || written != len(input) {
		t.Fatalf("Write() = %d, %v", written, err)
	}
	got := buffer.String()
	if len(got) != maximumAchievementSessionLog || !strings.HasSuffix(got, "tail") {
		t.Fatalf("bounded log length = %d, suffix present = %v", len(got), strings.HasSuffix(got, "tail"))
	}
}

func TestPlatformAssetOverridesPluginIsHidden(t *testing.T) {
	m := newModel()
	defer m.cancel()
	for _, item := range m.pluginItems {
		if item.title == "Fix PlatformAssetOverrides" {
			t.Fatalf("hidden plugin is exposed in the default menu: %#v", m.pluginItems)
		}
	}
	content := m.pluginsView()
	if !strings.Contains(content, "Plugins") || strings.Contains(content, "Selene Plugins") {
		t.Fatalf("plugins screen title is incorrect: %q", content)
	}
	if strings.Contains(content, "Fix PlatformAssetOverrides") {
		t.Fatalf("hidden plugin is rendered: %q", content)
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
	for _, expected := range []string{"symlink", "Press c", "No files are deleted", "Native target"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("plan confirmation does not mention %q: %q", expected, content)
		}
	}
}

func TestCompatdataPlanExplainsAutomaticRollbackRecovery(t *testing.T) {
	m := model{
		selectedPlan: plugins.CompatdataPlan{
			Library: plugins.SteamLibrary{
				Path:       "/run/media/player/Games/SteamLibrary",
				MountPoint: "/run/media/player/Games",
				Filesystem: "ntfs3",
			},
			Compatdata:            "/run/media/player/Games/SteamLibrary/steamapps/compatdata",
			NativeTarget:          "/home/player/.local/share/selene/steam-compatdata/abc123",
			PreservedNativeTarget: "/home/player/.local/share/selene/steam-compatdata/abc123.selene-rollback-old",
			CurrentState:          plugins.CompatdataDirectory,
			RequiresCopy:          true,
			RequiresBackup:        true,
		},
	}
	content := m.compatdataPlanContent()
	for _, expected := range []string{"Preserved native copy", "earlier rollback", "atomic rename", "does not merge", "Press c"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("automatic recovery plan does not mention %q: %q", expected, content)
		}
	}
}

func TestCompatdataConfigureRequestsSteamConfirmationWhenRunning(t *testing.T) {
	m := newModel()
	defer m.cancel()
	m.screen = screenCompatdataPlan
	m.selectedPlan = configurableCompatdataPlan()
	m.steamRunning = func() bool { return true }

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	got := updated.(model)
	if cmd != nil || got.screen != screenCompatdataSteamConfirm || got.compatPending != compatdataActionConfigure {
		t.Fatalf("Steam confirmation transition = screen %v, pending %v, cmd %v", got.screen, got.compatPending, cmd)
	}
	content := got.compatdataSteamConfirmContent()
	for _, expected := range []string{"Steam is currently running", "Close Steam and continue", "will not change compatdata"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("Steam confirmation does not mention %q: %q", expected, content)
		}
	}
}

func TestCompatdataSteamConfirmationCanBeCancelled(t *testing.T) {
	m := newModel()
	defer m.cancel()
	m.screen = screenCompatdataSteamConfirm
	m.selectedPlan = configurableCompatdataPlan()
	m.compatPending = compatdataActionConfigure
	m.err = errors.New("previous close failed")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(model)
	if cmd != nil || got.screen != screenCompatdataPlan || got.compatPending != compatdataActionNone || got.err != nil {
		t.Fatalf("cancel transition = screen %v, pending %v, err %v, cmd %v", got.screen, got.compatPending, got.err, cmd)
	}
}

func TestCompatdataConfigureStartsDirectlyWhenSteamIsClosed(t *testing.T) {
	m := newModel()
	defer m.cancel()
	m.screen = screenCompatdataPlan
	m.selectedPlan = configurableCompatdataPlan()
	m.steamRunning = func() bool { return false }

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	got := updated.(model)
	if cmd == nil || !got.checking || !got.mutating || got.compatPending != compatdataActionConfigure {
		t.Fatalf("direct setup transition = checking %v, mutating %v, pending %v, cmd %v", got.checking, got.mutating, got.compatPending, cmd)
	}
}

func TestCompatdataSteamCloseFailureDoesNotStartOperation(t *testing.T) {
	m := newModel()
	defer m.cancel()
	m.screen = screenCompatdataSteamConfirm
	m.selectedPlan = configurableCompatdataPlan()
	m.compatPending = compatdataActionConfigure
	m.checking = true
	m.mutating = true

	closeErr := errors.New("Steam is still running")
	updated, cmd := m.Update(compatdataSteamClosedMsg{err: closeErr})
	got := updated.(model)
	if cmd != nil || got.screen != screenCompatdataSteamConfirm || got.checking || got.mutating || !errors.Is(got.err, closeErr) {
		t.Fatalf("failed close transition = screen %v, checking %v, mutating %v, err %v, cmd %v", got.screen, got.checking, got.mutating, got.err, cmd)
	}
}

func TestCompatdataSteamCloseSuccessContinuesPendingSetup(t *testing.T) {
	m := newModel()
	defer m.cancel()
	m.screen = screenCompatdataSteamConfirm
	m.selectedPlan = configurableCompatdataPlan()
	m.compatPending = compatdataActionConfigure
	m.checking = true
	m.mutating = true

	updated, cmd := m.Update(compatdataSteamClosedMsg{})
	got := updated.(model)
	if cmd == nil || got.screen != screenCompatdataSteamConfirm || !got.checking || !got.mutating || got.activity != textActivityConfigureCompatdata {
		t.Fatalf("successful close transition = screen %v, checking %v, mutating %v, activity %q, cmd %v", got.screen, got.checking, got.mutating, got.activity, cmd)
	}
}

func TestCompatdataRestoreRequestsSteamConfirmationWhenRunning(t *testing.T) {
	m := newModel()
	defer m.cancel()
	m.screen = screenCompatdataPlan
	m.selectedPlan = configurableCompatdataPlan()
	m.selectedPlan.CurrentState = plugins.CompatdataManagedLink
	m.selectedPlan.RollbackAvailable = true
	m.steamRunning = func() bool { return true }

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	got := updated.(model)
	if cmd != nil || got.screen != screenCompatdataSteamConfirm || got.compatPending != compatdataActionRestore {
		t.Fatalf("restore confirmation transition = screen %v, pending %v, cmd %v", got.screen, got.compatPending, cmd)
	}
	if content := got.compatdataSteamConfirmContent(); !strings.Contains(content, "restores the previous compatdata setup") {
		t.Fatalf("restore confirmation has the wrong reason: %q", content)
	}
}

func TestCompatdataPlanWarnsAboutForcedLowercaseMount(t *testing.T) {
	m := model{selectedPlan: incompatibleNTFSPlan()}
	content := m.compatdataPlanContent()
	for _, expected := range []string{
		"forces Steam filenames to lowercase",
		"scripts, textures, or assets",
		"Fix filename handling for this session",
		"Show manual steps",
		"temporary",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("NTFS warning does not mention %q: %q", expected, content)
		}
	}
}

func TestCompatdataWarnsAgainstWindowsSystemPartition(t *testing.T) {
	m := model{}
	content := m.compatdataContent()
	for _, expected := range []string{
		"intended only for dual-boot users",
		"share a game library between Windows and Linux",
		"Btrfs or Ext4",
		"does not recommend",
		"Windows system partition",
		"usually C:",
		"separate partition dedicated to games",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("compatdata screen does not mention %q: %q", expected, content)
		}
	}
}

func TestNTFSRepairRequestsSteamConfirmationWhenRunning(t *testing.T) {
	m := newModel()
	defer m.cancel()
	m.screen = screenCompatdataPlan
	m.selectedPlan = incompatibleNTFSPlan()
	m.steamRunning = func() bool { return true }

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	got := updated.(model)
	if cmd != nil || got.screen != screenCompatdataSteamConfirm || got.compatPending != compatdataActionRemountNTFS {
		t.Fatalf("repair confirmation = screen %v, pending %v, cmd %v", got.screen, got.compatPending, cmd)
	}
	content := got.compatdataSteamConfirmContent()
	for _, expected := range []string{"will not unmount a game disk", "temporarily remounts", "without force"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("repair confirmation does not mention %q: %q", expected, content)
		}
	}
}

func TestNTFSRepairStartsDirectlyWhenSteamIsClosed(t *testing.T) {
	m := newModel()
	defer m.cancel()
	m.screen = screenCompatdataPlan
	m.selectedPlan = incompatibleNTFSPlan()
	m.steamRunning = func() bool { return false }

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	got := updated.(model)
	if cmd == nil || !got.checking || !got.mutating || got.compatPending != compatdataActionRemountNTFS || got.activity != textActivityRepairNTFSMount {
		t.Fatalf("repair transition = checking %v, mutating %v, pending %v, activity %q, cmd %v", got.checking, got.mutating, got.compatPending, got.activity, cmd)
	}
}

func TestSteamCloseSuccessContinuesPendingNTFSRepair(t *testing.T) {
	m := newModel()
	defer m.cancel()
	m.screen = screenCompatdataSteamConfirm
	m.selectedPlan = incompatibleNTFSPlan()
	m.compatPending = compatdataActionRemountNTFS
	m.checking = true
	m.mutating = true

	updated, cmd := m.Update(compatdataSteamClosedMsg{})
	got := updated.(model)
	if cmd == nil || !got.checking || !got.mutating || got.activity != textActivityRepairNTFSMount {
		t.Fatalf("post-close repair = checking %v, mutating %v, activity %q, cmd %v", got.checking, got.mutating, got.activity, cmd)
	}
}

func TestNTFSManualInstructionsAreSpecificAndNonDestructive(t *testing.T) {
	m := model{selectedPlan: incompatibleNTFSPlan()}
	content := m.compatdataNTFSManualContent()
	for _, expected := range []string{
		"udisksctl unmount -b '/dev/sdb1'",
		"udisksctl mount -b '/dev/sdb1' -t ntfs",
		"uid=$(id -u)",
		"without changing /etc or using sudo",
		"temporary",
		"Never add --force",
		"Windows system partition",
		"separate partition dedicated to games",
		"sudoedit /etc/default/steamos-btrfs",
		"remove only the ignore_case item",
		"outside Selene's user-only automation",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("manual instructions do not mention %q: %q", expected, content)
		}
	}
}

func TestNTFSRepairResultExplainsSessionScopeAndLuaToolsRestart(t *testing.T) {
	m := model{compatNTFSResult: &plugins.NTFSSessionRepairResult{
		Device:     "/dev/sdb1",
		MountPoint: "/run/media/player/Games",
		After: plugins.NTFSMountAssessment{
			Compatibility: plugins.FilenameCompatibilityCompatible,
			Driver:        "mount.ntfs",
		},
	}}
	content := m.compatdataNTFSResultContent()
	for _, expected := range []string{"case-preserving", "only until", "SLSsteam/LuaTools", "Show manual steps"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("repair result does not mention %q: %q", expected, content)
		}
	}
}

func TestNTFSRepairFailureKeepsManualRecoveryAvailable(t *testing.T) {
	m := newModel()
	defer m.cancel()
	m.screen = screenCompatdataSteamConfirm
	m.compatPending = compatdataActionRemountNTFS
	m.checking = true
	m.mutating = true
	repairErr := errors.New("custom remount failed")

	updated, cmd := m.Update(compatdataNTFSRepairMsg{err: repairErr})
	got := updated.(model)
	if cmd != nil || got.screen != screenCompatdataNTFSResult || got.checking || got.mutating || !errors.Is(got.err, repairErr) {
		t.Fatalf("repair failure = screen %v, checking %v, mutating %v, err %v, cmd %v", got.screen, got.checking, got.mutating, got.err, cmd)
	}
	if content := got.compatdataNTFSResultContent(); !strings.Contains(content, "Show manual steps") || !strings.Contains(content, "restore") {
		t.Fatalf("repair failure hides recovery guidance: %q", content)
	}
}

func configurableCompatdataPlan() plugins.CompatdataPlan {
	return plugins.CompatdataPlan{
		Library: plugins.SteamLibrary{
			Path:       "/run/media/player/Games/SteamLibrary",
			MountPoint: "/run/media/player/Games",
			Source:     "/dev/sdb1",
			Filesystem: "ntfs3",
		},
		Compatdata:   "/run/media/player/Games/SteamLibrary/steamapps/compatdata",
		NativeTarget: "/home/player/.local/share/selene/steam-compatdata/abc123",
		CurrentState: plugins.CompatdataDirectory,
		RequiresCopy: true,
	}
}

func incompatibleNTFSPlan() plugins.CompatdataPlan {
	plan := configurableCompatdataPlan()
	plan.Library.Filesystem = "fuseblk"
	plan.MountAssessment = plugins.NTFSMountAssessment{
		Compatibility:   plugins.FilenameCompatibilityIncompatible,
		Driver:          "mount.lowntfs-3g",
		ForcedLowercase: true,
		CanRepair:       true,
		Reason:          "the active mount forces lowercase filenames",
	}
	return plan
}

func TestCompatdataRollbackResultShowsPreservedLocation(t *testing.T) {
	m := model{
		compatRolledBack: true,
		compatResult: &plugins.CompatdataResult{Plan: plugins.CompatdataPlan{
			Compatdata:            "/run/media/player/Games/SteamLibrary/steamapps/compatdata",
			NativeTarget:          "/home/player/.local/share/selene/steam-compatdata/abc123",
			PreservedNativeTarget: "/home/player/.local/share/selene/steam-compatdata/abc123.selene-rollback-tx",
		}},
	}
	content := m.compatdataResultContent()
	for _, expected := range []string{"Previous compatdata setup restored", "Preserved native copy", "archived automatically"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("rollback result does not mention %q: %q", expected, content)
		}
	}
}
