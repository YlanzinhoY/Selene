package transaction

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

type State string

const (
	StateActive      State = "active"
	StateCommitted   State = "committed"
	StateRollingBack State = "rolling_back"
	StateRolledBack  State = "rolled_back"
	StateFailed      State = "failed"
)

// Target is one exact path that must be restored byte-for-byte on rollback.
type Target struct {
	Path      string
	Recursive bool
}

// Pattern tracks a narrow group of files. Files created after Begin are removed
// during rollback, while pre-existing matches are restored through Entries.
type Pattern struct {
	Glob string
}

type Entry struct {
	Path       string      `json:"path"`
	Existed    bool        `json:"existed"`
	Recursive  bool        `json:"recursive,omitempty"`
	Kind       string      `json:"kind,omitempty"`
	Mode       fs.FileMode `json:"mode,omitempty"`
	LinkTarget string      `json:"link_target,omitempty"`
	Backup     string      `json:"backup,omitempty"`
}

type PatternSnapshot struct {
	Glob          string   `json:"glob"`
	OriginalPaths []string `json:"original_paths"`
}

type Journal struct {
	SchemaVersion int               `json:"schema_version"`
	ID            string            `json:"id"`
	Description   string            `json:"description"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	State         State             `json:"state"`
	Root          string            `json:"root"`
	Entries       []Entry           `json:"entries"`
	Patterns      []PatternSnapshot `json:"patterns,omitempty"`
	Error         string            `json:"error,omitempty"`
}

type Transaction struct {
	Journal Journal
}

// Begin snapshots all requested paths before any installer process is started.
func Begin(stateRoot, description string, targets []Target, patterns []Pattern) (*Transaction, error) {
	if !filepath.IsAbs(stateRoot) {
		return nil, errors.New("transaction state root must be absolute")
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(filepath.Clean(stateRoot), "transactions", id)
	if err := os.MkdirAll(filepath.Join(root, "backup"), 0o700); err != nil {
		return nil, fmt.Errorf("create transaction directory: %w", err)
	}

	now := time.Now().UTC()
	tx := &Transaction{Journal: Journal{
		SchemaVersion: 1,
		ID:            id,
		Description:   description,
		CreatedAt:     now,
		UpdatedAt:     now,
		State:         StateActive,
		Root:          root,
	}}

	normalizedTargets, err := normalizeTargets(targets, root)
	if err != nil {
		tx.discardIncomplete()
		return nil, err
	}
	seen := make(map[string]bool)
	for _, target := range normalizedTargets {
		entry, err := tx.snapshot(target, len(tx.Journal.Entries))
		if err != nil {
			tx.discardIncomplete()
			return nil, err
		}
		tx.Journal.Entries = append(tx.Journal.Entries, entry)
		seen[target.Path] = true
	}

	for _, pattern := range patterns {
		snapshot, matches, err := normalizePattern(pattern)
		if err != nil {
			tx.discardIncomplete()
			return nil, err
		}
		for _, match := range matches {
			if seen[match] {
				continue
			}
			entry, err := tx.snapshot(Target{Path: match}, len(tx.Journal.Entries))
			if err != nil {
				tx.discardIncomplete()
				return nil, err
			}
			tx.Journal.Entries = append(tx.Journal.Entries, entry)
			seen[match] = true
		}
		tx.Journal.Patterns = append(tx.Journal.Patterns, snapshot)
	}

	if err := tx.persist(); err != nil {
		tx.discardIncomplete()
		return nil, err
	}
	return tx, nil
}

// Commit records a successful installation while retaining rollback data.
func (tx *Transaction) Commit() error {
	if tx == nil || tx.Journal.State != StateActive {
		return errors.New("only an active transaction can be committed")
	}
	tx.Journal.State = StateCommitted
	tx.Journal.UpdatedAt = time.Now().UTC()
	return tx.persist()
}

// MarkFailed records the installer error before rollback is attempted.
func (tx *Transaction) MarkFailed(cause error) error {
	if tx == nil {
		return errors.New("transaction is nil")
	}
	tx.Journal.State = StateFailed
	tx.Journal.UpdatedAt = time.Now().UTC()
	if cause != nil {
		tx.Journal.Error = cause.Error()
	}
	return tx.persist()
}

// Rollback removes files created by tracked patterns and restores every entry.
func (tx *Transaction) Rollback() error {
	if tx == nil {
		return errors.New("transaction is nil")
	}
	if tx.Journal.State == StateRolledBack {
		return nil
	}
	tx.Journal.State = StateRollingBack
	tx.Journal.UpdatedAt = time.Now().UTC()
	if err := tx.persist(); err != nil {
		return err
	}

	for _, pattern := range tx.Journal.Patterns {
		current, err := filepath.Glob(pattern.Glob)
		if err != nil {
			return tx.rollbackError(fmt.Errorf("expand rollback pattern %s: %w", pattern.Glob, err))
		}
		for _, path := range current {
			path = filepath.Clean(path)
			if slices.Contains(pattern.OriginalPaths, path) {
				continue
			}
			if err := removeTrackedPath(path); err != nil {
				return tx.rollbackError(err)
			}
		}
	}

	for index := len(tx.Journal.Entries) - 1; index >= 0; index-- {
		if err := tx.restore(tx.Journal.Entries[index]); err != nil {
			return tx.rollbackError(err)
		}
	}
	tx.Journal.State = StateRolledBack
	tx.Journal.UpdatedAt = time.Now().UTC()
	return tx.persist()
}

// Load opens a transaction journal previously created by Begin.
func Load(journalPath string) (*Transaction, error) {
	data, err := os.ReadFile(journalPath)
	if err != nil {
		return nil, fmt.Errorf("read transaction journal: %w", err)
	}
	var journal Journal
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return nil, fmt.Errorf("decode transaction journal: %w", err)
	}
	if journal.SchemaVersion != 1 || journal.ID == "" || !filepath.IsAbs(journal.Root) {
		return nil, errors.New("invalid transaction journal")
	}
	expected := filepath.Join(journal.Root, "journal.json")
	if filepath.Clean(journalPath) != filepath.Clean(expected) {
		return nil, errors.New("journal path does not match transaction root")
	}
	return &Transaction{Journal: journal}, nil
}

func (tx *Transaction) snapshot(target Target, index int) (Entry, error) {
	entry := Entry{Path: target.Path, Recursive: target.Recursive}
	info, err := os.Lstat(target.Path)
	if errors.Is(err, os.ErrNotExist) {
		return entry, nil
	}
	if err != nil {
		return Entry{}, fmt.Errorf("inspect transaction target %s: %w", target.Path, err)
	}
	entry.Existed = true
	entry.Mode = info.Mode()
	backupRelative := filepath.ToSlash(filepath.Join("backup", fmt.Sprintf("%04d", index)))
	entry.Backup = backupRelative
	backupPath := filepath.Join(tx.Journal.Root, filepath.FromSlash(backupRelative))

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		entry.Kind = "symlink"
		entry.LinkTarget, err = os.Readlink(target.Path)
		if err != nil {
			return Entry{}, fmt.Errorf("read link %s: %w", target.Path, err)
		}
		if err := os.Symlink(entry.LinkTarget, backupPath); err != nil {
			return Entry{}, fmt.Errorf("backup link %s: %w", target.Path, err)
		}
	case info.IsDir():
		if !target.Recursive {
			return Entry{}, fmt.Errorf("directory target %s must be recursive", target.Path)
		}
		entry.Kind = "directory"
		if err := copyTree(target.Path, backupPath); err != nil {
			return Entry{}, fmt.Errorf("backup directory %s: %w", target.Path, err)
		}
	case info.Mode().IsRegular():
		entry.Kind = "file"
		if err := copyRegularFile(target.Path, backupPath, info.Mode()); err != nil {
			return Entry{}, fmt.Errorf("backup file %s: %w", target.Path, err)
		}
	default:
		return Entry{}, fmt.Errorf("transaction target %s has unsupported type", target.Path)
	}
	return entry, nil
}

func (tx *Transaction) restore(entry Entry) error {
	if err := validateTrackedPath(entry.Path); err != nil {
		return err
	}
	if err := removeTrackedPath(entry.Path); err != nil {
		return err
	}
	if !entry.Existed {
		return nil
	}
	backupPath := filepath.Join(tx.Journal.Root, filepath.FromSlash(entry.Backup))
	if err := ensureWithin(tx.Journal.Root, backupPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(entry.Path), 0o700); err != nil {
		return fmt.Errorf("create restore parent for %s: %w", entry.Path, err)
	}
	switch entry.Kind {
	case "file":
		if err := copyRegularFile(backupPath, entry.Path, entry.Mode); err != nil {
			return fmt.Errorf("restore file %s: %w", entry.Path, err)
		}
	case "directory":
		if err := copyTree(backupPath, entry.Path); err != nil {
			return fmt.Errorf("restore directory %s: %w", entry.Path, err)
		}
	case "symlink":
		if err := os.Symlink(entry.LinkTarget, entry.Path); err != nil {
			return fmt.Errorf("restore link %s: %w", entry.Path, err)
		}
	default:
		return fmt.Errorf("unsupported journal entry kind %q", entry.Kind)
	}
	return nil
}

func (tx *Transaction) persist() error {
	data, err := json.MarshalIndent(tx.Journal, "", "  ")
	if err != nil {
		return fmt.Errorf("encode transaction journal: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(tx.Journal.Root, "journal.json")
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("write transaction journal: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("activate transaction journal: %w", err)
	}
	return nil
}

func (tx *Transaction) rollbackError(err error) error {
	tx.Journal.State = StateFailed
	tx.Journal.UpdatedAt = time.Now().UTC()
	tx.Journal.Error = "rollback: " + err.Error()
	_ = tx.persist()
	return err
}

func (tx *Transaction) discardIncomplete() {
	if tx != nil && tx.Journal.Root != "" {
		_ = os.RemoveAll(tx.Journal.Root)
	}
}

func normalizeTargets(targets []Target, transactionRoot string) ([]Target, error) {
	normalized := make([]Target, 0, len(targets))
	seen := make(map[string]bool)
	for _, target := range targets {
		target.Path = filepath.Clean(target.Path)
		if err := validateTrackedPath(target.Path); err != nil {
			return nil, err
		}
		if strings.ContainsAny(target.Path, "*?[") {
			return nil, fmt.Errorf("exact transaction target contains glob characters: %s", target.Path)
		}
		if within, _ := isWithin(target.Path, transactionRoot); within {
			return nil, fmt.Errorf("transaction root is inside tracked target %s", target.Path)
		}
		if seen[target.Path] {
			continue
		}
		seen[target.Path] = true
		normalized = append(normalized, target)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Path < normalized[j].Path })
	for i, left := range normalized {
		for _, right := range normalized[i+1:] {
			if within, _ := isWithin(left.Path, right.Path); within {
				return nil, fmt.Errorf("overlapping transaction targets: %s and %s", left.Path, right.Path)
			}
		}
	}
	return normalized, nil
}

func normalizePattern(pattern Pattern) (PatternSnapshot, []string, error) {
	cleaned := filepath.Clean(pattern.Glob)
	if !filepath.IsAbs(cleaned) || !strings.ContainsAny(cleaned, "*?[") {
		return PatternSnapshot{}, nil, errors.New("transaction pattern must be an absolute glob")
	}
	matches, err := filepath.Glob(cleaned)
	if err != nil {
		return PatternSnapshot{}, nil, fmt.Errorf("invalid transaction pattern %s: %w", cleaned, err)
	}
	for index := range matches {
		matches[index] = filepath.Clean(matches[index])
		if err := validateTrackedPath(matches[index]); err != nil {
			return PatternSnapshot{}, nil, err
		}
	}
	sort.Strings(matches)
	return PatternSnapshot{Glob: cleaned, OriginalPaths: append([]string(nil), matches...)}, matches, nil
}

func validateTrackedPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("transaction target must be absolute: %s", path)
	}
	cleaned := filepath.Clean(path)
	volume := filepath.VolumeName(cleaned)
	root := volume + string(filepath.Separator)
	if cleaned == root || cleaned == string(filepath.Separator) {
		return errors.New("refusing to track a filesystem root")
	}
	return nil
}

func removeTrackedPath(path string) error {
	if err := validateTrackedPath(path); err != nil {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove tracked path %s: %w", path, err)
	}
	return nil
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
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
			link, err := os.Readlink(current)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode().IsRegular():
			return copyRegularFile(current, target, info.Mode())
		default:
			return fmt.Errorf("unsupported file type in tree: %s", current)
		}
	})
}

func copyRegularFile(source, destination string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func ensureWithin(root, target string) error {
	within, err := isWithin(root, target)
	if err != nil || !within {
		return errors.New("transaction backup escapes transaction root")
	}
	return nil
}

func isWithin(root, target string) (bool, error) {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil {
		return false, err
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative), nil
}

func newID() (string, error) {
	buffer := make([]byte, 6)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("create transaction id: %w", err)
	}
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(buffer), nil
}
