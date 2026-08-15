package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionFlagIsReservedForBootstrap(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(--version) code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "selene ") {
		t.Fatalf("version metadata missing: %q", stdout.String())
	}
}

func TestOperationalCommandsAreNotExposed(t *testing.T) {
	for _, arguments := range [][]string{{"doctor"}, {"install", "--yes"}, {"rollback", "--yes"}, {"uninstall", "--yes"}} {
		var stdout, stderr bytes.Buffer
		code := Run(arguments, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("Run(%v) code = %d, want 2", arguments, code)
		}
		if !strings.Contains(stderr.String(), "TUI-only") {
			t.Fatalf("Run(%v) error = %q", arguments, stderr.String())
		}
	}
}
