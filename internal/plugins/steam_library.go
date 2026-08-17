// Package plugins contains optional, user-scoped Selene integrations.
package plugins

import (
	"crypto/sha256"
	"encoding/hex"
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

const steamLibraryPluginID = "steam-library"

// SteamLibrary is an existing Steam library found on a mounted NTFS volume.
// Path is the directory that contains steamapps, not steamapps itself.
type SteamLibrary struct {
	Path       string
	MountPoint string
	Source     string
	Filesystem string
}

// Link is a symbolic link managed by the Steam library plugin.
type Link struct {
	Path   string
	Target string
}

// Result records the committed transaction for a link creation or removal.
type Result struct {
	Link          Link
	TransactionID string
	JournalPath   string
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
		return nil, errors.New("the shared Steam library plugin is supported only on Linux")
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

// ManagedLinks lists only direct symbolic links in Selene's plugin directory.
// Broken links are deliberately retained in the list so users can remove them.
func ManagedLinks(env planner.Environment) ([]Link, error) {
	directory := linksDirectory(env)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list managed Steam library links: %w", err)
	}

	links := make([]Link, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect managed Steam library link %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		target, err := os.Readlink(path)
		if err != nil {
			return nil, fmt.Errorf("read managed Steam library link %s: %w", path, err)
		}
		links = append(links, Link{Path: path, Target: target})
	}
	sort.Slice(links, func(i, j int) bool { return links[i].Path < links[j].Path })
	return links, nil
}

// CreateSteamLibraryLink creates a user-owned alias for a discovered library.
// The mounted volume is never changed. Any failure restores the prior path.
func CreateSteamLibraryLink(env planner.Environment, library SteamLibrary) (Result, error) {
	if err := validateEnvironment(env); err != nil {
		return Result{}, err
	}
	if err := validateLibrary(library); err != nil {
		return Result{}, err
	}

	link := PlannedSteamLibraryLink(env, library)
	if info, err := os.Lstat(link.Path); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return Result{}, fmt.Errorf("refusing to replace non-link path %s", link.Path)
		}
		target, readErr := os.Readlink(link.Path)
		if readErr != nil {
			return Result{}, fmt.Errorf("read existing link %s: %w", link.Path, readErr)
		}
		if filepath.Clean(target) == filepath.Clean(link.Target) {
			return Result{Link: Link{Path: link.Path, Target: target}}, nil
		}
		return Result{}, fmt.Errorf("refusing to replace existing link %s", link.Path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("inspect link destination %s: %w", link.Path, err)
	}

	if err := os.MkdirAll(linksDirectory(env), 0o700); err != nil {
		return Result{}, fmt.Errorf("create plugin link directory: %w", err)
	}
	tx, err := transaction.Begin(stateRoot(env), "plugin steam-library link "+link.Target, []transaction.Target{{Path: link.Path}}, nil)
	if err != nil {
		return Result{}, fmt.Errorf("create link safety snapshot: %w", err)
	}
	if err := os.Symlink(link.Target, link.Path); err != nil {
		return Result{}, abort(tx, fmt.Errorf("create symbolic link %s: %w", link.Path, err))
	}
	if err := tx.Commit(); err != nil {
		return Result{}, abort(tx, fmt.Errorf("commit link transaction: %w", err))
	}
	return Result{
		Link:          link,
		TransactionID: tx.Journal.ID,
		JournalPath:   filepath.Join(tx.Journal.Root, "journal.json"),
	}, nil
}

// PlannedSteamLibraryLink returns the Selene-managed destination for a
// discovered library. It does not create directories or modify files.
func PlannedSteamLibraryLink(env planner.Environment, library SteamLibrary) Link {
	return Link{
		Path:   filepath.Join(linksDirectory(env), libraryLinkName(library)),
		Target: library.Path,
	}
}

// RemoveSteamLibraryLink removes exactly one Selene-managed symbolic link. It
// never follows the link or changes the mounted library it points to.
func RemoveSteamLibraryLink(env planner.Environment, link Link) (Result, error) {
	if err := validateEnvironment(env); err != nil {
		return Result{}, err
	}
	if err := validateManagedLink(env, link.Path); err != nil {
		return Result{}, err
	}
	info, err := os.Lstat(link.Path)
	if err != nil {
		return Result{}, fmt.Errorf("inspect managed Steam library link %s: %w", link.Path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return Result{}, fmt.Errorf("refusing to remove non-link path %s", link.Path)
	}
	target, err := os.Readlink(link.Path)
	if err != nil {
		return Result{}, fmt.Errorf("read managed Steam library link %s: %w", link.Path, err)
	}

	tx, err := transaction.Begin(stateRoot(env), "plugin steam-library unlink "+link.Path, []transaction.Target{{Path: link.Path}}, nil)
	if err != nil {
		return Result{}, fmt.Errorf("create removal safety snapshot: %w", err)
	}
	if err := os.Remove(link.Path); err != nil {
		return Result{}, abort(tx, fmt.Errorf("remove symbolic link %s: %w", link.Path, err))
	}
	if err := tx.Commit(); err != nil {
		return Result{}, abort(tx, fmt.Errorf("commit removal transaction: %w", err))
	}
	return Result{
		Link:          Link{Path: link.Path, Target: target},
		TransactionID: tx.Journal.ID,
		JournalPath:   filepath.Join(tx.Journal.Root, "journal.json"),
	}, nil
}

func parseMountInfo(data []byte) ([]mount, error) {
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
		if !isNTFS(filesystem) {
			continue
		}
		point, err := decodeMountField(fields[4])
		if err != nil {
			return nil, fmt.Errorf("decode NTFS mount point: %w", err)
		}
		source, err := decodeMountField(fields[separator+2])
		if err != nil {
			return nil, fmt.Errorf("decode NTFS mount source: %w", err)
		}
		mounts = append(mounts, mount{point: filepath.Clean(point), source: source, filesystem: filesystem})
	}
	sort.Slice(mounts, func(i, j int) bool { return mounts[i].point < mounts[j].point })
	return mounts, nil
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
		return errors.New("the shared Steam library plugin is supported only on Linux")
	}
	if !filepath.IsAbs(env.XDGDataHome) || !filepath.IsAbs(env.XDGStateHome) {
		return errors.New("the shared Steam library plugin requires absolute XDG paths")
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

func validateManagedLink(env planner.Environment, path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("the managed link path must be absolute")
	}
	parent := linksDirectory(env)
	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(parent) {
		return errors.New("refusing to remove a link outside Selene's plugin directory")
	}
	return nil
}

func linksDirectory(env planner.Environment) string {
	return filepath.Join(env.XDGDataHome, "selene", "plugins", steamLibraryPluginID)
}

func stateRoot(env planner.Environment) string {
	return filepath.Join(env.XDGStateHome, "selene")
}

func libraryLinkName(library SteamLibrary) string {
	label := strings.ToLower(filepath.Base(filepath.Clean(library.MountPoint)))
	var clean strings.Builder
	for _, character := range label {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			clean.WriteRune(character)
		} else {
			clean.WriteByte('-')
		}
	}
	label = strings.Trim(clean.String(), "-")
	if label == "" || label == "." {
		label = "steam-library"
	}
	digest := sha256.Sum256([]byte(filepath.Clean(library.Path)))
	return label + "-" + hex.EncodeToString(digest[:4])
}

func abort(tx *transaction.Transaction, cause error) error {
	_ = tx.MarkFailed(cause)
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		return fmt.Errorf("%w; plugin rollback also failed: %v", cause, rollbackErr)
	}
	return cause
}
