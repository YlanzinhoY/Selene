package ui

// This file is the main editing point for Selene's TUI copy. Keep interface
// wording here so contributors can revise tone without touching state logic.

const (
	textTagline = "There are truths that only the light of the moon can reveal."

	textActivityDoctor          = "Checking the environment without changing any files..."
	textActivityDetails         = "Loading installation details without applying changes..."
	textActivityFetch           = "Downloading, checking SHA-256, and inspecting packages..."
	textActivityInstallPreview  = "Preparing the installation confirmation..."
	textActivityRollbackPreview = "Looking for snapshots that can be restored..."
	textActivityRemovePreview   = "Looking for every managed LuaTools integration file..."
	textActivityDetailsRefresh  = "Refreshing installation details without applying changes..."
	textActivityFetchRefresh    = "Checking cached artifacts again..."
	textActivityInstall         = "Installing with an active snapshot. Do not close Selene..."
	textActivityRollback        = "Restoring the snapshot and restarting Steam. Do not close Selene..."
	textActivityRemove          = "Removing with a safety snapshot. Do not close Selene..."

	textFooterHome            = "↑/↓ navigate  •  enter select  •  esc back  •  q quit"
	textFooterDoctor          = "↑/↓ scroll  •  r check again  •  esc back  •  q quit"
	textFooterDetails         = "↑/↓ scroll  •  r refresh  •  esc back  •  q quit"
	textFooterFetch           = "↑/↓ scroll  •  r verify cache  •  esc back  •  q quit"
	textFooterInstallConfirm  = "↑/↓ scroll  •  i install  •  esc cancel"
	textFooterRollbackConfirm = "↑/↓ scroll  •  d restore  •  esc cancel"
	textFooterRemoveConfirm   = "↑/↓ scroll  •  x remove completely  •  esc cancel"
	textFooterResult          = "↑/↓ scroll  •  esc back  •  q quit"
	textFooterTransaction     = "Transaction in progress • wait for commit or automatic rollback"

	textNoDiagnostics       = "No diagnostic has been run yet."
	textDoctorTitle         = "Environment check"
	textDoctorSummaryFormat = "%d passed • %d warning(s) • %d error(s) • r check again"

	textDetailsTitle       = "Installation details"
	textNoDetails          = "No installation details are available."
	textCatalogLabel       = "Catalog "
	textDetailsReady       = "✓ This environment is ready to install"
	textDetailsBlocked     = "! This installation still has blockers"
	textProposedOperations = "Proposed operations:"
	textNoChangesRefresh   = "No changes were applied. Press r to refresh."

	textArtifactsTitle         = "LuaTools artifacts"
	textVerifiedArtifactsTitle = "Verified artifacts"
	textNoArtifacts            = "No verified artifacts."
	textDownloadedNow          = "downloaded now"
	textAlreadyCached          = "already in cache"
	textCacheLabel             = "cache: "
	textCacheOnly              = "These files are only cached; nothing was installed."

	textInstallConfirmTitle = "Confirm installation"
	textInstallBlocked      = "× This environment cannot run the installation yet."
	textBeforeContinue      = "Before you continue"
	textInstallBulletSteam  = "• Steam and Lumen will be stopped by the verified installer."
	textInstallBulletSource = "• setup.sh comes from the pinned ZIP and is checked with SHA-256."
	textInstallBulletScope  = "• Sudo and /usr changes are blocked; everything stays in your user account."
	textInstallBulletBackup = "• A persistent snapshot is created before the first change."
	textInstallBulletFail   = "• Any failure triggers automatic rollback and restarts Steam."
	textInstallAction       = "Press i to install."
	textEscapeNoChanges     = "Esc cancels without changing anything."

	textInstallResultTitle = "Installation result"
	textInstalled          = "✓ LuaTools installed"
	textTransactionLabel   = "Transaction: "
	textSnapshotRetained   = "The snapshot was kept; use Undo last installation if needed."
	textOperationLog       = "Operation log:"

	textRollbackTitle         = "Undo last installation"
	textNoRollback            = "No restorable snapshot was found."
	textRollbackExplanation   = "Selene will stop Steam and the guardian, restore every tracked file, then start Steam again."
	textRollbackAction        = "Press d to restore."
	textRecoveryTitle         = "Recover interrupted removal"
	textRecoveryExplanation   = "The removal did not finish. Selene will restore the installed stack from its safety snapshot and restart Steam."
	textRecoveryAction        = "Press d to recover."
	textSnapshotLabel         = "Snapshot: "
	textCreatedLabel          = "Created: "
	textOperationLabel        = "Operation: "
	textEscapeKeepsInstall    = "Esc keeps the current installation."
	textRollbackResultTitle   = "Rollback result"
	textPreviousStateRestored = "✓ Previous state restored and Steam restarted"

	textRemoveTitle            = "Remove LuaTools completely"
	textNoManagedTraces        = "No managed LuaTools, Lumen, or slsteam-moon files were found."
	textRemoveIsDifferent      = "This is different from undoing the last installation."
	textRemoveScope            = "It removes LuaTools, Lumen, slsteam-moon, settings, services, and Steam integration from your user account. Internal plugin data is deleted too."
	textRemoveKeeps            = "It does not remove your games, Steam, Selene, the download cache, or safety snapshots."
	textDetectedTraces         = "Detected files:"
	textRemoveSafety           = "Selene creates another snapshot first and restores the installed state if anything fails."
	textRemoveAction           = "Press x to remove completely."
	textRemoveResultTitle      = "Complete removal result"
	textRemoved                = "✓ LuaTools integration removed"
	textRemovedExplanation     = "LuaTools, Lumen, and slsteam-moon are no longer integrated with Steam."
	textSafetyTransactionLabel = "Safety transaction: "
	textNothingRemoved         = "No managed installation was found; nothing changed."

	textAboutTitle  = "About Selene"
	textAboutBody   = "Selene is an independent community manager that makes the\nLuaTools ecosystem accessible on Linux. Its priority is to install,\ncheck, update, undo, and remove changes safely."
	textAboutState  = "Current state: transactional user-only install, rollback, and complete removal."
	textAboutAuthor = "Created by YlanzinhoY."
)

var textHomeMenu = []menuItem{
	{title: "Check compatibility", description: "Check Linux, Steam, libraries, and Proton"},
	{title: "Installation details", description: "Review downloads and changes before installing"},
	{title: "Download and verify", description: "Cache verified packages without installing them"},
	{title: "Install LuaTools", description: "Use verified sources with an automatic snapshot and rollback"},
	{title: "Undo last installation", description: "Restore the exact previous state and restart Steam"},
	{title: "Remove LuaTools completely", description: "Remove LuaTools, Lumen, and slsteam-moon from your user account"},
	{title: "About Selene", description: "Learn about the project's mission and current status"},
	{title: "Exit", description: "Close the interface"},
}

func defaultMenuItems() []menuItem {
	return append([]menuItem(nil), textHomeMenu...)
}
