//go:build !linux

package plugins

import (
	"context"
	"errors"
)

func inspectNTFSFilenameCompatibility(SteamLibrary) NTFSMountAssessment {
	return NTFSMountAssessment{
		Compatibility: FilenameCompatibilityUnknown,
		Reason:        "NTFS mount inspection is available only on Linux",
	}
}

func repairNTFSFilenameCompatibility(context.Context, SteamLibrary) (NTFSSessionRepairResult, error) {
	return NTFSSessionRepairResult{}, errors.New("NTFS session repair is available only on Linux")
}
