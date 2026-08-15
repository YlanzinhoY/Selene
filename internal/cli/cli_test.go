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

func TestCatalogJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"catalog", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(catalog --json) code = %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"revision": "2026-08-15-v2.8"`) {
		t.Fatalf("catalog output does not contain pinned revision: %q", stdout.String())
	}
}

func TestPlanJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"plan", "--json", "luatools"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(plan --json) code = %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"ready": false`) {
		t.Fatalf("plan should report current blockers: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"phase": "download"`) {
		t.Fatalf("plan does not contain download operations: %q", stdout.String())
	}
}

func TestFetchRejectsUnknownBundleBeforeNetwork(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"fetch", "missing"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("Run(fetch missing) code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "não encontrado") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}
