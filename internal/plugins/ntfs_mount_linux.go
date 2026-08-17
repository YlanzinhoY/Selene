//go:build linux

package plugins

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const ntfsTrustedPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

type ntfsRepairDependencies struct {
	assess       func(SteamLibrary) NTFSMountAssessment
	steamActive  func() bool
	libraryReady func(string) bool
	run          func(context.Context, string, ...string) ([]byte, error)
	euid         func() int
	egid         func() int
}

func inspectNTFSFilenameCompatibility(library SteamLibrary) NTFSMountAssessment {
	return inspectNTFSFilenameCompatibilityAt("/proc", library)
}

func inspectNTFSFilenameCompatibilityAt(procRoot string, library SteamLibrary) NTFSMountAssessment {
	assessment := NTFSMountAssessment{Compatibility: FilenameCompatibilityUnknown}
	if !isNTFS(library.Filesystem) {
		assessment.Reason = "the selected library is not on NTFS"
		return assessment
	}

	driver, options, ok := findNTFSMountProcess(procRoot, library.Source, library.MountPoint)
	if ok {
		assessment.Driver = driver
		if hasMountOption(options, "ignore_case") {
			assessment.Compatibility = FilenameCompatibilityIncompatible
			assessment.ForcedLowercase = true
			assessment.CanRepair = validNTFSDevice(library.Source)
			assessment.Reason = "the active lowntfs-3g mount exposes every filename in lowercase"
			return assessment
		}
		if isRecognizedNTFSDriver(driver) {
			assessment.Compatibility = FilenameCompatibilityCompatible
			assessment.Reason = "the active NTFS mount preserves directory-entry spelling"
			return assessment
		}
	}

	if library.Filesystem == "ntfs3" {
		assessment.Driver = "ntfs3"
		assessment.Compatibility = FilenameCompatibilityCompatible
		assessment.Reason = "the kernel NTFS mount does not report forced lowercase filenames"
		return assessment
	}
	assessment.Reason = "Selene could not determine the active NTFS filename policy"
	return assessment
}

func findNTFSMountProcess(procRoot, source, mountPoint string) (string, []string, bool) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return "", nil, false
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if !entry.IsDir() || !allDigits(entry.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(procRoot, entry.Name(), "cmdline"))
		if err != nil || len(data) == 0 {
			continue
		}
		arguments := splitProcessArguments(data)
		if len(arguments) < 3 || !sameMountArgument(arguments[1], source) || !sameMountArgument(arguments[2], mountPoint) {
			continue
		}
		driver := filepath.Base(arguments[0])
		if !isRecognizedNTFSDriver(driver) {
			continue
		}
		return driver, mountOptionsFromArguments(arguments), true
	}
	return "", nil, false
}

func splitProcessArguments(data []byte) []string {
	parts := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	arguments := parts[:0]
	for _, part := range parts {
		if part != "" {
			arguments = append(arguments, part)
		}
	}
	return arguments
}

func sameMountArgument(actual, expected string) bool {
	if filepath.Clean(actual) == filepath.Clean(expected) {
		return true
	}
	actualResolved, actualErr := filepath.EvalSymlinks(actual)
	expectedResolved, expectedErr := filepath.EvalSymlinks(expected)
	return actualErr == nil && expectedErr == nil && filepath.Clean(actualResolved) == filepath.Clean(expectedResolved)
}

func mountOptionsFromArguments(arguments []string) []string {
	for index := 1; index < len(arguments); index++ {
		if arguments[index] == "-o" && index+1 < len(arguments) {
			return strings.Split(arguments[index+1], ",")
		}
		if strings.HasPrefix(arguments[index], "-o") && len(arguments[index]) > 2 {
			return strings.Split(strings.TrimPrefix(arguments[index], "-o"), ",")
		}
	}
	return nil
}

func hasMountOption(options []string, wanted string) bool {
	for _, option := range options {
		if strings.TrimSpace(option) == wanted {
			return true
		}
	}
	return false
}

func isRecognizedNTFSDriver(driver string) bool {
	switch driver {
	case "mount.ntfs", "mount.ntfs-3g", "mount.lowntfs-3g", "ntfs-3g", "lowntfs-3g":
		return true
	default:
		return false
	}
}

func validNTFSDevice(source string) bool {
	clean := filepath.Clean(source)
	return filepath.IsAbs(clean) && clean != "/dev" && strings.HasPrefix(clean, "/dev/")
}

