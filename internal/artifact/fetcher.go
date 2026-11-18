package artifact

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/selene-linux/selene/internal/catalog"
)

const (
	maxArchiveEntries    = 20_000
	maxUncompressedBytes = 1 << 30
)

// Result describes one verified artifact in the local cache.
type Result struct {
	Component string `json:"component"`
	Version   string `json:"version"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Cached    bool   `json:"cached"`
}

// Fetcher downloads artifacts without extracting or installing them.
type Fetcher struct {
	Client *http.Client
}

// NewFetcher returns a client with bounded time and HTTPS-only redirects.
func NewFetcher() *Fetcher {
	return &Fetcher{Client: &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return errors.New("redirected to a non-HTTPS URL")
			}
			return nil
		},
	}}
}

// Fetch downloads, verifies and inspects one component artifact.
func (f *Fetcher) Fetch(ctx context.Context, component catalog.Component, cacheDir string) (Result, error) {
	if f == nil || f.Client == nil {
		return Result{}, errors.New("artifact fetcher has no HTTP client")
	}
	if err := validateArtifactInput(component); err != nil {
		return Result{}, err
	}
	if cacheDir == "" || !filepath.IsAbs(cacheDir) {
		return Result{}, errors.New("artifact cache directory must be absolute")
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("create artifact cache: %w", err)
	}

	target := filepath.Join(cacheDir, component.Artifact.SHA256+"-"+component.Artifact.Name)
	if err := ensureWithin(cacheDir, target); err != nil {
		return Result{}, err
	}
	if valid, err := verifyExisting(target, component); err != nil {
		return Result{}, err
	} else if valid {
		return resultFor(component, target, true), nil
	}

	sources := component.Artifact.Sources()
	var temporaryPath string
	var failures []string
	for _, source := range sources {
		downloaded, err := f.download(ctx, component, source, cacheDir)
		if err == nil {
			temporaryPath = downloaded
			break
		}
		if len(sources) == 1 || ctx.Err() != nil {
			return Result{}, fmt.Errorf("download %s: %w", component.ID, err)
		}
		failures = append(failures, sourceLabel(source)+": "+err.Error())
	}
	if temporaryPath == "" {
		return Result{}, fmt.Errorf("download %s: every source failed: %s", component.ID, strings.Join(failures, "; "))
	}
	defer os.Remove(temporaryPath)

	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() {
			return Result{}, fmt.Errorf("refusing to replace non-regular cache entry %s", target)
		}
		if err := os.Remove(target); err != nil {
			return Result{}, fmt.Errorf("replace invalid cache entry: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("inspect cache entry: %w", err)
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return Result{}, fmt.Errorf("activate cached artifact: %w", err)
	}
	return resultFor(component, target, false), nil
}

// download fetches one source into a private temporary file inside cacheDir
// and returns its path only after the byte count, SHA-256 and archive
// inspection all match the pinned artifact.
func (f *Fetcher) download(ctx context.Context, component catalog.Component, source, cacheDir string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return "", fmt.Errorf("create artifact request: %w", err)
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "Selene/dev")

	response, err := f.Client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %s", response.Status)
	}
	if response.ContentLength >= 0 && response.ContentLength != component.Artifact.Size {
		return "", fmt.Errorf("content length %d, expected %d", response.ContentLength, component.Artifact.Size)
	}

	temporary, err := os.CreateTemp(cacheDir, ".selene-partial-*")
	if err != nil {
		return "", fmt.Errorf("create temporary artifact: %w", err)
	}
	temporaryPath := temporary.Name()
	fail := func(cause error) (string, error) {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return "", cause
	}
	if err := temporary.Chmod(0o600); err != nil {
		return fail(fmt.Errorf("secure temporary artifact: %w", err))
	}

	hasher := sha256.New()
	limited := io.LimitReader(response.Body, component.Artifact.Size+1)
	written, err := io.Copy(io.MultiWriter(temporary, hasher), limited)
	if err != nil {
		return fail(err)
	}
	if written != component.Artifact.Size {
		return fail(fmt.Errorf("received %d bytes, expected %d", written, component.Artifact.Size))
	}
	if !hashMatches(hasher.Sum(nil), component.Artifact.SHA256) {
		return fail(errors.New("SHA-256 mismatch"))
	}
	if err := temporary.Sync(); err != nil {
		return fail(fmt.Errorf("sync artifact: %w", err))
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("close artifact: %w", err)
	}
	if err := Inspect(temporaryPath, component); err != nil {
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("inspect: %w", err)
	}
	return temporaryPath, nil
}

// sourceLabel names a download source by its host in aggregated errors.
func sourceLabel(source string) string {
	if parsed, err := url.Parse(source); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return source
}

// Inspect rejects unsafe archives and confirms required files before install.
func Inspect(filePath string, component catalog.Component) error {
	switch component.Artifact.Format {
	case "file":
		return nil
	case "zip":
		return inspectZIP(filePath, component)
	default:
		return fmt.Errorf("unsupported artifact format %q", component.Artifact.Format)
	}
}

func inspectZIP(filePath string, component catalog.Component) error {
	reader, err := zip.OpenReader(filePath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > maxArchiveEntries {
		return fmt.Errorf("zip contains too many entries: %d", len(reader.File))
	}

	required := make(map[string]bool)
	for _, marker := range append(slicesCopy(component.Install.Validate), component.Install.Executables...) {
		required[path.Clean(marker)] = false
	}
	seen := make(map[string]bool, len(reader.File))
	var expanded uint64
	for _, entry := range reader.File {
		if entry.Flags&0x1 != 0 {
			return fmt.Errorf("encrypted zip entry %q is not supported", entry.Name)
		}
		normalized, skip, err := safeArchivePath(entry.Name, component.Install.StripComponents)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		mode := entry.Mode()
		if mode&os.ModeSymlink != 0 {
			return fmt.Errorf("zip entry %q is a symbolic link", entry.Name)
		}
		if !mode.IsRegular() && !mode.IsDir() {
			return fmt.Errorf("zip entry %q has unsupported file type", entry.Name)
		}
		if seen[normalized] {
			return fmt.Errorf("zip contains duplicate destination %q", normalized)
		}
		seen[normalized] = true
		if !mode.IsDir() {
			expanded += entry.UncompressedSize64
			if expanded > maxUncompressedBytes {
				return errors.New("zip expands beyond the safety limit")
			}
		}
		if _, ok := required[normalized]; ok && !mode.IsDir() {
			required[normalized] = true
		}
	}

	var missing []string
	for marker, found := range required {
		if !found {
			missing = append(missing, marker)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("zip is missing required content: %s", strings.Join(missing, ", "))
	}
	return nil
}

func safeArchivePath(name string, stripComponents int) (string, bool, error) {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, `\`) {
		return "", false, fmt.Errorf("zip contains unsafe path %q", name)
	}
	if strings.HasPrefix(name, "/") {
		return "", false, fmt.Errorf("zip contains absolute path %q", name)
	}
	cleaned := path.Clean(name)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false, fmt.Errorf("zip path escapes destination: %q", name)
	}
	parts := strings.Split(cleaned, "/")
	if len(parts) <= stripComponents {
		return "", true, nil
	}
	cleaned = path.Join(parts[stripComponents:]...)
	if cleaned == "." || cleaned == "" {
		return "", true, nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false, fmt.Errorf("zip path escapes destination after stripping: %q", name)
	}
	return cleaned, false, nil
}

func verifyExisting(target string, component catalog.Component) (bool, error) {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect cached artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("cache entry is not a regular file: %s", target)
	}
	if info.Size() != component.Artifact.Size {
		return false, nil
	}
	file, err := os.Open(target)
	if err != nil {
		return false, fmt.Errorf("open cached artifact: %w", err)
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if copyErr != nil {
		return false, fmt.Errorf("hash cached artifact: %w", copyErr)
	}
	if closeErr != nil {
		return false, fmt.Errorf("close cached artifact: %w", closeErr)
	}
	if !hashMatches(hasher.Sum(nil), component.Artifact.SHA256) {
		return false, nil
	}
	if err := Inspect(target, component); err != nil {
		return false, nil
	}
	return true, nil
}

func validateArtifactInput(component catalog.Component) error {
	for _, source := range component.Artifact.Sources() {
		parsed, err := url.Parse(source)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return errors.New("artifact URL must use HTTPS")
		}
	}
	if component.Artifact.Size <= 0 || component.Artifact.Size > maxUncompressedBytes {
		return errors.New("artifact size is outside the supported range")
	}
	if filepath.Base(component.Artifact.Name) != component.Artifact.Name || strings.ContainsAny(component.Artifact.Name, `/\`) {
		return errors.New("artifact name must not contain path separators")
	}
	expected, err := hex.DecodeString(component.Artifact.SHA256)
	if err != nil || len(expected) != sha256.Size {
		return errors.New("artifact SHA-256 is invalid")
	}
	return nil
}

func hashMatches(actual []byte, expectedHex string) bool {
	expected, err := hex.DecodeString(expectedHex)
	if err != nil || len(expected) != sha256.Size || len(actual) != sha256.Size {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func ensureWithin(root, target string) error {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("artifact cache target escapes the cache directory")
	}
	return nil
}

func resultFor(component catalog.Component, target string, cached bool) Result {
	return Result{
		Component: component.ID,
		Version:   component.Version,
		Path:      target,
		SHA256:    component.Artifact.SHA256,
		Size:      component.Artifact.Size,
		Cached:    cached,
	}
}

func slicesCopy(values []string) []string {
	return append([]string(nil), values...)
}
