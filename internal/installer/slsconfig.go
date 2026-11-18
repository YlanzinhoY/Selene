package installer

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// SLSsteam v2.9 added "Donate", which shares manifest request codes for depots
// the account owns with an external archive. It is enabled both in the shipped
// config template and in code when config.yaml has no Donate section, so an
// upgrade would silently opt existing users in. Selene keeps it off unless the
// user's config.yaml already configures it.

const donateOptOutBlock = `
# Added by Selene: SLSsteam shares manifest request codes for owned depots
# with an external archive unless this is disabled. Set Enabled: yes to opt in.
Donate:
  Enabled: no
`

var (
	donateKey    = regexp.MustCompile(`^Donate[ \t]*:`)
	donateHeader = regexp.MustCompile(`^Donate:[ \t]*(#.*)?$`)
	enabledEntry = regexp.MustCompile(`^([ \t]+)Enabled[ \t]*:[^#]*?([ \t]*#.*)?$`)
)

// seedSLSConfig creates config.yaml from the release template when it is
// missing, with Donate disabled. An existing config is only touched when it
// has no Donate section, which SLSsteam would otherwise treat as enabled.
func seedSLSConfig(source, destination string) error {
	info, err := os.Lstat(destination)
	if err == nil {
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(destination)
		if err != nil {
			return fmt.Errorf("read SLSsteam config: %w", err)
		}
		if hasDonateKey(string(data)) {
			return nil
		}
		return writeFileAtomic(destination, strings.NewReader(disableDonate(string(data))), info.Mode())
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	templateInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("inspect SLSsteam config template: %w", err)
	}
	template, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read SLSsteam config template: %w", err)
	}
	return writeFileAtomic(destination, strings.NewReader(disableDonate(string(template))), templateInfo.Mode())
}

func hasDonateKey(config string) bool {
	for line := range strings.SplitSeq(config, "\n") {
		if donateKey.MatchString(line) {
			return true
		}
	}
	return false
}

// disableDonate returns config with the top-level Donate block's Enabled key
// set to no, inserting the key when the block lacks it. A config without a
// Donate key gets an opt-out block appended; a Donate key with an inline value
// is left untouched because its shape is unknown.
func disableDonate(config string) string {
	lines := strings.Split(config, "\n")
	for i, line := range lines {
		if !donateKey.MatchString(line) {
			continue
		}
		if !donateHeader.MatchString(strings.TrimSuffix(line, "\r")) {
			return config
		}
		childIndent := ""
		for j := i + 1; j < len(lines); j++ {
			current := strings.TrimSuffix(lines[j], "\r")
			trimmed := strings.TrimSpace(current)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			indent := current[:len(current)-len(strings.TrimLeft(current, " \t"))]
			if indent == "" {
				break
			}
			if childIndent == "" {
				childIndent = indent
			}
			if indent != childIndent {
				continue
			}
			if match := enabledEntry.FindStringSubmatch(current); match != nil {
				lines[j] = match[1] + "Enabled: no" + match[2] + lines[j][len(current):]
				return strings.Join(lines, "\n")
			}
		}
		if childIndent == "" {
			childIndent = "  "
		}
		inserted := append([]string{}, lines[:i+1]...)
		inserted = append(inserted, childIndent+"Enabled: no")
		return strings.Join(append(inserted, lines[i+1:]...), "\n")
	}
	return strings.TrimRight(config, "\n") + "\n" + donateOptOutBlock
}
