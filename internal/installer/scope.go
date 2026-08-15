package installer

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/transaction"
)

func userTransactionScope(env planner.Environment) ([]transaction.Target, []transaction.Pattern, error) {
	for name, value := range map[string]string{
		"HOME": env.Home, "XDG_DATA_HOME": env.XDGDataHome,
		"XDG_CONFIG_HOME": env.XDGConfigHome, "XDG_STATE_HOME": env.XDGStateHome,
	} {
		if value == "" || !filepath.IsAbs(value) {
			return nil, nil, fmt.Errorf("%s must be an absolute path", name)
		}
	}

	unitDir := filepath.Join(env.XDGConfigHome, "systemd", "user")
	stackData := stackDataHome(env)
	targets := []transaction.Target{
		{Path: filepath.Join(stackData, "SLSsteam"), Recursive: true},
		{Path: filepath.Join(stackData, "Lumen"), Recursive: true},
		{Path: filepath.Join(env.XDGConfigHome, "SLSsteam"), Recursive: true},
		{Path: filepath.Join(env.XDGStateHome, "slsteam-moon"), Recursive: true},
		{Path: filepath.Join(env.Home, ".bashrc")},
		{Path: filepath.Join(env.Home, ".zshrc")},
		{Path: filepath.Join(env.Home, ".profile")},
		{Path: filepath.Join(unitDir, "slsteam-desktop-guardian.service")},
		{Path: filepath.Join(unitDir, "slsteam-desktop-guardian.path")},
		{Path: filepath.Join(unitDir, "slsteam-desktop-guardian.timer")},
		{Path: filepath.Join(unitDir, "default.target.wants", "slsteam-desktop-guardian.path")},
		{Path: filepath.Join(unitDir, "timers.target.wants", "slsteam-desktop-guardian.timer")},
		{Path: filepath.Join(env.XDGDataHome, "applications", "mimeinfo.cache")},
	}

	desktop := resolveDesktopDir(env)
	directories := []string{
		filepath.Join(env.XDGDataHome, "applications"),
		filepath.Join(env.XDGConfigHome, "autostart"),
		desktop,
	}
	var patterns []transaction.Pattern
	for _, directory := range directories {
		patterns = append(patterns,
			transaction.Pattern{Glob: filepath.Join(directory, "*steam*.desktop")},
			transaction.Pattern{Glob: filepath.Join(directory, "*steam*.desktop.slssteam-backup")},
			transaction.Pattern{Glob: filepath.Join(directory, "*steam*.desktop.slsteam-bak")},
		)
	}
	patterns = append(patterns, transaction.Pattern{
		Glob: filepath.Join(unitDir, "app-*@autostart.service.d", "slsteam-guardian.conf"),
	})
	patterns = append(patterns,
		transaction.Pattern{Glob: filepath.Join(stackData, ".selene-Lumen-stage-*"), Recursive: true},
		transaction.Pattern{Glob: filepath.Join(stackData, ".selene-Lumen-previous-*"), Recursive: true},
	)
	return targets, patterns, nil
}

func validateUserScope(env planner.Environment) error {
	critical := []string{
		filepath.Join(stackDataHome(env), "SLSsteam"),
		filepath.Join(stackDataHome(env), "Lumen"),
		filepath.Join(env.XDGConfigHome, "SLSsteam"),
		filepath.Join(env.Home, ".bashrc"),
		filepath.Join(env.Home, ".zshrc"),
		filepath.Join(env.Home, ".profile"),
	}
	for _, path := range critical {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing user-only install because %s is a symbolic link", path)
		}
	}
	return nil
}

// The current upstream stack intentionally resolves its runtime from
// ~/.local/share, independently of XDG_DATA_HOME. Keep this explicit so the
// plan, transaction scope and installed layout cannot drift apart.
func stackDataHome(env planner.Environment) string {
	return filepath.Join(env.Home, ".local", "share")
}

func resolveDesktopDir(env planner.Environment) string {
	fallback := filepath.Join(env.Home, "Desktop")
	file, err := os.Open(filepath.Join(env.XDGConfigHome, "user-dirs.dirs"))
	if err != nil {
		return fallback
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "XDG_DESKTOP_DIR=") {
			continue
		}
		value := strings.Trim(strings.TrimPrefix(line, "XDG_DESKTOP_DIR="), `"'`)
		switch {
		case value == "$HOME" || value == "${HOME}":
			return env.Home
		case strings.HasPrefix(value, "$HOME/"):
			return filepath.Join(env.Home, filepath.FromSlash(strings.TrimPrefix(value, "$HOME/")))
		case strings.HasPrefix(value, "${HOME}/"):
			return filepath.Join(env.Home, filepath.FromSlash(strings.TrimPrefix(value, "${HOME}/")))
		case filepath.IsAbs(value):
			return filepath.Clean(value)
		}
	}
	return fallback
}
