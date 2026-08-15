package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/selene-linux/selene/internal/doctor"
	"github.com/selene-linux/selene/internal/ui"
	"github.com/selene-linux/selene/internal/version"
)

const usage = `Selene — LuaTools no Linux, sem rituais de terminal.

Uso:
  selene                 Abre a interface interativa
  selene doctor          Diagnostica Linux, Steam e Proton
  selene doctor --json   Emite o diagnóstico em JSON
  selene version         Exibe a versão
  selene help            Exibe esta ajuda
`

// Run executes the command and returns a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		if err := ui.Run(); err != nil {
			fmt.Fprintf(stderr, "selene: não foi possível abrir a interface: %v\n", err)
			return 1
		}
		return 0
	}

	switch args[0] {
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "selene %s (commit %s, build %s)\n", version.Version, version.Commit, version.Date)
		return 0
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "selene: comando desconhecido %q\n\n%s", args[0], usage)
		return 2
	}
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "emite o relatório em JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "selene doctor: argumento inesperado %q\n", flags.Arg(0))
		return 2
	}

	report := doctor.Run(context.Background())
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(stderr, "selene doctor: %v\n", err)
			return 1
		}
	} else {
		printReport(stdout, report)
	}

	if report.Summary.Errors > 0 {
		return 1
	}
	return 0
}

func printReport(w io.Writer, report doctor.Report) {
	fmt.Fprintf(w, "Selene Doctor — %s/%s\n\n", report.OS, report.Arch)
	for _, check := range report.Checks {
		fmt.Fprintf(w, "[%s] %s: %s\n", statusLabel(check.Status), check.Title, check.Summary)
		for _, detail := range check.Details {
			fmt.Fprintf(w, "     %s\n", detail)
		}
	}
	fmt.Fprintf(w, "\nResultado: %d ok, %d aviso(s), %d erro(s), %d informativo(s).\n",
		report.Summary.OK, report.Summary.Warnings, report.Summary.Errors, report.Summary.Info)
}

func statusLabel(status doctor.Status) string {
	switch status {
	case doctor.StatusOK:
		return "OK"
	case doctor.StatusWarning:
		return "AVISO"
	case doctor.StatusError:
		return "ERRO"
	default:
		return strings.ToUpper(string(status))
	}
}
