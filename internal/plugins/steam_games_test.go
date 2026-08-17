package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLibraryFoldersUnescapesAndDeduplicates(t *testing.T) {
	data := `
"libraryfolders"
{
  "0" { "path" "/mnt/Games/SteamLibrary" }
  "1" { "path" "/mnt/Games/SteamLibrary" }
  "2" { "path" "/mnt/Game\\ Disk/SteamLibrary" }
}`
	paths := parseLibraryFolders(data)
	if len(paths) != 2 || paths[0] != "/mnt/Games/SteamLibrary" || paths[1] != `/mnt/Game\ Disk/SteamLibrary` {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestDiscoverSteamGamesFollowsLibraryDirectories(t *testing.T) {
	root := t.TempDir()
	library := filepath.Join(root, "SteamLibrary")
	install := filepath.Join(library, "steamapps", "common", "LEGO Batman")
	if err := os.MkdirAll(install, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `"AppState"
{
  "appid" "1234"
  "name" "LEGO Batman"
  "installdir" "LEGO Batman"
}`
	if err := os.WriteFile(filepath.Join(library, "steamapps", "appmanifest_1234.acf"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	games := discoverSteamGames([]string{library})
	if len(games) != 1 || games[0].AppID != "1234" || games[0].Name != "LEGO Batman" || games[0].InstallPath != install {
		t.Fatalf("games = %#v", games)
	}
}

func TestAnalyzePlatformAssetOverrideFindsUnityAndUnrealSignals(t *testing.T) {
	root := t.TempDir()
	unityAsset := filepath.Join(root, "Batman_Data", "resources.assets")
	plugin := filepath.Join(root, "Plugins", "PlatformAssetOverrides", "PlatformAssetOverrides.uplugin")
	project := filepath.Join(root, "Batman.uproject")
	for _, path := range []string{unityAsset, plugin, project} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("PlatformAssetOverrides"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	analysis, err := AnalyzePlatformAssetOverride(SteamGame{InstallPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Engine != GameEngineUnreal || len(analysis.UnityAssets) != 1 || analysis.PlatformPluginDescriptor != plugin || !analysis.PlatformPluginReferenced {
		t.Fatalf("analysis = %#v", analysis)
	}
}
