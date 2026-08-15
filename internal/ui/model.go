package ui

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/selene-linux/selene/internal/artifact"
	"github.com/selene-linux/selene/internal/catalog"
	"github.com/selene-linux/selene/internal/doctor"
	"github.com/selene-linux/selene/internal/installer"
	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/transaction"
)

type screen int

const (
	screenHome screen = iota
	screenDoctor
	screenPlan
	screenFetch
	screenInstallConfirm
	screenInstallResult
	screenRollbackConfirm
	screenRollbackResult
	screenAbout
)

type doctorMsg struct {
	report doctor.Report
}

type planMsg struct {
	plan planner.Plan
	err  error
}

type fetchMsg struct {
	results []artifact.Result
	err     error
}

type installPlanMsg struct {
	plan planner.Plan
	err  error
}

type installMsg struct {
	result installer.Result
	log    string
	err    error
}

type rollbackPreviewMsg struct {
	history []transaction.Journal
	err     error
}

type rollbackMsg struct {
	result installer.RollbackResult
	log    string
	err    error
}

type menuItem struct {
	title       string
	description string
}

type model struct {
	width      int
	height     int
	cursor     int
	screen     screen
	ctx        context.Context
	cancel     context.CancelFunc
	checking   bool
	mutating   bool
	activity   string
	spinner    spinner.Model
	viewport   viewport.Model
	report     *doctor.Report
	plan       *planner.Plan
	fetched    []artifact.Result
	installed  *installer.Result
	rolledBack *installer.RollbackResult
	history    []transaction.Journal
	log        string
	err        error
	items      []menuItem
}

var (
	moonColor   = lipgloss.Color("#C4B5FD")
	accentColor = lipgloss.Color("#7DD3FC")
	mutedColor  = lipgloss.AdaptiveColor{Light: "#64748B", Dark: "#94A3B8"}
	goodColor   = lipgloss.Color("#4ADE80")
	warnColor   = lipgloss.Color("#FACC15")
	errorColor  = lipgloss.Color("#FB7185")

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(moonColor)
	mutedStyle = lipgloss.NewStyle().Foreground(mutedColor)
	boxStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(moonColor).Padding(1, 2)
)

