package artifact

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/selene-linux/selene/internal/catalog"
)

// Extract expands a previously verified artifact into a new staging directory.
// It never writes directly to an installation destination.
func Extract(artifactPath string, component catalog.Component, destination string) error {
	if !filepath.IsAbs(destination) {
		return errors.New("staging destination must be absolute")
	}
	if err := validateStagingDestination(destination); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("staging destination already exists: %s", destination)
		}
		return fmt.Errorf("inspect staging destination: %w", err)
	}
	if err := Inspect(artifactPath, component); err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return fmt.Errorf("create staging destination: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(destination)
		}
	}()

	var err error
	switch component.Artifact.Format {
	case "zip":
		err = extractZIP(artifactPath, component, destination)
	case "file":
		target := filepath.Join(destination, component.Artifact.Name)
		err = copyStagedFile(artifactPath, target, 0o644, component.Artifact.Size)
	default:
		err = fmt.Errorf("unsupported artifact format %q", component.Artifact.Format)
	}
	if err != nil {
		return err
	}
	success = true
	return nil
}

func extractZIP(artifactPath string, component catalog.Component, destination string) error {
	reader, err := zip.OpenReader(artifactPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer reader.Close()

	for _, entry := range reader.File {
		normalized, skip, err := safeArchivePath(entry.Name, component.Install.StripComponents)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(normalized))
		if err := ensureWithin(destination, target); err != nil {
			return err
		}
		mode := entry.Mode()
		if mode.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create staged directory %s: %w", normalized, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create staged parent %s: %w", normalized, err)
		}
		permission := fs.FileMode(0o644)
		if slices.Contains(component.Install.Executables, normalized) {
			permission = 0o755
		}
		input, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", normalized, err)
		}
		err = writeStagedReader(input, target, permission, int64(entry.UncompressedSize64))
		closeErr := input.Close()
		if err != nil {
			return fmt.Errorf("extract zip entry %s: %w", normalized, err)
		}
		if closeErr != nil {
			return fmt.Errorf("close zip entry %s: %w", normalized, closeErr)
		}
	}
	return nil
}

func copyStagedFile(source, destination string, mode fs.FileMode, expectedSize int64) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	return writeStagedReader(input, destination, mode, expectedSize)
}

func writeStagedReader(input io.Reader, destination string, mode fs.FileMode, expectedSize int64) error {
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(input, expectedSize+1))
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if written != expectedSize {
		return fmt.Errorf("wrote %d bytes, expected %d", written, expectedSize)
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func validateStagingDestination(destination string) error {
	cleaned := filepath.Clean(destination)
	root := filepath.VolumeName(cleaned) + string(filepath.Separator)
	if cleaned == root || cleaned == string(filepath.Separator) {
		return errors.New("refusing to use filesystem root as staging destination")
	}
	return nil
}
