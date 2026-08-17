package plugins

import "context"

// FilenameCompatibility describes whether an NTFS mount preserves the
// filename spelling Steam installed. Some lowntfs-3g configurations expose
// every directory entry in lowercase, which can break games that key assets
// or integrity metadata by the original spelling.
type FilenameCompatibility string

const (
	FilenameCompatibilityCompatible   FilenameCompatibility = "compatible"
	FilenameCompatibilityIncompatible FilenameCompatibility = "incompatible"
	FilenameCompatibilityUnknown      FilenameCompatibility = "unknown"
)

// NTFSMountAssessment is a read-only description of one library's active
// mount. A session repair is deliberately separate from compatdata setup.
type NTFSMountAssessment struct {
	Compatibility   FilenameCompatibility
	Driver          string
	ForcedLowercase bool
	CanRepair       bool
	Reason          string
}

// NTFSSessionRepairResult describes a successful temporary remount. The
// operating system may restore its default mount policy after reboot.
type NTFSSessionRepairResult struct {
	Library    SteamLibrary
	Before     NTFSMountAssessment
	After      NTFSMountAssessment
	Temporary  bool
	MountPoint string
	Device     string
}

// InspectNTFSFilenameCompatibility reports the active filename semantics
// without changing the mount or probing it with temporary files.
func InspectNTFSFilenameCompatibility(library SteamLibrary) NTFSMountAssessment {
	return inspectNTFSFilenameCompatibility(library)
}

// RepairNTFSFilenameCompatibility remounts an affected NTFS volume for the
// current session. Platform implementations must never force-unmount.
func RepairNTFSFilenameCompatibility(ctx context.Context, library SteamLibrary) (NTFSSessionRepairResult, error) {
	return repairNTFSFilenameCompatibility(ctx, library)
}