// Run starts the terminal interface.
func Run() error {
	m := newModel()
	defer m.cancel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newModel() model {
	ctx, cancel := context.WithCancel(context.Background())
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(accentColor)
	return model{
		ctx:      ctx,
		cancel:   cancel,
		spinner:  s,
		viewport: viewport.New(80, 15),
		items: []menuItem{
			{title: "Diagnosticar ambiente", description: "Verificar Linux, Steam, bibliotecas e Proton"},
			{title: "Planejar instalação", description: "Revisar hashes e destinos sem alterar arquivos"},
			{title: "Baixar e verificar", description: "Guardar os pacotes verificados no cache do Selene"},
			{title: "Instalar LuaTools", description: "Executar a fonte verificada com snapshot e rollback automático"},
			{title: "Desfazer instalação", description: "Restaurar o snapshot mais recente do Selene"},
			{title: "Sobre o Selene", description: "Conhecer a missão e o estado do projeto"},
			{title: "Sair", description: "Fechar a interface"},
		},
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeViewport()
		return m, nil
	case spinner.TickMsg:
		if !m.checking {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case doctorMsg:
		m.checking = false
		m.activity = ""
		m.report = &msg.report
		m.screen = screenDoctor
		m.refreshViewport()
		return m, nil
	case planMsg:
		m.checking = false
		m.activity = ""
		m.err = msg.err
		m.plan = &msg.plan
		m.screen = screenPlan
		m.refreshViewport()
		return m, nil
	case fetchMsg:
		m.checking = false
		m.activity = ""
		m.err = msg.err
		m.fetched = msg.results
		m.screen = screenFetch
		m.refreshViewport()
		return m, nil
	case installPlanMsg:
		m.checking = false
		m.activity = ""
		m.err = msg.err
		m.plan = &msg.plan
		m.screen = screenInstallConfirm
		m.refreshViewport()
		return m, nil
	case installMsg:
		m.checking = false
		m.mutating = false
		m.activity = ""
		m.err = msg.err
		m.installed = &msg.result
		m.log = msg.log
		m.screen = screenInstallResult
		m.refreshViewport()
		return m, nil
	case rollbackPreviewMsg:
		m.checking = false
		m.activity = ""
		m.err = msg.err
		m.history = msg.history
		m.screen = screenRollbackConfirm
		m.refreshViewport()
		return m, nil
	case rollbackMsg:
		m.checking = false
		m.mutating = false
		m.activity = ""
		m.err = msg.err
		m.rolledBack = &msg.result
		m.log = msg.log
		m.screen = screenRollbackResult
		m.refreshViewport()
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" || key == "q" {
			if m.mutating {
				return m, nil
			}
			m.cancel()
			return m, tea.Quit
		}
		if m.checking {
			return m, nil
		}

		switch m.screen {
		case screenHome:
			switch key {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.items)-1 {
					m.cursor++
				}
			case "enter", " ":
				switch m.cursor {
				case 0:
					m.checking = true
					m.activity = "Verificando o ambiente sem alterar nenhum arquivo..."
					return m, tea.Batch(m.spinner.Tick, runDoctor())
				case 1:
					m.checking = true
					m.activity = "Montando um plano verificável sem aplicar mudanças..."
					return m, tea.Batch(m.spinner.Tick, buildPlan())
				case 2:
					m.checking = true
					m.activity = "Baixando, conferindo SHA-256 e inspecionando os pacotes..."
					return m, tea.Batch(m.spinner.Tick, fetchArtifacts(m.ctx))
				case 3:
					m.checking = true
					m.activity = "Preparando a confirmação da instalação..."
					return m, tea.Batch(m.spinner.Tick, buildInstallPlan())
				case 4:
					m.checking = true
					m.activity = "Procurando snapshots que podem ser restaurados..."
					return m, tea.Batch(m.spinner.Tick, previewRollback())
				case 5:
					m.screen = screenAbout
				case 6:
					return m, tea.Quit
				}
			}
		case screenDoctor:
			switch key {
			case "esc", "backspace":
				m.screen = screenHome
				return m, nil
			case "r":
				m.checking = true
				m.activity = "Verificando o ambiente sem alterar nenhum arquivo..."
				return m, tea.Batch(m.spinner.Tick, runDoctor())
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenPlan:
			switch key {
			case "esc", "backspace":
				m.screen = screenHome
				return m, nil
			case "r":
				m.checking = true
				m.activity = "Atualizando o plano sem aplicar mudanças..."
				return m, tea.Batch(m.spinner.Tick, buildPlan())
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenFetch:
			switch key {
			case "esc", "backspace":
				m.screen = screenHome
				return m, nil
			case "r":
				m.checking = true
				m.activity = "Conferindo novamente os artefatos no cache..."
				return m, tea.Batch(m.spinner.Tick, fetchArtifacts(m.ctx))
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenInstallConfirm:
			switch key {
			case "esc", "backspace":
				m.screen = screenHome
				return m, nil
			case "i":
				if m.err == nil && m.plan != nil && m.plan.Ready {
					m.checking = true
					m.mutating = true
					m.activity = "Instalando com snapshot ativo. Não feche o Selene..."
					return m, tea.Batch(m.spinner.Tick, installBundle(m.ctx))
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenRollbackConfirm:
			switch key {
			case "esc", "backspace":
				m.screen = screenHome
				return m, nil
			case "d":
				if m.err == nil && latestRestorable(m.history) != nil {
					m.checking = true
					m.mutating = true
					m.activity = "Restaurando o snapshot. Não feche o Selene..."
					return m, tea.Batch(m.spinner.Tick, rollbackLatest(m.ctx))
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenInstallResult, screenRollbackResult:
			if key == "esc" || key == "backspace" {
				m.screen = screenHome
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenAbout:
			if key == "esc" || key == "backspace" {
				m.screen = screenHome
			}
		}
	}

	return m, nil
}

func (m model) View() string {
	contentWidth := m.width - 8
	if contentWidth < 28 {
		contentWidth = 28
	}
	if contentWidth > 88 {
		contentWidth = 88
	}

	header := titleStyle.Render("SELENE  ☾") + "\n" + mutedStyle.Render("LuaTools no Linux, sem rituais de terminal.")
	var body string
	if m.checking {
		body = fmt.Sprintf("%s  %s", m.spinner.View(), m.activity)
	} else {
		switch m.screen {
		case screenDoctor:
			body = m.viewport.View()
		case screenPlan:
			body = m.viewport.View()
		case screenFetch:
			body = m.viewport.View()
		case screenInstallConfirm, screenInstallResult, screenRollbackConfirm, screenRollbackResult:
			body = m.viewport.View()
		case screenAbout:
			body = aboutView()
		default:
			body = m.homeView()
		}
	}

	footerText := "↑/↓ navegar  •  enter selecionar  •  esc voltar  •  q sair"
	if m.screen == screenDoctor && !m.checking {
		footerText = "↑/↓ rolar  •  r verificar  •  esc voltar  •  q sair"
	} else if m.screen == screenPlan && !m.checking {
		footerText = "↑/↓ rolar  •  r recalcular  •  esc voltar  •  q sair"
	} else if m.screen == screenFetch && !m.checking {
		footerText = "↑/↓ rolar  •  r verificar cache  •  esc voltar  •  q sair"
	} else if m.screen == screenInstallConfirm && !m.checking {
		footerText = "↑/↓ rolar  •  i confirmar instalação  •  esc cancelar"
	} else if m.screen == screenRollbackConfirm && !m.checking {
		footerText = "↑/↓ rolar  •  d confirmar rollback  •  esc cancelar"
	} else if (m.screen == screenInstallResult || m.screen == screenRollbackResult) && !m.checking {
		footerText = "↑/↓ rolar  •  esc voltar  •  q sair"
	} else if m.mutating {
		footerText = "Transação em andamento • aguarde o commit ou rollback automático"
	}
	footer := mutedStyle.Render(footerText)
	inside := lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
	panel := boxStyle.Width(contentWidth).Render(inside)

	if m.width > 0 {
		return lipgloss.Place(m.width, max(m.height, lipgloss.Height(panel)), lipgloss.Center, lipgloss.Center, panel)
	}
	return panel
}

func (m model) homeView() string {
	var b strings.Builder
	for i, item := range m.items {
		cursor := "  "
		style := lipgloss.NewStyle()
		if i == m.cursor {
			cursor = "› "
			style = style.Bold(true).Foreground(accentColor)
		}
		fmt.Fprintf(&b, "%s%s\n", cursor, style.Render(item.title))
		fmt.Fprintf(&b, "    %s\n", mutedStyle.Render(item.description))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) doctorContent() string {
	if m.report == nil {
		return "Nenhum diagnóstico executado."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", titleStyle.Render("Diagnóstico do ambiente"))
	for _, check := range m.report.Checks {
		icon, style := statusPresentation(check.Status)
		fmt.Fprintf(&b, "%s %s\n", style.Render(icon), lipgloss.NewStyle().Bold(true).Render(check.Title))
		fmt.Fprintf(&b, "  %s\n", check.Summary)
		for _, detail := range check.Details {
			fmt.Fprintf(&b, "  %s\n", mutedStyle.Render("· "+detail))
		}
		b.WriteString("\n")
	}

	summary := m.report.Summary
	fmt.Fprintf(&b, "%s\n", mutedStyle.Render(fmt.Sprintf(
		"%d ok • %d aviso(s) • %d erro(s) • r verificar novamente",
		summary.OK, summary.Warnings, summary.Errors,
	)))
	return strings.TrimRight(b.String(), "\n")
}

func (m *model) resizeViewport() {
	width := m.width - 8
	if width < 28 {
		width = 28
	}
	if width > 88 {
		width = 88
	}
	height := m.height - 9
	if height < 6 {
		height = 6
	}
	m.viewport.Width = width
	m.viewport.Height = height
	m.refreshViewport()
}

func (m *model) refreshViewport() {
	var content string
	switch m.screen {
	case screenDoctor:
		if m.report == nil {
			content = "Nenhum diagnóstico executado."
		} else {
			content = m.doctorContent()
		}
	case screenPlan:
		content = m.planContent()
	case screenFetch:
		content = m.fetchContent()
	case screenInstallConfirm:
		content = m.installConfirmContent()
	case screenInstallResult:
		content = m.installResultContent()
	case screenRollbackConfirm:
		content = m.rollbackConfirmContent()
	case screenRollbackResult:
		content = m.rollbackResultContent()
	default:
		return
	}
	content = lipgloss.NewStyle().Width(m.viewport.Width).Render(content)
	m.viewport.SetContent(content)
	m.viewport.GotoTop()
}

func (m model) planContent() string {
	if m.err != nil {
		return titleStyle.Render("Plano de instalação") + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}
	if m.plan == nil {
		return "Nenhum plano gerado."
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("Plano de instalação · " + m.plan.BundleName))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Catálogo " + m.plan.CatalogRevision))
	b.WriteString("\n\n")
	if m.plan.Ready {
		b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Render("✓ Plano pronto para execução"))
	} else {
		b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Render("! Plano ainda não executável"))
		b.WriteString("\n")
		for _, blocker := range m.plan.Blockers {
			b.WriteString("  " + mutedStyle.Render("· "+blocker) + "\n")
		}
	}
	b.WriteString("\nOperações propostas:\n\n")
	for _, operation := range m.plan.Operations {
		component := ""
		if operation.Component != "" {
			component = " · " + operation.Component
		}
		fmt.Fprintf(&b, "%02d  %s%s\n", operation.Order,
			lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render(operation.Phase), component)
		b.WriteString("    " + operation.Action + "\n")
		if operation.Target != "" {
			b.WriteString("    " + mutedStyle.Render(operation.Target) + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(mutedStyle.Render("Nenhuma alteração foi aplicada. Pressione r para recalcular."))
	return strings.TrimRight(b.String(), "\n")
}

func (m model) fetchContent() string {
	if m.err != nil {
		return titleStyle.Render("Artefatos do LuaTools") + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}
	if len(m.fetched) == 0 {
		return "Nenhum artefato verificado."
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("Artefatos verificados"))
	b.WriteString("\n\n")
	for _, result := range m.fetched {
		origin := "baixado agora"
		if result.Cached {
			origin = "já estava no cache"
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(goodColor).Render("✓ " + result.Component))
		b.WriteString("  " + mutedStyle.Render(result.Version+" · "+origin) + "\n")
		name := strings.TrimPrefix(filepath.Base(result.Path), result.SHA256+"-")
		b.WriteString("  " + name + "\n")
		b.WriteString("  " + mutedStyle.Render("cache: "+filepath.Dir(result.Path)) + "\n")
		b.WriteString("  " + mutedStyle.Render("sha256:"+compactHash(result.SHA256)) + "\n\n")
	}
	b.WriteString(mutedStyle.Render("Os arquivos estão somente no cache; nada foi instalado."))
	return strings.TrimRight(b.String(), "\n")
}

func (m model) installConfirmContent() string {
	if m.err != nil {
		return titleStyle.Render("Confirmar instalação") + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}
	if m.plan == nil {
		return "Nenhum plano de instalação disponível."
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Confirmar instalação · " + m.plan.BundleName))
	b.WriteString("\n\n")
	if !m.plan.Ready {
		b.WriteString(errorStyle().Render("× Este ambiente ainda não pode executar a instalação."))
		b.WriteString("\n")
		for _, blocker := range m.plan.Blockers {
			b.WriteString("  " + mutedStyle.Render("· "+blocker) + "\n")
		}
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Bold(true).Render("Antes de continuar"))
	b.WriteString("\n\n")
	b.WriteString("• Steam e Lumen serão encerrados pelo instalador upstream.\n")
	b.WriteString("• O setup.sh veio do ZIP fixado no catálogo e conferido por SHA-256.\n")
	b.WriteString("• Sudo e alterações em /usr ficam bloqueados; o escopo é somente do usuário.\n")
	b.WriteString("• Um snapshot persistente é criado antes da primeira alteração.\n")
	b.WriteString("• Qualquer falha dispara rollback automático.\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("Pressione i para instalar."))
	b.WriteString("  " + mutedStyle.Render("Esc cancela sem alterar nada."))
	return b.String()
}

func (m model) installResultContent() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Resultado da instalação"))
	b.WriteString("\n\n")
	if m.err != nil {
		b.WriteString(errorStyle().Render("× " + m.err.Error()))
		b.WriteString("\n\n")
	} else if m.installed != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Bold(true).Render("✓ LuaTools instalado"))
		b.WriteString("\n")
		b.WriteString("Transação: " + m.installed.TransactionID + "\n")
		b.WriteString(mutedStyle.Render("O snapshot foi mantido; use Desfazer instalação se precisar."))
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(m.log) != "" {
		b.WriteString(mutedStyle.Render("Log da operação:"))
		b.WriteString("\n")
		b.WriteString(m.log)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) rollbackConfirmContent() string {
	if m.err != nil {
		return titleStyle.Render("Desfazer instalação") + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}
	journal := latestRestorable(m.history)
	if journal == nil {
		return titleStyle.Render("Desfazer instalação") + "\n\n" + mutedStyle.Render("Nenhum snapshot pendente de rollback foi encontrado.")
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Desfazer instalação"))
	b.WriteString("\n\n")
	b.WriteString("Snapshot: " + journal.ID + "\n")
	b.WriteString("Criado em: " + journal.CreatedAt.Local().Format("02/01/2006 15:04:05") + "\n")
	b.WriteString("Operação: " + journal.Description + "\n\n")
	b.WriteString("O Selene parará o guardian, restaurará arquivos, diretórios, atalhos e links do systemd exatamente como estavam.\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Bold(true).Render("Pressione d para desfazer."))
	b.WriteString("  " + mutedStyle.Render("Esc mantém a instalação atual."))
	return b.String()
}

func (m model) rollbackResultContent() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Resultado do rollback"))
	b.WriteString("\n\n")
	if m.err != nil {
		b.WriteString(errorStyle().Render("× " + m.err.Error()))
		b.WriteString("\n\n")
	} else if m.rolledBack != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Bold(true).Render("✓ Estado anterior restaurado"))
		b.WriteString("\n")
		b.WriteString("Transação: " + m.rolledBack.TransactionID + "\n\n")
	}
	if strings.TrimSpace(m.log) != "" {
		b.WriteString(mutedStyle.Render("Log da operação:"))
		b.WriteString("\n")
		b.WriteString(m.log)
	}
	return strings.TrimRight(b.String(), "\n")
}

func latestRestorable(history []transaction.Journal) *transaction.Journal {
	for index := range history {
		journal := &history[index]
		if journal.State != transaction.StateRolledBack && strings.HasPrefix(journal.Description, "install ") {
			return journal
		}
	}
	return nil
}

func compactHash(value string) string {
	if len(value) <= 24 {
		return value
	}
	return value[:16] + "…" + value[len(value)-8:]
}

func aboutView() string {
	return titleStyle.Render("Sobre o Selene") + "\n\n" +
		"Selene é um gerenciador comunitário e independente para tornar o\n" +
		"ecossistema LuaTools acessível no Linux. A prioridade é instalar,\n" +
		"diagnosticar, atualizar e desfazer mudanças com segurança.\n\n" +
		mutedStyle.Render("Estado atual: instalação user-only transacional com rollback persistente.")
}

func runDoctor() tea.Cmd {
	return func() tea.Msg {
		return doctorMsg{report: doctor.Run(context.Background())}
	}
}

func buildPlan() tea.Cmd {
	return func() tea.Msg {
		source, err := catalog.LoadStable()
		if err != nil {
			return planMsg{err: err}
		}
		env, err := planner.DetectEnvironment()
		if err != nil {
			return planMsg{err: err}
		}
		plan, err := planner.Build(source, "luatools", env)
		return planMsg{plan: plan, err: err}
	}
}

func fetchArtifacts(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		source, err := catalog.LoadStable()
		if err != nil {
			return fetchMsg{err: err}
		}
		bundle, ok := source.Bundle("luatools")
		if !ok {
			return fetchMsg{err: fmt.Errorf("bundle luatools não encontrado")}
		}
		components, err := source.OrderedComponents(bundle)
		if err != nil {
			return fetchMsg{err: err}
		}
		env, err := planner.DetectEnvironment()
		if err != nil {
			return fetchMsg{err: err}
		}
		cacheDir := filepath.Join(env.XDGCacheHome, "selene", "downloads")
		fetcher := artifact.NewFetcher()
		results := make([]artifact.Result, 0, len(components))
		for _, component := range components {
			result, err := fetcher.Fetch(ctx, component, cacheDir)
			if err != nil {
				return fetchMsg{results: results, err: err}
			}
			results = append(results, result)
		}
		return fetchMsg{results: results}
	}
}

func buildInstallPlan() tea.Cmd {
	return func() tea.Msg {
		source, err := catalog.LoadStable()
		if err != nil {
			return installPlanMsg{err: err}
		}
		env, err := planner.DetectEnvironment()
		if err != nil {
			return installPlanMsg{err: err}
		}
		plan, err := planner.Build(source, "luatools", env)
		return installPlanMsg{plan: plan, err: err}
	}
}

func installBundle(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		source, err := catalog.LoadStable()
		if err != nil {
			return installMsg{err: err}
		}
		env, err := planner.DetectEnvironment()
		if err != nil {
			return installMsg{err: err}
		}
		var output bytes.Buffer
		result, err := installer.Install(ctx, source, "luatools", env, installer.Options{Output: &output})
		return installMsg{result: result, log: output.String(), err: err}
	}
}

func previewRollback() tea.Cmd {
	return func() tea.Msg {
		env, err := planner.DetectEnvironment()
		if err != nil {
			return rollbackPreviewMsg{err: err}
		}
		history, err := installer.History(env)
		return rollbackPreviewMsg{history: history, err: err}
	}
}

func rollbackLatest(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		env, err := planner.DetectEnvironment()
		if err != nil {
			return rollbackMsg{err: err}
		}
		var output bytes.Buffer
		result, err := installer.Rollback(ctx, env, "", &output)
		return rollbackMsg{result: result, log: output.String(), err: err}
	}
}

func errorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(errorColor)
}

func statusPresentation(status doctor.Status) (string, lipgloss.Style) {
	switch status {
	case doctor.StatusOK:
		return "✓", lipgloss.NewStyle().Foreground(goodColor)
	case doctor.StatusWarning:
		return "!", lipgloss.NewStyle().Foreground(warnColor)
	case doctor.StatusError:
		return "×", lipgloss.NewStyle().Foreground(errorColor)
	default:
		return "·", lipgloss.NewStyle().Foreground(accentColor)
	}
}
