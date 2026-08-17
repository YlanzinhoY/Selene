package ui

// This file is the main editing point for Selene's TUI copy. Keep interface
// wording here so contributors can revise tone without touching state logic.

const (
	textTagline = "There are truths that only the light of the moon can reveal."

	textActivityDoctor                    = "Checking the environment without changing any files..."
	textActivityDetails                   = "Loading installation details without applying changes..."
	textActivityFetch                     = "Downloading, checking SHA-256, and inspecting packages..."
	textActivityInstallPreview            = "Preparing the installation confirmation..."
	textActivityRollbackPreview           = "Looking for snapshots that can be restored..."
	textActivityRemovePreview             = "Looking for every managed LuaTools integration file..."
	textActivityDetailsRefresh            = "Refreshing installation details without applying changes..."
	textActivityFetchRefresh              = "Checking cached artifacts again..."
	textActivityInstall                   = "Installing with an active snapshot. Do not close Selene..."
	textActivityRollback                  = "Restoring the snapshot and restarting Steam. Do not close Selene..."
	textActivityRemove                    = "Removing with a safety snapshot. Do not close Selene..."
	textActivitySteamLibraries            = "Scanning mounted NTFS disks for Steam libraries..."
	textActivityCreateSteamLink           = "Creating the symbolic link with a safety snapshot..."
	textActivityRemoveSteamLink           = "Removing the symbolic link with a safety snapshot..."
	textActivitySteamGames                = "Scanning Steam libraries and linked game folders..."
	textActivityAnalyzeGame               = "Inspecting the selected game without changing its files..."
	textActivityFixPlatformAssetOverride  = "Disabling the PlatformAssetOverrides plugin with a safety snapshot..."
	textActivityUndoPlatformAssetOverride = "Restoring the PlatformAssetOverrides plugin from its safety snapshot..."

	textFooterHome                            = "↑/↓ navigate  •  enter select  •  esc back  •  q quit"
	textFooterDoctor                          = "↑/↓ scroll  •  r check again  •  esc back  •  q quit"
	textFooterDetails                         = "↑/↓ scroll  •  r refresh  •  esc back  •  q quit"
	textFooterFetch                           = "↑/↓ scroll  •  r verify cache  •  esc back  •  q quit"
	textFooterInstallConfirm                  = "↑/↓ scroll  •  i install  •  esc cancel"
	textFooterRollbackConfirm                 = "↑/↓ scroll  •  d restore  •  esc cancel"
	textFooterRemoveConfirm                   = "↑/↓ scroll  •  x remove completely  •  esc cancel"
	textFooterPlugins                         = "↑/↓ navigate  •  enter select  •  esc back  •  q quit"
	textFooterSteamLibraries                  = "↑/↓ select  •  enter add link  •  r remove link  •  esc back"
	textFooterSteamLibraryConfirm             = "↑/↓ scroll  •  l create link  •  esc cancel"
	textFooterSteamLibraryRemove              = "↑/↓ scroll  •  x remove link  •  esc cancel"
	textFooterPluginResult                    = "↑/↓ scroll  •  esc back  •  q quit"
	textFooterPlatformAssetOverride           = "↑/↓ select  •  enter inspect  •  esc back"
	textFooterPlatformAssetOverrideDetails    = "↑/↓ scroll  •  f fix  •  esc back  •  q quit"
	textFooterPlatformAssetOverrideFixConfirm = "↑/↓ scroll  •  f apply fix  •  esc cancel"
	textFooterPlatformAssetOverrideFixResult  = "↑/↓ scroll  •  u undo fix  •  esc back"
	textFooterResult                          = "↑/↓ scroll  •  esc back  •  q quit"
	textFooterTransaction                     = "Transaction in progress • wait for commit or automatic rollback"

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

	textPluginsTitle               = "Selene Plugins"
	textSteamLibraryTitle          = "Shared Steam library"
	textSteamLibraryIntro          = "Selene found existing Steam libraries on mounted NTFS disks. Add a user-only link for one library, or remove a link previously managed by Selene."
	textNoSteamLibraries           = "No Steam libraries or managed links were found."
	textSteamLibraryMountHint      = "Mount the Windows game disk first, then return here to scan it again."
	textSteamLibraryAddLabel       = "Add symbolic link"
	textSteamLibraryManagedLabel   = "Managed symbolic link"
	textSteamLibraryMetadataFormat = "%s · mounted at %s"
	textSteamLibrarySelectHint     = "Press Enter to review the selected link before Selene creates it."
	textSteamLibraryRemoveHint     = "Press r to review removal. Selene removes only the link, never the games."
	textSteamLibraryConfirmTitle   = "Confirm shared Steam library link"
	textNoSteamLibrarySelected     = "No Steam library was selected."
	textNoSteamLibraryLinkSelected = "No managed symbolic link was selected."
	textSteamLibraryConfirmIntro   = "Selene will create a symbolic link to this existing Steam library."
	textSteamLibrarySourceLabel    = "Library: "
	textSteamLibraryMountLabel     = "Mounted disk: "
	textSteamLibraryLinkLabel      = "Selene link: "
	textSteamLibrarySafety         = "The NTFS disk, its games, and Steam's configuration are not changed. A transaction records the previous path before the link is created."
	textSteamLibrarySteamHint      = "Afterward, add the Selene link in Steam Settings → Storage if Steam has not already found this library."
	textSteamLibraryConfirmAction  = "Press l to create the symbolic link."
	textSteamLibraryRemoveTitle    = "Remove shared Steam library link"
	textSteamLibraryRemoveSafety   = "Only this symbolic link will be removed. The NTFS disk, games, and Steam configuration remain unchanged."
	textSteamLibraryRemoveAction   = "Press x to remove the symbolic link."
	textSteamLibraryResultTitle    = "Shared Steam library result"
	textNoSteamLibraryResult       = "No link operation result is available."
	textSteamLibraryLinked         = "✓ Shared Steam library link created"
	textSteamLibraryAlreadyLinked  = "✓ This shared Steam library is already linked"
	textSteamLibraryRemoved        = "✓ Shared Steam library link removed"
	textSteamLibraryResultHint     = "You can return to Selene Plugins at any time to add or remove a managed link."

	textPlatformAssetOverrideTitle            = "Fix PlatformAssetOverrides"
	textPlatformAssetOverrideIntro            = "Choose a Steam-managed game to inspect for Unreal, Unity, and PlatformAssetOverrides files. This inspection does not change game files."
	textNoSteamGames                          = "No installed Steam games were found in the configured or Selene-linked libraries."
	textSteamGameMetadataFormat               = "App %s · library: %s"
	textPlatformAssetOverrideSelectHint       = "Press Enter to inspect the selected game before any repair is considered."
	textPlatformAssetOverrideAnalysisTitle    = "PlatformAssetOverrides analysis"
	textNoPlatformAssetOverrideAnalysis       = "No game analysis is available."
	textSteamGamePathLabel                    = "Game: "
	textDetectedEngineLabel                   = "Detected engine: "
	textDetectedEngineUnreal                  = "Unreal Engine"
	textDetectedEngineUnity                   = "Unity"
	textDetectedEngineUnknown                 = "not identified"
	textPlatformPluginFound                   = "! PlatformAssetOverrides plugin descriptor found"
	textPlatformPluginReferenced              = "! PlatformAssetOverrides is referenced by the game project"
	textPlatformPluginNotFound                = "No PlatformAssetOverrides descriptor or project reference was found."
	textUnityAssetsFound                      = "Unity resources.assets file(s) found:"
	textPlatformAssetOverrideSafety           = "Selene did not replace assets, disable a plugin, or add a module. A missing Unreal module needs a game-version-compatible repair; a generic asset link would be unsafe."
	textPlatformAssetOverrideVerifyHint       = "First use Steam Properties → Installed Files → Verify integrity. Selene can automate a repair only after its exact compatible source is known."
	textPlatformAssetOverrideFixAction        = "Press f to disable the PlatformAssetOverrides plugin."
	textPlatformAssetOverrideFixConfirmTitle  = "Confirm PlatformAssetOverrides fix"
	textPlatformAssetOverrideFixIntro         = "This Unreal game references the PlatformAssetOverrides plugin, which fails to load on Linux. Selene will rename the plugin descriptor so Unreal skips it."
	textPlatformAssetOverrideFixSafety        = "Only the plugin descriptor is renamed; no game assets are replaced. A safety snapshot is created first so the fix can be undone."
	textPlatformAssetOverrideFixConfirmAction = "Press f to apply the fix."
	textPlatformAssetOverrideFixResultTitle   = "PlatformAssetOverrides fix result"
	textNoPlatformAssetOverrideFix            = "No fix result is available."
	textPlatformAssetOverrideFixed            = "✓ PlatformAssetOverrides plugin disabled"
	textDisabledDescriptorLabel               = "Disabled descriptor: "
	textPlatformAssetOverrideFixResultHint    = "Press u to undo the fix and restore the original plugin descriptor."

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
	{title: "Selene Plugins", description: "Explore optional community tools that remain safe and user-scoped"},
	{title: "About Selene", description: "Learn about the project's mission and current status"},
	{title: "Exit", description: "Close the interface"},
}

func defaultMenuItems() []menuItem {
	return append([]menuItem(nil), textHomeMenu...)
}

var textPluginMenu = []menuItem{
	{title: "Shared Steam library (NTFS)", description: "Link a mounted Windows Steam library without changing its games"},
	{title: "Fix PlatformAssetOverrides", description: "Find a Steam game and inspect this Unreal or Unity compatibility error"},
}

func defaultPluginItems() []menuItem {
	return append([]menuItem(nil), textPluginMenu...)
}