func repairNTFSFilenameCompatibility(ctx context.Context, library SteamLibrary) (NTFSSessionRepairResult, error) {
	dependencies := ntfsRepairDependencies{
		assess:       InspectNTFSFilenameCompatibility,
		steamActive:  steamRunning,
		libraryReady: isSteamLibraryRoot,
		run:          runNTFSCommand,
		euid:         os.Geteuid,
		egid:         os.Getegid,
	}
	return repairNTFSFilenameCompatibilityWith(ctx, library, dependencies)
}

func repairNTFSFilenameCompatibilityWith(ctx context.Context, library SteamLibrary, dependencies ntfsRepairDependencies) (NTFSSessionRepairResult, error) {
	result := NTFSSessionRepairResult{
		Library:    library,
		MountPoint: filepath.Clean(library.MountPoint),
		Device:     filepath.Clean(library.Source),
		Temporary:  true,
	}
	if !filepath.IsAbs(library.Path) || !filepath.IsAbs(library.MountPoint) || !isNTFS(library.Filesystem) {
		return result, errors.New("the selected library is not a valid mounted NTFS Steam library")
	}
	if !validNTFSDevice(library.Source) {
		return result, fmt.Errorf("the NTFS source is not a supported block device: %s", library.Source)
	}
	if !dependencies.libraryReady(library.Path) {
		return result, errors.New("the selected Steam library is no longer accessible at its mounted path")
	}
	result.Before = dependencies.assess(library)
	if result.Before.Compatibility == FilenameCompatibilityCompatible {
		result.After = result.Before
		return result, nil
	}
	if result.Before.Compatibility != FilenameCompatibilityIncompatible || !result.Before.CanRepair {
		return result, errors.New("the active NTFS filename policy cannot be repaired automatically")
	}
	if dependencies.steamActive() {
		return result, errors.New("Steam started again before the NTFS remount; close it and review the plan again")
	}

	if _, err := dependencies.run(ctx, "udisksctl", "unmount", "-b", result.Device); err != nil {
		return result, fmt.Errorf("unmount the NTFS volume without force: %w", err)
	}

	restore := func(cause error) error {
		restoreContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, restoreErr := dependencies.run(restoreContext, "udisksctl", "mount", "-b", result.Device)
		if restoreErr != nil {
			return fmt.Errorf("%w; restoring the operating system mount also failed: %v", cause, restoreErr)
		}
		return cause
	}
	options := strings.Join([]string{
		"rw",
		"noatime",
		"uid=" + strconv.Itoa(dependencies.euid()),
		"gid=" + strconv.Itoa(dependencies.egid()),
		"umask=0022",
		"windows_names",
		"big_writes",
	}, ",")
	if _, err := dependencies.run(ctx, "udisksctl", "mount", "-b", result.Device, "-t", "ntfs", "-o", options); err != nil {
		return result, restore(fmt.Errorf("remount NTFS with case-preserving names: %w", err))
	}

	result.After = dependencies.assess(library)
	if result.After.Compatibility != FilenameCompatibilityCompatible || result.After.ForcedLowercase || !dependencies.libraryReady(library.Path) {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, unmountErr := dependencies.run(cleanupContext, "udisksctl", "unmount", "-b", result.Device)
		cause := errors.New("the remounted volume did not pass the filename compatibility check")
		if unmountErr != nil {
			cause = fmt.Errorf("%w; remove the unsuccessful remount: %v", cause, unmountErr)
		}
		return result, restore(cause)
	}
	return result, nil
}

func runNTFSCommand(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	path, ok := trustedNTFSCommand(name)
	if !ok {
		return nil, fmt.Errorf("required command %q was not found in the trusted system path", name)
	}
	command := exec.CommandContext(ctx, path, arguments...)
	command.Env = sanitizedNTFSEnvironment()
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return output, err
		}
		return output, fmt.Errorf("%s: %w", message, err)
	}
	return output, nil
}

func trustedNTFSCommand(name string) (string, bool) {
	if filepath.Base(name) != name || name == "." || name == "" {
		return "", false
	}
	for _, directory := range strings.Split(ntfsTrustedPath, ":") {
		candidate := filepath.Join(directory, name)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, true
		}
	}
	return "", false
}

func sanitizedNTFSEnvironment() []string {
	allowed := map[string]bool{
		"DBUS_SESSION_BUS_ADDRESS": true,
		"HOME":                     true,
		"LANG":                     true,
		"LANGUAGE":                 true,
		"LC_ALL":                   true,
		"LC_MESSAGES":              true,
		"LOGNAME":                  true,
		"USER":                     true,
		"XDG_RUNTIME_DIR":          true,
	}
	environment := []string{"PATH=" + ntfsTrustedPath}
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found && allowed[name] {
			environment = append(environment, entry)
		}
	}
	return environment
}
