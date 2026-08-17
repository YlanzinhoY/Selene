//go:build linux

package plugins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInspectNTFSMountDetectsForcedLowercase(t *testing.T) {
	procRoot := t.TempDir()
	writeMountProcess(t, procRoot, "101", []string{
		"/sbin/mount.lowntfs-3g",
		"/dev/sdb1",
		"/run/media/player/Games",
		"-o",
		"rw,uid=1000,ignore_case,windows_names",
	})

	assessment := inspectNTFSFilenameCompatibilityAt(procRoot, testNTFSLibrary())
	if assessment.Compatibility != FilenameCompatibilityIncompatible {
		t.Fatalf("compatibility = %q, want incompatible", assessment.Compatibility)
	}
	if !assessment.ForcedLowercase || !assessment.CanRepair {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}
	if assessment.Driver != "mount.lowntfs-3g" {
		t.Fatalf("driver = %q", assessment.Driver)
	}
}

func TestInspectNTFSMountAcceptsCasePreservingNTFS3G(t *testing.T) {
	procRoot := t.TempDir()
	writeMountProcess(t, procRoot, "202", []string{
		"/sbin/mount.ntfs",
		"/dev/sdb1",
		"/run/media/player/Games",
		"-o",
		"rw,uid=1000,windows_names,big_writes",
	})

	assessment := inspectNTFSFilenameCompatibilityAt(procRoot, testNTFSLibrary())
	if assessment.Compatibility != FilenameCompatibilityCompatible || assessment.ForcedLowercase {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}
}

func TestInspectNTFSMountIgnoresDifferentDeviceAndMount(t *testing.T) {
	procRoot := t.TempDir()
	writeMountProcess(t, procRoot, "303", []string{
		"/sbin/mount.lowntfs-3g",
		"/dev/sdc1",
		"/run/media/player/Other",
		"-oignore_case,windows_names",
	})

	assessment := inspectNTFSFilenameCompatibilityAt(procRoot, testNTFSLibrary())
	if assessment.Compatibility != FilenameCompatibilityUnknown {
		t.Fatalf("compatibility = %q, want unknown", assessment.Compatibility)
	}
}

func TestInspectNTFSMountTreatsKernelNTFS3AsCompatible(t *testing.T) {
	library := testNTFSLibrary()
	library.Filesystem = "ntfs3"
	assessment := inspectNTFSFilenameCompatibilityAt(t.TempDir(), library)
	if assessment.Compatibility != FilenameCompatibilityCompatible || assessment.Driver != "ntfs3" {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}
}

func TestRepairNTFSMountRunsSafeSequence(t *testing.T) {
	library := testNTFSLibrary()
	assessments := []NTFSMountAssessment{
		{Compatibility: FilenameCompatibilityIncompatible, ForcedLowercase: true, CanRepair: true},
		{Compatibility: FilenameCompatibilityCompatible, Driver: "mount.ntfs"},
	}
	var calls [][]string
	dependencies := ntfsRepairDependencies{
		assess: func(SteamLibrary) NTFSMountAssessment {
			assessment := assessments[0]
			assessments = assessments[1:]
			return assessment
		},
		steamActive:  func() bool { return false },
		libraryReady: func(string) bool { return true },
		run: func(_ context.Context, name string, arguments ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, arguments...))
			return nil, nil
		},
		euid: func() int { return 1000 },
		egid: func() int { return 1001 },
	}

	result, err := repairNTFSFilenameCompatibilityWith(context.Background(), library, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Temporary || result.After.Compatibility != FilenameCompatibilityCompatible {
		t.Fatalf("unexpected result: %+v", result)
	}
	want := [][]string{
		{"udisksctl", "unmount", "-b", "/dev/sdb1"},
		{"udisksctl", "mount", "-b", "/dev/sdb1", "-t", "ntfs", "-o", "rw,noatime,uid=1000,gid=1001,umask=0022,windows_names,big_writes"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRepairNTFSMountRefusesSteamRace(t *testing.T) {
	called := false
	dependencies := repairTestDependencies(&called)
	dependencies.steamActive = func() bool { return true }
	_, err := repairNTFSFilenameCompatibilityWith(context.Background(), testNTFSLibrary(), dependencies)
	if err == nil || !strings.Contains(err.Error(), "Steam started again") {
		t.Fatalf("error = %v", err)
	}
	if called {
		t.Fatal("udisksctl was called while Steam was running")
	}
}

func TestRepairNTFSMountRestoresDefaultAfterMountFailure(t *testing.T) {
	library := testNTFSLibrary()
	var calls [][]string
	dependencies := repairTestDependencies(nil)
	dependencies.run = func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, arguments...))
		if len(calls) == 2 {
			return nil, errors.New("custom mount failed")
		}
		return nil, nil
	}
	_, err := repairNTFSFilenameCompatibilityWith(context.Background(), library, dependencies)
	if err == nil || !strings.Contains(err.Error(), "custom mount failed") {
		t.Fatalf("error = %v", err)
	}
	if len(calls) != 3 || !reflect.DeepEqual(calls[2], []string{"udisksctl", "mount", "-b", "/dev/sdb1"}) {
		t.Fatalf("default mount was not restored: %#v", calls)
	}
}

