package plugins

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/transaction"
)

// GameEngine describes the files Selene could identify in a Steam game.
type GameEngine string

const (
	GameEngineUnknown GameEngine = "unknown"
	GameEngineUnity   GameEngine = "unity"
	GameEngineUnreal  GameEngine = "unreal"
)

// SteamGame is one installed Steam title declared by an appmanifest file.
type SteamGame struct {
	AppID       string
	Name        string
	LibraryPath string
	InstallPath string
}

// AssetOverrideAnalysis is a read-only inspection of the selected game. It
// deliberately does not infer a replacement asset or change game files.
type AssetOverrideAnalysis struct {
	Game                     SteamGame
	Engine                   GameEngine
	UnityAssets              []string
	UnrealProjectFiles       []string
	PlatformPluginDescriptor string
	PlatformPluginReferenced bool
}

var (
	vdfValuePattern    = regexp.MustCompile(`(?m)"(appid|name|installdir)"\s+"((?:\\.|[^"])*)"`)
	libraryPathPattern = regexp.MustCompile(`(?m)"path"\s+"((?:\\.|[^"])*)"`)
)

// DiscoverSteamGames lists Steam-managed games across native, Flatpak, and
// Selene-managed linked libraries. It reads manifests and directory metadata
// only, following a symbolic link only when the operating system does so while
// reading its destination.
func DiscoverSteamGames(env planner.Environment) ([]SteamGame, error) {
	if runtime.GOOS != "linux" || env.OS != "linux" {
		return nil, errors.New("Steam game discovery is supported only on Linux")
	}
	libraries, err := steamLibraryDirectories(env)
	if err != nil {
		return nil, err
	}
	return discoverSteamGames(libraries), nil
}

// PlatformAssetOverrideFix records the committed transaction for a repair.
type PlatformAssetOverrideFix struct {
	Game          SteamGame
	Engine        GameEngine
	DisabledFile  string
	TransactionID string
	JournalPath   string
}

