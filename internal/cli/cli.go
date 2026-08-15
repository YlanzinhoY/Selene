package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/selene-linux/selene/internal/catalog"
	"github.com/selene-linux/selene/internal/doctor"
	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/ui"
	"github.com/selene-linux/selene/internal/version"
)

const usage = `Selene — LuaTools no Linux, sem rituais de terminal.

Uso:
  selene                 Abre a interface interativa
  selene doctor          Diagnostica Linux, Steam e Proton
  selene doctor --json   Emite o diagnóstico em JSON
  selene catalog         Lista bundles e componentes verificados
  selene plan [bundle]   Mostra todas as alterações sem aplicá-las
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
	case "catalog":
		return runCatalog(args[1:], stdout, stderr)
	case "plan":
		return runPlan(args[1:], stdout, stderr)
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

func runCatalog(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "emite o catálogo em JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "selene catalog: argumento inesperado %q\n", flags.Arg(0))
		return 2
	}

	source, err := catalog.LoadStable()
	if err != nil {
		fmt.Fprintf(stderr, "selene catalog: %v\n", err)
		return 1
	}
	if *jsonOutput {
		if err := writeJSON(stdout, source); err != nil {
			fmt.Fprintf(stderr, "selene catalog: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintf(stdout, "Catálogo Selene — %s (%s)\n\n", source.Revision, source.Channel)
	for _, bundle := range source.Bundles {
		fmt.Fprintf(stdout, "Bundle %s: %s\n", bundle.ID, bundle.Name)
		fmt.Fprintf(stdout, "  %s\n", bundle.Description)
		fmt.Fprintf(stdout, "  Componentes: %s\n\n", strings.Join(bundle.Components, ", "))
	}
	fmt.Fprintln(stdout, "Componentes verificados:")
	for _, component := range source.Components {
		optional := ""
		if component.Optional {
			optional = " (opcional)"
		}
		fmt.Fprintf(stdout, "  %-20s %-12s %s%s\n", component.ID, component.Version, component.Artifact.Name, optional)
		fmt.Fprintf(stdout, "  %-20s sha256:%s\n", "", component.Artifact.SHA256)
	}
	return 0
}

func runPlan(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "emite o plano em JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(stderr, "selene plan: informe no máximo um bundle")
		return 2
	}
	bundleID := "luatools"
	if flags.NArg() == 1 {
		bundleID = flags.Arg(0)
	}

	source, err := catalog.LoadStable()
	if err != nil {
		fmt.Fprintf(stderr, "selene plan: %v\n", err)
		return 1
	}
	env, err := planner.DetectEnvironment()
	if err != nil {
		fmt.Fprintf(stderr, "selene plan: %v\n", err)
		return 1
	}
	plan, err := planner.Build(source, bundleID, env)
	if err != nil {
		fmt.Fprintf(stderr, "selene plan: %v\n", err)
		return 1
	}
	if *jsonOutput {
		if err := writeJSON(stdout, plan); err != nil {
			fmt.Fprintf(stderr, "selene plan: %v\n", err)
			return 1
		}
		return 0
	}
	printPlan(stdout, plan)
	return 0
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
		if err := writeJSON(stdout, report); err != nil {
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

func printPlan(w io.Writer, plan planner.Plan) {
	fmt.Fprintf(w, "Plano Selene — %s\n", plan.BundleName)
	fmt.Fprintf(w, "Catálogo: %s\n", plan.CatalogRevision)
	fmt.Fprintf(w, "Destino: %s/%s\n\n", plan.Environment.OS, plan.Environment.Arch)
	if plan.Ready {
		fmt.Fprintln(w, "Status: pronto para execução")
	} else {
		fmt.Fprintln(w, "Status: somente planejamento; existem bloqueios")
		for _, blocker := range plan.Blockers {
			fmt.Fprintf(w, "  ! %s\n", blocker)
		}
	}
	fmt.Fprintln(w, "\nOperações propostas:")
	for _, operation := range plan.Operations {
		component := ""
		if operation.Component != "" {
			component = " [" + operation.Component + "]"
		}
		fmt.Fprintf(w, "%2d. %-10s%s %s\n", operation.Order, operation.Phase, component, operation.Action)
		if operation.Target != "" {
			fmt.Fprintf(w, "    destino: %s\n", operation.Target)
		}
	}
	fmt.Fprintln(w, "\nNenhuma alteração foi aplicada.")
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
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