func TestRepairNTFSMountRestoresDefaultWhenVerificationFails(t *testing.T) {
	assessments := []NTFSMountAssessment{
		{Compatibility: FilenameCompatibilityIncompatible, ForcedLowercase: true, CanRepair: true},
		{Compatibility: FilenameCompatibilityIncompatible, ForcedLowercase: true, CanRepair: true},
	}
	var calls [][]string
	dependencies := repairTestDependencies(nil)
	dependencies.assess = func(SteamLibrary) NTFSMountAssessment {
		assessment := assessments[0]
		assessments = assessments[1:]
		return assessment
	}
	dependencies.run = func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, arguments...))
		return nil, nil
	}
	_, err := repairNTFSFilenameCompatibilityWith(context.Background(), testNTFSLibrary(), dependencies)
	if err == nil || !strings.Contains(err.Error(), "did not pass") {
		t.Fatalf("error = %v", err)
	}
	if len(calls) != 4 {
		t.Fatalf("calls = %#v", calls)
	}
	if !reflect.DeepEqual(calls[2], []string{"udisksctl", "unmount", "-b", "/dev/sdb1"}) ||
		!reflect.DeepEqual(calls[3], []string{"udisksctl", "mount", "-b", "/dev/sdb1"}) {
		t.Fatalf("unexpected compensation sequence: %#v", calls)
	}
}

func TestRepairNTFSMountRestoresDefaultWhenLibraryDoesNotReturn(t *testing.T) {
	assessments := []NTFSMountAssessment{
		{Compatibility: FilenameCompatibilityIncompatible, ForcedLowercase: true, CanRepair: true},
		{Compatibility: FilenameCompatibilityCompatible, Driver: "mount.ntfs"},
	}
	readyChecks := 0
	var calls [][]string
	dependencies := repairTestDependencies(nil)
	dependencies.assess = func(SteamLibrary) NTFSMountAssessment {
		assessment := assessments[0]
		assessments = assessments[1:]
		return assessment
	}
	dependencies.libraryReady = func(string) bool {
		readyChecks++
		return readyChecks == 1
	}
	dependencies.run = func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, arguments...))
		return nil, nil
	}
	_, err := repairNTFSFilenameCompatibilityWith(context.Background(), testNTFSLibrary(), dependencies)
	if err == nil || !strings.Contains(err.Error(), "did not pass") {
		t.Fatalf("error = %v", err)
	}
	if len(calls) != 4 || !reflect.DeepEqual(calls[3], []string{"udisksctl", "mount", "-b", "/dev/sdb1"}) {
		t.Fatalf("library disappearance did not restore the default mount: %#v", calls)
	}
}

func TestRepairNTFSMountNoopsWhenAlreadyCompatible(t *testing.T) {
	called := false
	dependencies := repairTestDependencies(&called)
	dependencies.assess = func(SteamLibrary) NTFSMountAssessment {
		return NTFSMountAssessment{Compatibility: FilenameCompatibilityCompatible}
	}
	result, err := repairNTFSFilenameCompatibilityWith(context.Background(), testNTFSLibrary(), dependencies)
	if err != nil {
		t.Fatal(err)
	}
	if called || result.After.Compatibility != FilenameCompatibilityCompatible {
		t.Fatalf("unexpected no-op result: %+v, called=%t", result, called)
	}
}

func TestRepairNTFSMountRejectsNonDeviceSource(t *testing.T) {
	library := testNTFSLibrary()
	library.Source = "/tmp/disk.img"
	_, err := repairNTFSFilenameCompatibilityWith(context.Background(), library, repairTestDependencies(nil))
	if err == nil || !strings.Contains(err.Error(), "block device") {
		t.Fatalf("error = %v", err)
	}
}

func repairTestDependencies(called *bool) ntfsRepairDependencies {
	return ntfsRepairDependencies{
		assess: func(SteamLibrary) NTFSMountAssessment {
			return NTFSMountAssessment{Compatibility: FilenameCompatibilityIncompatible, ForcedLowercase: true, CanRepair: true}
		},
		steamActive:  func() bool { return false },
		libraryReady: func(string) bool { return true },
		run: func(context.Context, string, ...string) ([]byte, error) {
			if called != nil {
				*called = true
			}
			return nil, nil
		},
		euid: func() int { return 1000 },
		egid: func() int { return 1000 },
	}
}

func testNTFSLibrary() SteamLibrary {
	return SteamLibrary{
		Path:       "/run/media/player/Games/SteamLibrary",
		MountPoint: "/run/media/player/Games",
		Source:     "/dev/sdb1",
		Filesystem: "fuseblk",
	}
}

func writeMountProcess(t *testing.T, procRoot, pid string, arguments []string) {
	t.Helper()
	directory := filepath.Join(procRoot, pid)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(strings.Join(arguments, "\x00") + "\x00")
	if err := os.WriteFile(filepath.Join(directory, "cmdline"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
