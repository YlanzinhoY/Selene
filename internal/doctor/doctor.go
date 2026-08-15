package doctor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"
)

// Status represents the outcome of a diagnostic check.
type Status string

const (
	StatusOK      Status = "ok"
	StatusWarning Status = "warning"
	StatusError   Status = "error"
	StatusInfo    Status = "info"
)

// Check is one result shown by the TUI.
type Check struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Status  Status   `json:"status"`
	Summary string   `json:"summary"`
	Details []string `json:"details,omitempty"`
}

// Summary contains aggregate diagnostic counters.
type Summary struct {
	OK       int `json:"ok"`
	Warnings int `json:"warnings"`
	Errors   int `json:"errors"`
	Info     int `json:"info"`
}

// Report is a portable, machine-readable snapshot of the host environment.
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	OS          string    `json:"os"`
	Arch        string    `json:"arch"`
	Checks      []Check   `json:"checks"`
	Summary     Summary   `json:"summary"`
}

type steamInstall struct {
	Kind string
	Root string
}

// Run inspects the host without modifying it.
func Run(ctx context.Context) Report {
	report := Report{
		GeneratedAt: time.Now().UTC(),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
	}

	report.Checks = append(report.Checks, checkPlatform())
	if contextDone(ctx) {
		return finish(report)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		report.Checks = append(report.Checks, Check{
			ID: "home", Title: "Home directory", Status: StatusError,
			Summary: "The home directory could not be located.",
			Details: []string{err.Error()},
		})
		return finish(report)
	}

	installs := findSteamInstalls(home)
	report.Checks = append(report.Checks, checkSteam(installs))
	if contextDone(ctx) {
		return finish(report)
	}

	libraries := findSteamLibraries(installs)
	report.Checks = append(report.Checks, checkLibraries(libraries))
	report.Checks = append(report.Checks, checkProton(installs, libraries))
	report.Checks = append(report.Checks, checkDesktopSession())

	return finish(report)
}

func checkPlatform() Check {
	if runtime.GOOS != "linux" {
		return Check{
			ID: "platform", Title: "Platform", Status: StatusWarning,
			Summary: fmt.Sprintf("Detected %s/%s; Selene targets Linux.", runtime.GOOS, runtime.GOARCH),
		}
	}

	name := "Linux"
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		values := parseOSRelease(string(data))
		if pretty := values["PRETTY_NAME"]; pretty != "" {
			name = pretty
		}
	}

	status := StatusOK
	details := []string{"Architecture: " + runtime.GOARCH}
	if runtime.GOARCH != "amd64" {
		status = StatusWarning
		details = append(details, "Initial support is validated on amd64 first.")
	}

	return Check{
		ID: "platform", Title: "Platform", Status: status,
		Summary: name,
		Details: details,
	}
}

func checkSteam(installs []steamInstall) Check {
	if len(installs) == 0 {
		status := StatusError
		if runtime.GOOS != "linux" {
			status = StatusWarning
		}
		return Check{
			ID: "steam", Title: "Steam", Status: status,
			Summary: "No Linux Steam installation was found.",
			Details: []string{
				"Native and Flatpak installations are recognized.",
				"No files were changed.",
			},
		}
	}

	details := make([]string, 0, len(installs))
	for _, install := range installs {
		details = append(details, fmt.Sprintf("%s: %s", install.Kind, install.Root))
	}
	return Check{
		ID: "steam", Title: "Steam", Status: StatusOK,
		Summary: fmt.Sprintf("Found %d installation(s).", len(installs)),
		Details: details,
	}
}

func checkLibraries(libraries []string) Check {
	if len(libraries) == 0 {
		return Check{
			ID: "libraries", Title: "Steam libraries", Status: StatusWarning,
			Summary: "No library containing a steamapps directory was found.",
		}
	}

	return Check{
		ID: "libraries", Title: "Steam libraries", Status: StatusOK,
		Summary: fmt.Sprintf("Found %d library or libraries.", len(libraries)),
		Details: libraries,
	}
}

func checkProton(installs []steamInstall, libraries []string) Check {
	tools := findProtonTools(installs, libraries)
	if len(tools) == 0 {
		return Check{
			ID: "proton", Title: "Proton", Status: StatusWarning,
			Summary: "No Proton installation was found.",
			Details: []string{"Open a game's Steam properties to select a compatibility tool."},
		}
	}

	return Check{
		ID: "proton", Title: "Proton", Status: StatusOK,
		Summary: fmt.Sprintf("Found %d compatibility tool(s).", len(tools)),
		Details: tools,
	}
}

