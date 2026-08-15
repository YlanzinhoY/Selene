package cli

import (
	"fmt"
	"io"

	"github.com/selene-linux/selene/internal/ui"
	"github.com/selene-linux/selene/internal/version"
)

// Run keeps Selene TUI-first. Operational subcommands are intentionally not
// exposed; --version remains available for the verified bootstrap self-test.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintf(stdout, "selene %s (commit %s, build %s)\n", version.Version, version.Commit, version.Date)
		return 0
	}
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Selene is TUI-only. Run selene without arguments.")
		return 2
	}
	if err := ui.Run(); err != nil {
		fmt.Fprintf(stderr, "selene: could not open the interface: %v\n", err)
		return 1
	}
	return 0
}
