package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(help) code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "selene doctor") {
		t.Fatalf("help does not mention doctor: %q", stdout.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"moon"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("Run(unknown) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "comando desconhecido") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestDoctorJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor", "--json"}, &stdout, &stderr)
	if code != 0 && code != 1 {
		t.Fatalf("Run(doctor --json) code = %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"checks"`) {
		t.Fatalf("doctor output is not a report: %q", stdout.String())
	}
}