func checkDesktopSession() Check {
	if runtime.GOOS != "linux" {
		return Check{
			ID: "session", Title: "Desktop session", Status: StatusInfo,
			Summary: "The desktop session will be detected when Selene runs on Linux.",
		}
	}

	session := strings.TrimSpace(os.Getenv("XDG_SESSION_TYPE"))
	desktop := strings.TrimSpace(os.Getenv("XDG_CURRENT_DESKTOP"))
	if session == "" && desktop == "" {
		return Check{
			ID: "session", Title: "Desktop session", Status: StatusWarning,
			Summary: "The session's XDG variables are not available.",
		}
	}

	parts := make([]string, 0, 2)
	if desktop != "" {
		parts = append(parts, desktop)
	}
	if session != "" {
		parts = append(parts, session)
	}
	return Check{
		ID: "session", Title: "Desktop session", Status: StatusInfo,
		Summary: strings.Join(parts, " · "),
	}
}

func findSteamInstalls(home string) []steamInstall {
	candidates := []steamInstall{
		{Kind: "native", Root: filepath.Join(home, ".local", "share", "Steam")},
		{Kind: "native", Root: filepath.Join(home, ".steam", "steam")},
		{Kind: "native", Root: filepath.Join(home, ".steam", "root")},
		{Kind: "native", Root: filepath.Join(home, ".steam", "debian-installation")},
		{Kind: "Flatpak", Root: filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam")},
	}

	seen := make(map[string]bool)
	installs := make([]steamInstall, 0, len(candidates))
	for _, candidate := range candidates {
		info, err := os.Stat(candidate.Root)
		if err != nil || !info.IsDir() {
			continue
		}

		root := canonicalPath(candidate.Root)
		if seen[root] {
			continue
		}
		seen[root] = true
		candidate.Root = root
		installs = append(installs, candidate)
	}

	return installs
}

func findSteamLibraries(installs []steamInstall) []string {
	seen := make(map[string]bool)
	var libraries []string

	add := func(path string) {
		path = canonicalPath(path)
		if seen[path] || !isDir(filepath.Join(path, "steamapps")) {
			return
		}
		seen[path] = true
		libraries = append(libraries, path)
	}

	for _, install := range installs {
		add(install.Root)
		data, err := os.ReadFile(filepath.Join(install.Root, "steamapps", "libraryfolders.vdf"))
		if err != nil {
			continue
		}
		for _, path := range parseLibraryFolders(string(data)) {
			add(path)
		}
	}

	sort.Strings(libraries)
	return libraries
}

func findProtonTools(installs []steamInstall, libraries []string) []string {
	seen := make(map[string]bool)
	var tools []string

	addMatches := func(pattern string) {
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			if !isDir(match) {
				continue
			}
			name := filepath.Base(match)
			key := strings.ToLower(name)
			if seen[key] {
				continue
			}
			seen[key] = true
			tools = append(tools, name)
		}
	}

	for _, library := range libraries {
		addMatches(filepath.Join(library, "steamapps", "common", "Proton*"))
	}
	for _, install := range installs {
		addMatches(filepath.Join(install.Root, "compatibilitytools.d", "*"))
	}

	sort.Strings(tools)
	return tools
}

func parseOSRelease(data string) map[string]string {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = strings.Trim(strings.TrimSpace(value), "\"'")
	}
	return values
}

var libraryPathPattern = regexp.MustCompile(`(?m)"path"\s+"([^"]+)"`)

func parseLibraryFolders(data string) []string {
	var paths []string
	for _, match := range libraryPathPattern.FindAllStringSubmatch(data, -1) {
		if len(match) != 2 {
			continue
		}
		path := strings.ReplaceAll(match[1], `\\`, `\`)
		if path != "" && !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	return paths
}

func finish(report Report) Report {
	for _, check := range report.Checks {
		switch check.Status {
		case StatusOK:
			report.Summary.OK++
		case StatusWarning:
			report.Summary.Warnings++
		case StatusError:
			report.Summary.Errors++
		case StatusInfo:
			report.Summary.Info++
		}
	}
	return report
}

func canonicalPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = resolved
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func contextDone(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded)
}
