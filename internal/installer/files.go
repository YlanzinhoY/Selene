package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/selene-linux/selene/internal/catalog"
)

func prepareLumenCandidate(dataHome, transactionRoot, current, lumenStage, pluginStage string, source catalog.Catalog) (string, error) {
	if err := os.MkdirAll(dataHome, 0o755); err != nil {
		return "", fmt.Errorf("create stack data directory: %w", err)
	}
	candidate, err := os.MkdirTemp(dataHome, ".selene-Lumen-stage-")
	if err != nil {
		return "", fmt.Errorf("create Lumen candidate: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(candidate)
		}
	}()

	if info, err := os.Lstat(current); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("existing Lumen destination is not a regular directory")
		}
		if err := copyTreeContents(current, candidate, true); err != nil {
			return "", fmt.Errorf("copy existing Lumen tree: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect existing Lumen tree: %w", err)
	}
	if err := copyTreeContents(lumenStage, candidate, false); err != nil {
		return "", fmt.Errorf("overlay verified Lumen: %w", err)
	}

	pluginDestination := filepath.Join(candidate, "luatools")
	preserve := filepath.Join(transactionRoot, "preserve", "plugin-data")
	legacyAPI := filepath.Join(pluginDestination, "backend", "api.json")
	dataSource := filepath.Join(pluginDestination, "backend", "data")
	if info, err := os.Lstat(dataSource); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		if err := os.MkdirAll(preserve, 0o700); err != nil {
			return "", err
		}
		if err := copyTreeContents(dataSource, preserve, true); err != nil {
			return "", fmt.Errorf("preserve LuaTools data: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect LuaTools data: %w", err)
	}
	legacyTarget := filepath.Join(preserve, "api.json")
	if _, err := os.Stat(legacyTarget); errors.Is(err, os.ErrNotExist) {
		if info, legacyErr := os.Lstat(legacyAPI); legacyErr == nil && info.Mode().IsRegular() {
			if err := copyFileAtomic(legacyAPI, legacyTarget, info.Mode()); err != nil {
				return "", fmt.Errorf("preserve legacy LuaTools API catalog: %w", err)
			}
		}
	}
	if err := os.RemoveAll(pluginDestination); err != nil {
		return "", fmt.Errorf("replace LuaTools candidate: %w", err)
	}
	if err := os.MkdirAll(pluginDestination, 0o755); err != nil {
		return "", err
	}
	if err := copyTreeContents(pluginStage, pluginDestination, false); err != nil {
		return "", fmt.Errorf("copy verified LuaTools plugin: %w", err)
	}
	if info, err := os.Stat(preserve); err == nil && info.IsDir() {
		dataDestination := filepath.Join(pluginDestination, "backend", "data")
		if err := os.MkdirAll(dataDestination, 0o700); err != nil {
			return "", err
		}
		if err := copyTreeContents(preserve, dataDestination, true); err != nil {
			return "", fmt.Errorf("restore LuaTools data into candidate: %w", err)
		}
	}

	versions := make(map[string]map[string]any)
	for _, component := range source.Components {
		if component.ID == "cloudredirect-moon" {
			continue
		}
		versions[component.ID] = map[string]any{
			"version": component.Version,
			"sha256":  component.Artifact.SHA256,
			"size":    component.Artifact.Size,
		}
	}
	versionsPath := filepath.Join(candidate, "versions.json")
	if err := writeJSONAtomic(versionsPath, map[string]any{
		"catalog_revision": source.Revision,
		"installed_at":     time.Now().UTC(),
		"components":       versions,
	}); err != nil {
		return "", err
	}

	success = true
	return candidate, nil
}

func activateDirectory(candidate, destination string) error {
	parent := filepath.Dir(destination)
	previous, err := os.MkdirTemp(parent, ".selene-Lumen-previous-")
	if err != nil {
		return fmt.Errorf("reserve activation backup path: %w", err)
	}
	if err := os.Remove(previous); err != nil {
		return fmt.Errorf("prepare activation backup path: %w", err)
	}
	hadPrevious := false
	if _, err := os.Lstat(destination); err == nil {
		if err := os.Rename(destination, previous); err != nil {
			return fmt.Errorf("move current Lumen aside: %w", err)
		}
		hadPrevious = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect Lumen destination: %w", err)
	}
	if err := os.Rename(candidate, destination); err != nil {
		if hadPrevious {
			_ = os.Rename(previous, destination)
		}
		return fmt.Errorf("activate Lumen candidate: %w", err)
	}
	if hadPrevious {
		if err := os.RemoveAll(previous); err != nil {
			return fmt.Errorf("remove superseded Lumen tree: %w", err)
		}
	}
	return nil
}

func seedSLSConfig(source, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("inspect SLSsteam config template: %w", err)
	}
	return copyFileAtomic(source, destination, info.Mode())
}

func copyTreeContents(source, destination string, allowSymlinks bool) error {
	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == source {
			return nil
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			if !allowSymlinks {
				return fmt.Errorf("verified staging tree contains symlink %s", current)
			}
			if _, err := os.Lstat(target); err == nil {
				if err := os.RemoveAll(target); err != nil {
					return err
				}
			}
			link, err := os.Readlink(current)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.IsDir():
			if existing, err := os.Lstat(target); err == nil {
				if !existing.IsDir() || existing.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("refusing to traverse non-directory destination %s", target)
				}
				return nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode().IsRegular():
			return copyFileAtomic(current, target, info.Mode())
		default:
			return fmt.Errorf("unsupported file type in %s", current)
		}
	})
}

func copyFileAtomic(source, destination string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if info, err := os.Lstat(destination); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace symlink %s", destination)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".selene-file-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode.Perm()); err != nil {
		temporary.Close()
		return err
	}
	_, copyErr := io.Copy(temporary, input)
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if info, err := os.Lstat(destination); err == nil {
		if info.IsDir() {
			return fmt.Errorf("refusing to replace directory with file: %s", destination)
		}
		if err := os.Remove(destination); err != nil {
			return err
		}
	}
	return os.Rename(temporaryPath, destination)
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(temporary)
			return fmt.Errorf("refusing to replace non-regular JSON destination %s", path)
		}
		if err := os.Remove(path); err != nil {
			_ = os.Remove(temporary)
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(temporary)
		return err
	}
	return os.Rename(temporary, path)
}