// FixPlatformAssetOverride disables the Unreal PlatformAssetOverrides plugin
// by renaming its descriptor. The rename is wrapped in a Selene transaction so
// it can be rolled back. Unity games do not exhibit this error and are refused.
func FixPlatformAssetOverride(env planner.Environment, game SteamGame) (PlatformAssetOverrideFix, error) {
	if runtime.GOOS != "linux" || env.OS != "linux" {
		return PlatformAssetOverrideFix{}, errors.New("the PlatformAssetOverrides fix is supported only on Linux")
	}
	analysis, err := AnalyzePlatformAssetOverride(game)
	if err != nil {
		return PlatformAssetOverrideFix{}, err
	}
	if analysis.Engine != GameEngineUnreal {
		return PlatformAssetOverrideFix{}, errors.New("the PlatformAssetOverrides error is Unreal-only; this game does not need the fix")
	}
	if analysis.PlatformPluginDescriptor == "" {
		return PlatformAssetOverrideFix{}, errors.New("no PlatformAssetOverrides plugin descriptor was found to disable")
	}

	descriptor := analysis.PlatformPluginDescriptor
	disabled := descriptor + ".disabled"
	if _, err := os.Lstat(disabled); err == nil {
		return PlatformAssetOverrideFix{}, fmt.Errorf("refusing to overwrite existing path %s", disabled)
	} else if !errors.Is(err, os.ErrNotExist) {
		return PlatformAssetOverrideFix{}, fmt.Errorf("inspect disabled descriptor destination: %w", err)
	}

	tx, err := transaction.Begin(stateRoot(env), fixDescription(game), []transaction.Target{
		{Path: descriptor},
		{Path: disabled},
	}, nil)
	if err != nil {
		return PlatformAssetOverrideFix{}, fmt.Errorf("create fix safety snapshot: %w", err)
	}
	if err := os.Rename(descriptor, disabled); err != nil {
		return PlatformAssetOverrideFix{}, abort(tx, fmt.Errorf("disable PlatformAssetOverrides descriptor: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return PlatformAssetOverrideFix{}, abort(tx, fmt.Errorf("commit fix transaction: %w", err))
	}
	return PlatformAssetOverrideFix{
		Game:          game,
		Engine:        analysis.Engine,
		DisabledFile:  disabled,
		TransactionID: tx.Journal.ID,
		JournalPath:   filepath.Join(tx.Journal.Root, "journal.json"),
	}, nil
}

// UndoPlatformAssetOverrideFix rolls back the newest committed fix for a game,
// restoring the original plugin descriptor. It never stops or restarts Steam.
func UndoPlatformAssetOverrideFix(env planner.Environment, game SteamGame) (PlatformAssetOverrideFix, error) {
	if runtime.GOOS != "linux" || env.OS != "linux" {
		return PlatformAssetOverrideFix{}, errors.New("the PlatformAssetOverrides fix is supported only on Linux")
	}
	journals, err := transaction.List(stateRoot(env))
	if err != nil {
		return PlatformAssetOverrideFix{}, err
	}
	for _, journal := range journals {
		if journal.State != transaction.StateCommitted {
			continue
		}
		if journal.Description != fixDescription(game) {
			continue
		}
		tx, err := transaction.Open(stateRoot(env), journal.ID)
		if err != nil {
			return PlatformAssetOverrideFix{}, err
		}
		if err := tx.Rollback(); err != nil {
			return PlatformAssetOverrideFix{}, fmt.Errorf("undo PlatformAssetOverrides fix: %w", err)
		}
		return PlatformAssetOverrideFix{
			Game:          game,
			TransactionID: tx.Journal.ID,
			JournalPath:   filepath.Join(tx.Journal.Root, "journal.json"),
		}, nil
	}
	return PlatformAssetOverrideFix{}, errors.New("no committed PlatformAssetOverrides fix was found for this game")
}

func fixDescription(game SteamGame) string {
	return "plugin platform-asset-override fix " + game.AppID
}

// AnalyzePlatformAssetOverride identifies the game files relevant to the
// PlatformAssetOverrides error without changing them.
func AnalyzePlatformAssetOverride(game SteamGame) (AssetOverrideAnalysis, error) {
	if !filepath.IsAbs(game.InstallPath) {
		return AssetOverrideAnalysis{}, errors.New("the selected game path must be absolute")
	}
	info, err := os.Stat(game.InstallPath)
	if err != nil {
		return AssetOverrideAnalysis{}, fmt.Errorf("inspect selected game: %w", err)
	}
	if !info.IsDir() {
		return AssetOverrideAnalysis{}, errors.New("the selected game path is not a directory")
	}

	analysis := AssetOverrideAnalysis{Game: game, Engine: GameEngineUnknown}
	entries, err := os.ReadDir(game.InstallPath)
	if err != nil {
		return AssetOverrideAnalysis{}, fmt.Errorf("read selected game directory: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(game.InstallPath, entry.Name())
		if strings.HasSuffix(entry.Name(), ".uproject") && !entry.IsDir() {
			analysis.UnrealProjectFiles = append(analysis.UnrealProjectFiles, path)
			analysis.Engine = GameEngineUnreal
			data, readErr := os.ReadFile(path)
			if readErr == nil && strings.Contains(string(data), "PlatformAssetOverrides") {
				analysis.PlatformPluginReferenced = true
			}
		}
		if entry.IsDir() && strings.HasSuffix(entry.Name(), "_Data") {
			asset := filepath.Join(path, "resources.assets")
			if assetInfo, statErr := os.Stat(asset); statErr == nil && assetInfo.Mode().IsRegular() {
				analysis.UnityAssets = append(analysis.UnityAssets, asset)
				if analysis.Engine == GameEngineUnknown {
					analysis.Engine = GameEngineUnity
				}
			}
		}
	}

	plugin, err := findPlatformPlugin(game.InstallPath)
	if err != nil {
		return AssetOverrideAnalysis{}, err
	}
	analysis.PlatformPluginDescriptor = plugin
	if plugin != "" {
		analysis.Engine = GameEngineUnreal
		analysis.PlatformPluginReferenced = true
	}
	sort.Strings(analysis.UnityAssets)
	sort.Strings(analysis.UnrealProjectFiles)
	return analysis, nil
}

func steamLibraryDirectories(env planner.Environment) ([]string, error) {
	seen := make(map[string]bool)
	var libraries []string
	add := func(path string) {
		if !isSteamLibraryRoot(path) {
			return
		}
		key := canonicalPath(path)
		if seen[key] {
			return
		}
		seen[key] = true
		libraries = append(libraries, path)
	}

	roots := []string{
		filepath.Join(env.Home, ".local", "share", "Steam"),
		filepath.Join(env.Home, ".steam", "steam"),
		filepath.Join(env.Home, ".steam", "root"),
		filepath.Join(env.Home, ".steam", "debian-installation"),
		filepath.Join(env.Home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam"),
	}
	for _, root := range roots {
		add(root)
		data, err := os.ReadFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
		if err != nil {
			continue
		}
		for _, library := range parseLibraryFolders(string(data)) {
			add(library)
		}
	}
	sort.Strings(libraries)
	return libraries, nil
}

func discoverSteamGames(libraries []string) []SteamGame {
	seen := make(map[string]bool)
	var games []SteamGame
	for _, library := range libraries {
		manifests, err := filepath.Glob(filepath.Join(library, "steamapps", "appmanifest_*.acf"))
		if err != nil {
			continue
		}
		sort.Strings(manifests)
		for _, manifest := range manifests {
			data, err := os.ReadFile(manifest)
			if err != nil {
				continue
			}
			game, ok := parseSteamGame(string(data), library)
			if !ok {
				continue
			}
			key := canonicalPath(game.InstallPath) + "\x00" + game.AppID
			if seen[key] {
				continue
			}
			seen[key] = true
			games = append(games, game)
		}
	}
	sort.Slice(games, func(i, j int) bool {
		if games[i].Name == games[j].Name {
			return games[i].AppID < games[j].AppID
		}
		return strings.ToLower(games[i].Name) < strings.ToLower(games[j].Name)
	})
	return games
}

func parseSteamGame(data, library string) (SteamGame, bool) {
	values := make(map[string]string)
	for _, match := range vdfValuePattern.FindAllStringSubmatch(data, -1) {
		if len(match) == 3 {
			values[match[1]] = unescapeVDF(match[2])
		}
	}
	appID, name, installDir := values["appid"], values["name"], values["installdir"]
	if appID == "" || name == "" || installDir == "" || filepath.IsAbs(installDir) {
		return SteamGame{}, false
	}
	installPath := filepath.Join(library, "steamapps", "common", installDir)
	if !isWithin(filepath.Join(library, "steamapps", "common"), installPath) {
		return SteamGame{}, false
	}
	info, err := os.Stat(installPath)
	if err != nil || !info.IsDir() {
		return SteamGame{}, false
	}
	return SteamGame{
		AppID:       appID,
		Name:        name,
		LibraryPath: library,
		InstallPath: installPath,
	}, true
}

func parseLibraryFolders(data string) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, match := range libraryPathPattern.FindAllStringSubmatch(data, -1) {
		if len(match) != 2 {
			continue
		}
		path := unescapeVDF(match[1])
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

func unescapeVDF(value string) string {
	value = strings.ReplaceAll(value, `\\`, `\`)
	value = strings.ReplaceAll(value, `\"`, `"`)
	return value
}

func findPlatformPlugin(gameRoot string) (string, error) {
	var found string
	for _, base := range []string{filepath.Join(gameRoot, "Plugins"), filepath.Join(gameRoot, "Engine", "Plugins")} {
		if _, err := os.Stat(base); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return "", fmt.Errorf("inspect Unreal plugin directory: %w", err)
		}
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(base, path)
			if err != nil {
				return err
			}
			if strings.Count(relative, string(filepath.Separator)) >= 4 && entry.IsDir() {
				return filepath.SkipDir
			}
			if !entry.IsDir() && entry.Name() == "PlatformAssetOverrides.uplugin" {
				found = path
				return fs.SkipAll
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("scan Unreal plugin directory: %w", err)
		}
		if found != "" {
			return found, nil
		}
	}
	return "", nil
}

func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func isWithin(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
