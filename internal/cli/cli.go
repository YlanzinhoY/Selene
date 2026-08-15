package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/selene-linux/selene/internal/artifact"
	"github.com/selene-linux/selene/internal/catalog"
	"github.com/selene-linux/selene/internal/doctor"
	"github.com/selene-linux/selene/internal/installer"
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
  selene fetch [bundle]  Baixa e verifica artefatos no cache
  selene install --yes   Instala com snapshot e rollback automático
  selene history         Lista transações e snapshots do Selene
  selene rollback --yes  Restaura a transação recuperável mais recente
  selene uninstall --yes Remove LuaTools, Lumen e slsteam-moon completamente
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
	case "fetch":
		return runFetch(args[1:], stdout, stderr)
	case "install":
		return runInstall(args[1:], stdout, stderr)
	case "history":
		return runHistory(args[1:], stdout, stderr)
	case "rollback":
		return runRollback(args[1:], stdout, stderr)
	case "uninstall":
		return runUninstall(args[1:], stdout, stderr)
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

func runInstall(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	confirmed := flags.Bool("yes", false, "confirma a instalação e o encerramento da Steam")
	jsonOutput := flags.Bool("json", false, "emite o resultado em JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(stderr, "selene install: informe no máximo um bundle")
		return 2
	}
	bundleID := "luatools"
	if flags.NArg() == 1 {
		bundleID = flags.Arg(0)
	}

	source, err := catalog.LoadStable()
	if err != nil {
		fmt.Fprintf(stderr, "selene install: %v\n", err)
		return 1
	}
	env, err := planner.DetectEnvironment()
	if err != nil {
		fmt.Fprintf(stderr, "selene install: %v\n", err)
		return 1
	}
	plan, err := planner.Build(source, bundleID, env)
	if err != nil {
		fmt.Fprintf(stderr, "selene install: %v\n", err)
		return 1
	}
	if !*confirmed {
		printPlan(stdout, plan)
		fmt.Fprintln(stdout, "\nA instalação encerrará Steam/Lumen, executará o setup verificado sem sudo e criará rollback.")
		fmt.Fprintf(stdout, "Para confirmar: selene install --yes %s\n", bundleID)
		return 2
	}
	if !plan.Ready {
		printPlan(stderr, plan)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	progress := stdout
	if *jsonOutput {
		progress = stderr
	}
	result, err := installer.Install(ctx, source, bundleID, env, installer.Options{Output: progress})
	if err != nil {
		fmt.Fprintf(stderr, "selene install: %v\n", err)
		return 1
	}
	if *jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "selene install: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "\nLuaTools instalado. Transação: %s\n", result.TransactionID)
	fmt.Fprintln(stdout, "Para desfazer: selene rollback --yes")
	return 0
}

func runHistory(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("history", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "emite as transações em JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "selene history: argumento inesperado %q\n", flags.Arg(0))
		return 2
	}
	env, err := planner.DetectEnvironment()
	if err != nil {
		fmt.Fprintf(stderr, "selene history: %v\n", err)
		return 1
	}
	history, err := installer.History(env)
	if err != nil {
		fmt.Fprintf(stderr, "selene history: %v\n", err)
		return 1
	}
	if *jsonOutput {
		if err := writeJSON(stdout, history); err != nil {
			fmt.Fprintf(stderr, "selene history: %v\n", err)
			return 1
		}
		return 0
	}
	if len(history) == 0 {
		fmt.Fprintln(stdout, "Nenhuma transação do Selene encontrada.")
		return 0
	}
	fmt.Fprintln(stdout, "Transações Selene (mais recente primeiro):")
	fmt.Fprintln(stdout)
	for _, journal := range history {
		fmt.Fprintf(stdout, "%-30s %-12s %s\n", journal.ID, journal.State, journal.Description)
		if journal.Error != "" {
			fmt.Fprintf(stdout, "  erro: %s\n", journal.Error)
		}
	}
	return 0
}

func runRollback(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("rollback", flag.ContinueOnError)
	flags.SetOutput(stderr)
	confirmed := flags.Bool("yes", false, "confirma a restauração do snapshot")
	jsonOutput := flags.Bool("json", false, "emite o resultado em JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(stderr, "selene rollback: informe no máximo um ID de transação")
		return 2
	}
	id := ""
	if flags.NArg() == 1 {
		id = flags.Arg(0)
	}
	if !*confirmed {
		target := "a transação recuperável mais recente"
		if id != "" && id != "latest" {
			target = "a transação " + id
		}
		fmt.Fprintf(stdout, "O rollback restaurará %s e encerrará os serviços do slsteam-moon.\n", target)
		fmt.Fprintln(stdout, "Para confirmar: selene rollback --yes"+formatOptionalID(id))
		return 2
	}
	env, err := planner.DetectEnvironment()
	if err != nil {
		fmt.Fprintf(stderr, "selene rollback: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	progress := stdout
	if *jsonOutput {
		progress = stderr
	}
	result, err := installer.Rollback(ctx, env, id, progress)
	if err != nil {
		fmt.Fprintf(stderr, "selene rollback: %v\n", err)
		return 1
	}
	if *jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "selene rollback: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "Estado anterior restaurado pela transação %s.\n", result.TransactionID)
	return 0
}

func runUninstall(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(stderr)
	confirmed := flags.Bool("yes", false, "confirma a remoção completa do stack LuaTools")
	jsonOutput := flags.Bool("json", false, "emite o resultado em JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "selene uninstall: argumento inesperado %q\n", flags.Arg(0))
		return 2
	}
	env, err := planner.DetectEnvironment()
	if err != nil {
		fmt.Fprintf(stderr, "selene uninstall: %v\n", err)
		return 1
	}
	preview, err := installer.PreviewUninstall(env)
	if err != nil {
		fmt.Fprintf(stderr, "selene uninstall: %v\n", err)
		return 1
	}
	if !*confirmed {
		fmt.Fprintln(stdout, "A remoção completa apaga LuaTools, Lumen, slsteam-moon, configurações e integrações da Steam no seu usuário.")
		fmt.Fprintln(stdout, "Seus jogos, a Steam, o executável do Selene, o cache e os snapshots não serão apagados.")
		if !preview.Detected {
			fmt.Fprintln(stdout, "\nNenhum vestígio gerenciado foi detectado no momento.")
		} else {
			fmt.Fprintln(stdout, "\nVestígios detectados:")
			for _, trace := range preview.Traces {
				fmt.Fprintln(stdout, "  - "+trace)
			}
		}
		fmt.Fprintln(stdout, "\nPara confirmar: selene uninstall --yes")
		return 2
	}

	source, err := catalog.LoadStable()
	if err != nil {
		fmt.Fprintf(stderr, "selene uninstall: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	progress := stdout
	if *jsonOutput {
		progress = stderr
	}
	result, err := installer.Uninstall(ctx, source, env, installer.Options{Output: progress})
	if err != nil {
		fmt.Fprintf(stderr, "selene uninstall: %v\n", err)
		return 1
	}
	if *jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "selene uninstall: %v\n", err)
			return 1
		}
		return 0
	}
	if !result.Removed {
		fmt.Fprintln(stdout, "Nenhuma instalação gerenciada foi encontrada; nada foi alterado.")
		return 0
	}
	fmt.Fprintf(stdout, "LuaTools, Lumen e slsteam-moon removidos. Transação de segurança: %s\n", result.TransactionID)
	return 0
}

func formatOptionalID(id string) string {
	if id == "" || id == "latest" {
		return ""
	}
	return " " + id
}

func runFetch(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("fetch", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "emite os artefatos verificados em JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(stderr, "selene fetch: informe no máximo um bundle")
		return 2
	}
	bundleID := "luatools"
	if flags.NArg() == 1 {
		bundleID = flags.Arg(0)
	}

	source, err := catalog.LoadStable()
	if err != nil {
		fmt.Fprintf(stderr, "selene fetch: %v\n", err)
		return 1
	}
	bundle, ok := source.Bundle(bundleID)
	if !ok {
		fmt.Fprintf(stderr, "selene fetch: bundle %q não encontrado\n", bundleID)
		return 1
	}
	components, err := source.OrderedComponents(bundle)
	if err != nil {
		fmt.Fprintf(stderr, "selene fetch: %v\n", err)
		return 1
	}
	env, err := planner.DetectEnvironment()
	if err != nil {
		fmt.Fprintf(stderr, "selene fetch: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	cacheDir := filepath.Join(env.XDGCacheHome, "selene", "downloads")
	fetcher := artifact.NewFetcher()
	results := make([]artifact.Result, 0, len(components))
	for _, component := range components {
		result, err := fetcher.Fetch(ctx, component, cacheDir)
		if err != nil {
			fmt.Fprintf(stderr, "selene fetch: %v\n", err)
			return 1
		}
		results = append(results, result)
	}

	if *jsonOutput {
		if err := writeJSON(stdout, results); err != nil {
			fmt.Fprintf(stderr, "selene fetch: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "Artefatos verificados — %s\n\n", bundle.Name)
	for _, result := range results {
		origin := "baixado"
		if result.Cached {
			origin = "cache"
		}
		fmt.Fprintf(stdout, "[OK] %-18s %-8s %d bytes\n", result.Component, origin, result.Size)
		fmt.Fprintf(stdout, "     %s\n", result.Path)
	}
	fmt.Fprintln(stdout, "\nNenhum artefato foi instalado.")
	return 0
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
	fmt.Fprintf(w, "Detalhes da instalação — %s\n", plan.BundleName)
	fmt.Fprintf(w, "Catálogo: %s\n", plan.CatalogRevision)
	fmt.Fprintf(w, "Destino: %s/%s\n\n", plan.Environment.OS, plan.Environment.Arch)
	if plan.Ready {
		fmt.Fprintln(w, "Status: pronto para execução")
	} else {
		fmt.Fprintln(w, "Status: prévia disponível; existem bloqueios para instalar")
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
