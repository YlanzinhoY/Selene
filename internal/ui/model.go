package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/selene-linux/selene/internal/doctor"
)

type screen int

const (
	screenHome screen = iota
	screenDoctor
	screenAbout
)

type doctorMsg struct {
	report doctor.Report
}

type menuItem struct {
	title       string
	description string
}

type model struct {
	width    int
	height   int
	cursor   int
	screen   screen
	checking bool
	spinner  spinner.Model
	viewport viewport.Model
	report   *doctor.Report
	items    []menuItem
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
	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newModel() model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(accentColor)
	return model{
		spinner:  s,
		viewport: viewport.New(80, 15),
		items: []menuItem{
			{title: "Diagnosticar ambiente", description: "Verificar Linux, Steam, bibliotecas e Proton"},
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
		m.report = &msg.report
		m.screen = screenDoctor
		m.refreshViewport()
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" || key == "q" {
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
					return m, tea.Batch(m.spinner.Tick, runDoctor())
				case 1:
					m.screen = screenAbout
				case 2:
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
				return m, tea.Batch(m.spinner.Tick, runDoctor())
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
		body = fmt.Sprintf("%s  Verificando o ambiente sem alterar nenhum arquivo...", m.spinner.View())
	} else {
		switch m.screen {
		case screenDoctor:
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
	if m.report == nil {
		m.viewport.SetContent("Nenhum diagnóstico executado.")
		return
	}
	content := lipgloss.NewStyle().Width(m.viewport.Width).Render(m.doctorContent())
	m.viewport.SetContent(content)
	m.viewport.GotoTop()
}

func aboutView() string {
	return titleStyle.Render("Sobre o Selene") + "\n\n" +
		"Selene é um gerenciador comunitário e independente para tornar o\n" +
		"ecossistema LuaTools acessível no Linux. A prioridade é instalar,\n" +
		"diagnosticar, atualizar e desfazer mudanças com segurança.\n\n" +
		mutedStyle.Render("Estado atual: fundação e diagnóstico somente leitura.")
}

func runDoctor() tea.Cmd {
	return func() tea.Msg {
		return doctorMsg{report: doctor.Run(context.Background())}
	}
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
