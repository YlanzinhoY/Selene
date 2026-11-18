package installer

import (
	"path/filepath"
	"strings"
	"testing"
)

// Excerpt of slsteam-moon v2.9 res/config.yaml around the Donate block.
const slsTemplateExcerpt = `DisableCloud: no

#Keep managed games on the latest build. Default "yes" (games auto-update).
AutoUpdateApps: yes

# Share short-lived manifest request codes for depots this account really owns.
# The wanted list is downloaded from the archive; only matching depots are sent.
# Set Enabled: no to disable both active requests and passive capture.
Donate:
  Enabled: yes
  Url: "https://manifest.luastools.xyz"
  IntervalSecs: 30
  WantedRefreshSecs: 300
  MaxMintsPerCycle: 25
  MinMintIntervalMs: 2000
  MaxMintsPerSession: 0 # 0 means no session cap

# Inject every storefront-advertised DLC into package 0. Keep disabled by
# default; content-backed DLC is still injected automatically.
InjectAllAdvertisedDlc: no
`

func TestSeedSLSConfigDisablesDonateInFreshConfig(t *testing.T) {
	root := t.TempDir()
	template := filepath.Join(root, "release", "res", "config.yaml")
	destination := filepath.Join(root, "config", "SLSsteam", "config.yaml")
	writeInstallerFile(t, template, slsTemplateExcerpt)

	if err := seedSLSConfig(template, destination); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(slsTemplateExcerpt, "Donate:\n  Enabled: yes\n", "Donate:\n  Enabled: no\n", 1)
	assertInstallerContent(t, destination, want)
}

func TestSeedSLSConfigAddsDonateOptOutToConfigWithoutIt(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "config", "SLSsteam", "config.yaml")
	existing := "PlayNotOwnedGames: yes\nAdditionalApps:\n  - 480\n"
	writeInstallerFile(t, destination, existing)

	if err := seedSLSConfig(filepath.Join(root, "missing-template.yaml"), destination); err != nil {
		t.Fatal(err)
	}
	assertInstallerContent(t, destination, existing+donateOptOutBlock)
}

func TestSeedSLSConfigKeepsExplicitDonateChoice(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "config", "SLSsteam", "config.yaml")
	existing := "PlayNotOwnedGames: yes\nDonate:\n  Enabled: yes\n"
	writeInstallerFile(t, destination, existing)

	if err := seedSLSConfig(filepath.Join(root, "missing-template.yaml"), destination); err != nil {
		t.Fatal(err)
	}
	assertInstallerContent(t, destination, existing)
}

func TestDisableDonate(t *testing.T) {
	for _, test := range []struct {
		name, input, want string
	}{
		{"inserts missing Enabled key", "Donate:\n  Url: x\nOther: 1\n", "Donate:\n  Enabled: no\n  Url: x\nOther: 1\n"},
		{"keeps trailing comment", "Donate:\n  Enabled: yes # opt\n", "Donate:\n  Enabled: no # opt\n"},
		{"ignores nested Enabled keys", "Donate:\n  Nested:\n    Enabled: yes\n  Enabled: true\n", "Donate:\n  Nested:\n    Enabled: yes\n  Enabled: no\n"},
		{"stops at the next top-level key", "Donate:\nEnabled: yes\n", "Donate:\n  Enabled: no\nEnabled: yes\n"},
		{"preserves CRLF line endings", "Donate:\r\n  Enabled: yes\r\n", "Donate:\r\n  Enabled: no\r\n"},
		{"leaves inline values alone", "Donate: {Enabled: yes}\n", "Donate: {Enabled: yes}\n"},
		{"ignores commented and nested Donate keys", "#Donate:\nA:\n  Donate: 1\n", "#Donate:\nA:\n  Donate: 1\n" + donateOptOutBlock},
		{"appends to a file without a final newline", "A: 1", "A: 1\n" + donateOptOutBlock},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := disableDonate(test.input); got != test.want {
				t.Fatalf("disableDonate(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}
