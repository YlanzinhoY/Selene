// Package plugins contains optional, user-scoped Selene integrations.
package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/transaction"
)

// SteamLibrary is an existing Steam library found on a mounted NTFS volume.
// Path is the directory that contains steamapps, not steamapps itself.
type SteamLibrary struct {
	Path       string
	MountPoint string
	Source     string
	Filesystem string
}

type mount struct {
	point      string
	source     string
	filesystem string
}

// DiscoverSteamLibraries finds existing Steam libraries on mounted NTFS
// volumes. It only reads the mount table and directory metadata.
func DiscoverSteamLibraries(env planner.Environment) ([]SteamLibrary, error) {
	if runtime.GOOS != "linux" || env.OS != "linux" {
		return nil, errors.New("the shared Steam library feature is supported only on Linux")
	}
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, fmt.Errorf("read mounted disks: %w", err)
	}
	mounts, err := parseMountInfo(data)
	if err != nil {
		return nil, err
	}
	return discoverSteamLibraries(mounts), nil
}

func parseMountInfo(data []byte) ([]mount, error) {
	mounts, err := parseMountInfoLines(data)
	if err != nil {
		return nil, err
	}
	filtered := mounts[:0]
	for _, candidate := range mounts {
		if isNTFS(candidate.filesystem) {
			filtered = append(filtered, candidate)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].point < filtered[j].point })
	return filtered, nil
}

// parseMountInfoLines returns every mount entry, regardless of filesystem. It
// is shared with the compatdata migration feature, which must compare the
// NTFS source library against the target's native filesystem.
func parseMountInfoLines(data []byte) ([]mount, error) {
	var mounts []mount
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		separator := -1
		for index, field := range fields {
			if field == "-" {
				separator = index
				break
			}
		}
		if separator < 6 || separator+2 >= len(fields) {
			return nil, fmt.Errorf("parse mounted disks: invalid mountinfo line %q", line)
		}
		filesystem := fields[separator+1]
		point, err := decodeMountField(fields[4])
		if err != nil {
			return nil, fmt.Errorf("decode mount point: %w", err)
		}
		source, err := decodeMountField(fields[separator+2])
		if err != nil {
			return nil, fmt.Errorf("decode mount source: %w", err)
		}
		mounts = append(mounts, mount{point: filepath.Clean(point), source: source, filesystem: filesystem})
	}
	sort.Slice(mounts, func(i, j int) bool { return mounts[i].point < mounts[j].point })
	return mounts, nil
}

// findMountFor returns the most specific mount whose point contains path.
func findMountFor(mounts []mount, path string) (mount, bool) {
	clean := filepath.Clean(path)
	best := mount{}
	found := false
	for _, candidate := range mounts {
		point := filepath.Clean(candidate.point)
		if clean != point && !strings.HasPrefix(clean, point+string(filepath.Separator)) {
			continue
		}
		if !found || len(point) > len(filepath.Clean(best.point)) {
			best = candidate
			found = true
		}
	}
	return best, found
}

func discoverSteamLibraries(mounts []mount) []SteamLibrary {
	seen := make(map[string]bool)
	var libraries []SteamLibrary
	for _, mount := range mounts {
		roots := findSteamLibraryRoots(mount.point)
		for _, root := range roots {
			root = filepath.Clean(root)
			if seen[root] {
				continue
			}
			seen[root] = true
			libraries = append(libraries, SteamLibrary{
				Path:       root,
				MountPoint: mount.point,
				Source:     mount.source,
				Filesystem: mount.filesystem,
			})
		}
	}
	sort.Slice(libraries, func(i, j int) bool { return libraries[i].Path < libraries[j].Path })
	return libraries
}

func findSteamLibraryRoots(mountPoint string) []string {
	const maxDepth = 2
	type directory struct {
		path  string
		depth int
	}
	queue := []directory{{path: mountPoint}}
	seen := make(map[string]bool)
	var roots []string

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		current.path = filepath.Clean(current.path)
		if seen[current.path] {
			continue
		}
		seen[current.path] = true

		if isSteamLibraryRoot(current.path) {
			roots = append(roots, current.path)
			continue
		}
		if current.depth >= maxDepth {
			continue
		}
		entries, err := os.ReadDir(current.path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				queue = append(queue, directory{path: filepath.Join(current.path, entry.Name()), depth: current.depth + 1})
			}
		}
	}
	sort.Strings(roots)
	return roots
}

func isSteamLibraryRoot(path string) bool {
	info, err := os.Stat(filepath.Join(path, "steamapps"))
	return err == nil && info.IsDir()
}

func isNTFS(filesystem string) bool {
	return filesystem == "ntfs" || filesystem == "ntfs3" || filesystem == "fuseblk" || filesystem == "fuse.ntfs-3g"
}

func decodeMountField(value string) (string, error) {
	var decoded strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' {
			decoded.WriteByte(value[index])
			continue
		}
		if index+3 >= len(value) {
			return "", errors.New("truncated escape sequence")
		}
		var character byte
		for offset := 1; offset <= 3; offset++ {
			digit := value[index+offset]
			if digit < '0' || digit > '7' {
				return "", errors.New("invalid escape sequence")
			}
			character = character*8 + digit - '0'
		}
		decoded.WriteByte(character)
		index += 3
	}
	return decoded.String(), nil
}

func validateEnvironment(env planner.Environment) error {
	if runtime.GOOS != "linux" || env.OS != "linux" {
		return errors.New("the shared Steam library feature is supported only on Linux")
	}
	if !filepath.IsAbs(env.XDGDataHome) || !filepath.IsAbs(env.XDGStateHome) {
		return errors.New("the shared Steam library feature requires absolute XDG paths")
	}
	return nil
}

func validateLibrary(library SteamLibrary) error {
	if !filepath.IsAbs(library.Path) || !filepath.IsAbs(library.MountPoint) {
		return errors.New("the selected Steam library must be on an absolute mounted path")
	}
	if !isNTFS(library.Filesystem) {
		return fmt.Errorf("the selected library filesystem %q is not NTFS", library.Filesystem)
	}
	if !isSteamLibraryRoot(library.Path) {
		return fmt.Errorf("the selected directory is not an accessible Steam library: %s", library.Path)
	}
	return nil
}

func stateRoot(env planner.Environment) string {
	return filepath.Join(env.XDGStateHome, "selene")
}

// abort marks a transaction failed and rolls it back, reporting the original
// cause unless the rollback itself also failed.
func abort(tx *transaction.Transaction, cause error) error {
	_ = tx.MarkFailed(cause)
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		return fmt.Errorf("%w; plugin rollback also failed: %v", cause, rollbackErr)
	}
	return cause
}