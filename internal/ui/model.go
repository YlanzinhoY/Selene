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
	"github.com/selene-linux/selene/internal/plugins"
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
	screenUninstallConfirm
	screenUninstallResult
	screenPlugins
	screenSteamLibrary
	screenSteamLibraryConfirm
	screenSteamLibraryRemoveConfirm
	screenSteamLibraryResult
	screenPlatformAssetOverride
	screenPlatformAssetOverrideDetails
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

type uninstallPreviewMsg struct {
	preview installer.UninstallPreview
	err     error
}

type uninstallMsg struct {
	result installer.UninstallResult
	log    string
	err    error
}

type steamLibrariesMsg struct {
	libraries []plugins.SteamLibrary
	links     []plugins.Link
	dataHome  string
	err       error
}

type steamLinkMsg struct {
	result  plugins.Result
	removed bool
	err     error
}

type steamGamesMsg struct {
	games []plugins.SteamGame
	err   error
}

type assetOverrideAnalysisMsg struct {
	analysis plugins.AssetOverrideAnalysis
	err      error
}

type menuItem struct {
	title       string
	description string
}

type steamEntryKind int

const (
	steamEntryLibrary steamEntryKind = iota
	steamEntryLink
)

type steamEntry struct {
	kind    steamEntryKind
	library plugins.SteamLibrary
	link    plugins.Link
}

type model struct {
	width         int
	height        int
	cursor        int
	pluginCursor  int
	steamCursor   int
	gameCursor    int
	screen        screen
	ctx           context.Context
	cancel        context.CancelFunc
	checking      bool
	mutating      bool
	activity      string
	spinner       spinner.Model
	viewport      viewport.Model
	report        *doctor.Report
	plan          *planner.Plan
	fetched       []artifact.Result
	installed     *installer.Result
	rolledBack    *installer.RollbackResult
	removal       *installer.UninstallPreview
	uninstalled   *installer.UninstallResult
	history       []transaction.Journal
	log           string
	err           error
	items         []menuItem
	pluginItems   []menuItem
	steamEntries  []steamEntry
	selectedSteam steamEntry
	steamDataHome string
	steamResult   *plugins.Result
	steamRemoved  bool
	steamGames    []plugins.SteamGame
	selectedGame  plugins.SteamGame
	gameAnalysis  *plugins.AssetOverrideAnalysis
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
	boxStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(moonColor).Padding(1, 4)
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
		ctx:         ctx,
		cancel:      cancel,
		spinner:     s,
		viewport:    viewport.New(80, 15),
		items:       defaultMenuItems(),
		pluginItems: defaultPluginItems(),
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
	case uninstallPreviewMsg:
		m.checking = false
		m.activity = ""
		m.err = msg.err
		m.removal = &msg.preview
		m.screen = screenUninstallConfirm
		m.refreshViewport()
		return m, nil
	case uninstallMsg:
		m.checking = false
		m.mutating = false
		m.activity = ""
		m.err = msg.err
		m.uninstalled = &msg.result
		m.log = msg.log
		m.screen = screenUninstallResult
		m.refreshViewport()
		return m, nil
	case steamLibrariesMsg:
		m.checking = false
		m.activity = ""
		m.err = msg.err
		m.steamEntries = makeSteamEntries(msg.libraries, msg.links)
		m.steamDataHome = msg.dataHome
		m.steamCursor = 0
		m.screen = screenSteamLibrary
		m.refreshViewport()
		return m, nil
	case steamLinkMsg:
		m.checking = false
		m.mutating = false
		m.activity = ""
		m.err = msg.err
		m.steamResult = &msg.result
		m.steamRemoved = msg.removed
		m.screen = screenSteamLibraryResult
		m.refreshViewport()
		return m, nil
	case steamGamesMsg:
		m.checking = false
		m.activity = ""
		m.err = msg.err
		m.steamGames = msg.games
		m.gameCursor = 0
		m.screen = screenPlatformAssetOverride
		m.refreshViewport()
		return m, nil
	case assetOverrideAnalysisMsg:
		m.checking = false
		m.activity = ""
		m.err = msg.err
		m.gameAnalysis = &msg.analysis
		m.screen = screenPlatformAssetOverrideDetails
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
					m.activity = textActivityDoctor
					return m, tea.Batch(m.spinner.Tick, runDoctor())
				case 1:
					m.checking = true
					m.activity = textActivityDetails
					return m, tea.Batch(m.spinner.Tick, buildPlan())
				case 2:
					m.checking = true
					m.activity = textActivityFetch
					return m, tea.Batch(m.spinner.Tick, fetchArtifacts(m.ctx))
				case 3:
					m.checking = true
					m.activity = textActivityInstallPreview
					return m, tea.Batch(m.spinner.Tick, buildInstallPlan())
				case 4:
					m.checking = true
					m.activity = textActivityRollbackPreview
					return m, tea.Batch(m.spinner.Tick, previewRollback())
				case 5:
					m.checking = true
					m.activity = textActivityRemovePreview
					return m, tea.Batch(m.spinner.Tick, previewUninstall())
				case 6:
					m.screen = screenPlugins
				case 7:
					m.screen = screenAbout
				case 8:
					return m, tea.Quit
				}
			}
		case screenPlugins:
			switch key {
			case "esc", "backspace":
				m.screen = screenHome
				return m, nil
			case "up", "k":
				if m.pluginCursor > 0 {
					m.pluginCursor--
				}
			case "down", "j":
				if m.pluginCursor < len(m.pluginItems)-1 {
					m.pluginCursor++
				}
			case "enter", " ":
				switch m.pluginCursor {
				case 0:
					m.checking = true
					m.activity = textActivitySteamLibraries
					return m, tea.Batch(m.spinner.Tick, scanSteamLibraries())
				case 1:
					m.checking = true
					m.activity = textActivitySteamGames
					return m, tea.Batch(m.spinner.Tick, scanSteamGames())
				}
			}
		case screenSteamLibrary:
			switch key {
			case "esc", "backspace":
				m.screen = screenPlugins
				return m, nil
			case "up", "k":
				if m.steamCursor > 0 {
					m.steamCursor--
					m.refreshViewport()
				}
			case "down", "j":
				if m.steamCursor < len(m.steamEntries)-1 {
					m.steamCursor++
					m.refreshViewport()
				}
			case "enter", " ":
				if entry, ok := m.currentSteamEntry(); ok && entry.kind == steamEntryLibrary && m.err == nil {
					m.selectedSteam = entry
					m.screen = screenSteamLibraryConfirm
					m.refreshViewport()
				}
			case "r":
				if entry, ok := m.currentSteamEntry(); ok && entry.kind == steamEntryLink && m.err == nil {
					m.selectedSteam = entry
					m.screen = screenSteamLibraryRemoveConfirm
					m.refreshViewport()
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenSteamLibraryConfirm:
			switch key {
			case "esc", "backspace":
				m.screen = screenSteamLibrary
				m.refreshViewport()
				return m, nil
			case "l":
				if m.err == nil && m.selectedSteam.kind == steamEntryLibrary {
					m.checking = true
					m.mutating = true
					m.activity = textActivityCreateSteamLink
					return m, tea.Batch(m.spinner.Tick, createSteamLibraryLink(m.selectedSteam.library))
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenSteamLibraryRemoveConfirm:
			switch key {
			case "esc", "backspace":
				m.screen = screenSteamLibrary
				m.refreshViewport()
				return m, nil
			case "x":
				if m.err == nil && m.selectedSteam.kind == steamEntryLink {
					m.checking = true
					m.mutating = true
					m.activity = textActivityRemoveSteamLink
					return m, tea.Batch(m.spinner.Tick, removeSteamLibraryLink(m.selectedSteam.link))
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenSteamLibraryResult:
			if key == "esc" || key == "backspace" {
				m.screen = screenPlugins
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenPlatformAssetOverride:
			switch key {
			case "esc", "backspace":
				m.screen = screenPlugins
				return m, nil
			case "up", "k":
				if m.gameCursor > 0 {
					m.gameCursor--
					m.refreshViewport()
				}
			case "down", "j":
				if m.gameCursor < len(m.steamGames)-1 {
					m.gameCursor++
					m.refreshViewport()
				}
			case "enter", " ":
				if game, ok := m.currentSteamGame(); ok && m.err == nil {
					m.selectedGame = game
					m.checking = true
					m.activity = textActivityAnalyzeGame
					return m, tea.Batch(m.spinner.Tick, analyzePlatformAssetOverride(game))
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenPlatformAssetOverrideDetails:
			if key == "esc" || key == "backspace" {
				m.screen = screenPlatformAssetOverride
				m.refreshViewport()
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenDoctor:
			switch key {
			case "esc", "backspace":
				m.screen = screenHome
				return m, nil
			case "r":
				m.checking = true
				m.activity = textActivityDoctor
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
				m.activity = textActivityDetailsRefresh
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
				m.activity = textActivityFetchRefresh
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
					m.activity = textActivityInstall
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
					m.activity = textActivityRollback
					return m, tea.Batch(m.spinner.Tick, rollbackLatest(m.ctx))
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenUninstallConfirm:
			switch key {
			case "esc", "backspace":
				m.screen = screenHome
				return m, nil
			case "x":
				if m.err == nil && m.removal != nil && m.removal.Detected {
					m.checking = true
					m.mutating = true
					m.activity = textActivityRemove
					return m, tea.Batch(m.spinner.Tick, uninstallBundle(m.ctx))
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenInstallResult, screenRollbackResult, screenUninstallResult:
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
	contentWidth := m.width - 12
	if contentWidth < 28 {
		contentWidth = 28
	}
	if contentWidth > 88 {
		contentWidth = 88
	}

	header := titleStyle.Render("SELENE  ☾") + "\n" + mutedStyle.Render(textTagline)
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
		case screenInstallConfirm, screenInstallResult, screenRollbackConfirm, screenRollbackResult,
			screenUninstallConfirm, screenUninstallResult, screenSteamLibrary, screenSteamLibraryConfirm,
			screenSteamLibraryRemoveConfirm, screenSteamLibraryResult, screenPlatformAssetOverride,
			screenPlatformAssetOverrideDetails:
			body = m.viewport.View()
		case screenPlugins:
			body = m.pluginsView()
		case screenAbout:
			body = aboutView()
		default:
			body = m.homeView()
		}
	}

	footerText := textFooterHome
	if m.screen == screenDoctor && !m.checking {
		footerText = textFooterDoctor
	} else if m.screen == screenPlan && !m.checking {
		footerText = textFooterDetails
	} else if m.screen == screenFetch && !m.checking {
		footerText = textFooterFetch
	} else if m.screen == screenInstallConfirm && !m.checking {
		footerText = textFooterInstallConfirm
	} else if m.screen == screenRollbackConfirm && !m.checking {
		footerText = textFooterRollbackConfirm
	} else if m.screen == screenUninstallConfirm && !m.checking {
		footerText = textFooterRemoveConfirm
	} else if m.screen == screenPlugins && !m.checking {
		footerText = textFooterPlugins
	} else if m.screen == screenSteamLibrary && !m.checking {
		footerText = textFooterSteamLibraries
	} else if m.screen == screenSteamLibraryConfirm && !m.checking {
		footerText = textFooterSteamLibraryConfirm
	} else if m.screen == screenSteamLibraryRemoveConfirm && !m.checking {
		footerText = textFooterSteamLibraryRemove
	} else if m.screen == screenSteamLibraryResult && !m.checking {
		footerText = textFooterPluginResult
	} else if m.screen == screenPlatformAssetOverride && !m.checking {
		footerText = textFooterPlatformAssetOverride
	} else if m.screen == screenPlatformAssetOverrideDetails && !m.checking {
		footerText = textFooterPlatformAssetOverrideDetails
	} else if (m.screen == screenInstallResult || m.screen == screenRollbackResult || m.screen == screenUninstallResult) && !m.checking {
		footerText = textFooterResult
	} else if m.mutating {
		footerText = textFooterTransaction
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
		if i == 3 || i == 5 {
			b.WriteString("\n")
		}
	}
	if m.cursor >= 0 && m.cursor < len(m.items) {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("  " + m.items[m.cursor].description))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) pluginsView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(textPluginsTitle))
	b.WriteString("\n\n")
	for index, item := range m.pluginItems {
		cursor := "  "
		style := lipgloss.NewStyle()
		if index == m.pluginCursor {
			cursor = "› "
			style = style.Bold(true).Foreground(accentColor)
		}
		fmt.Fprintf(&b, "%s%s\n", cursor, style.Render(item.title))
	}
	if m.pluginCursor >= 0 && m.pluginCursor < len(m.pluginItems) {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("  " + m.pluginItems[m.pluginCursor].description))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) doctorContent() string {
	if m.report == nil {
		return textNoDiagnostics
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", titleStyle.Render(textDoctorTitle))
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
		textDoctorSummaryFormat,
		summary.OK, summary.Warnings, summary.Errors,
	)))
	return strings.TrimRight(b.String(), "\n")
}

func (m *model) resizeViewport() {
	width := m.width - 12
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
			content = textNoDiagnostics
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
	case screenUninstallConfirm:
		content = m.uninstallConfirmContent()
	case screenUninstallResult:
		content = m.uninstallResultContent()
	case screenSteamLibrary:
		content = m.steamLibraryContent()
	case screenSteamLibraryConfirm:
		content = m.steamLibraryConfirmContent()
	case screenSteamLibraryRemoveConfirm:
		content = m.steamLibraryRemoveConfirmContent()
	case screenSteamLibraryResult:
		content = m.steamLibraryResultContent()
	case screenPlatformAssetOverride:
		content = m.platformAssetOverrideContent()
	case screenPlatformAssetOverrideDetails:
		content = m.platformAssetOverrideDetailsContent()
	default:
		return
	}
	content = lipgloss.NewStyle().Width(m.viewport.Width).Render(content)
	m.viewport.SetContent(content)
	m.viewport.GotoTop()
}

func (m model) planContent() string {
	if m.err != nil {
		return titleStyle.Render(textDetailsTitle) + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}
	if m.plan == nil {
		return textNoDetails
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(textDetailsTitle + " · " + m.plan.BundleName))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(textCatalogLabel + m.plan.CatalogRevision))
	b.WriteString("\n\n")
	if m.plan.Ready {
		b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Render(textDetailsReady))
	} else {
		b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Render(textDetailsBlocked))
		b.WriteString("\n")
		for _, blocker := range m.plan.Blockers {
			b.WriteString("  " + mutedStyle.Render("· "+blocker) + "\n")
		}
	}
	b.WriteString("\n" + textProposedOperations + "\n\n")
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
	b.WriteString(mutedStyle.Render(textNoChangesRefresh))
	return strings.TrimRight(b.String(), "\n")
}

func (m model) fetchContent() string {
	if m.err != nil {
		return titleStyle.Render(textArtifactsTitle) + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}
	if len(m.fetched) == 0 {
		return textNoArtifacts
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(textVerifiedArtifactsTitle))
	b.WriteString("\n\n")
	for _, result := range m.fetched {
		origin := textDownloadedNow
		if result.Cached {
			origin = textAlreadyCached
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(goodColor).Render("✓ " + result.Component))
		b.WriteString("  " + mutedStyle.Render(result.Version+" · "+origin) + "\n")
		name := strings.TrimPrefix(filepath.Base(result.Path), result.SHA256+"-")
		b.WriteString("  " + name + "\n")
		b.WriteString("  " + mutedStyle.Render(textCacheLabel+filepath.Dir(result.Path)) + "\n")
		b.WriteString("  " + mutedStyle.Render("sha256:"+compactHash(result.SHA256)) + "\n\n")
	}
	b.WriteString(mutedStyle.Render(textCacheOnly))
	return strings.TrimRight(b.String(), "\n")
}

func (m model) steamLibraryContent() string {
	if m.err != nil {
		return titleStyle.Render(textSteamLibraryTitle) + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(textSteamLibraryTitle))
	b.WriteString("\n\n")
	b.WriteString(textSteamLibraryIntro)
	b.WriteString("\n\n")
	if len(m.steamEntries) == 0 {
		b.WriteString(mutedStyle.Render(textNoSteamLibraries))
		b.WriteString("\n\n")
		b.WriteString(mutedStyle.Render(textSteamLibraryMountHint))
		return b.String()
	}

	for index, entry := range m.steamEntries {
		cursor := "  "
		style := lipgloss.NewStyle()
		if index == m.steamCursor {
			cursor = "› "
			style = style.Bold(true).Foreground(accentColor)
		}
		if entry.kind == steamEntryLibrary {
			b.WriteString(cursor + style.Render(textSteamLibraryAddLabel) + "\n")
			b.WriteString("  " + entry.library.Path + "\n")
			b.WriteString("  " + mutedStyle.Render(fmt.Sprintf(textSteamLibraryMetadataFormat, entry.library.Filesystem, entry.library.MountPoint)) + "\n")
		} else {
			b.WriteString(cursor + style.Render(textSteamLibraryManagedLabel) + "\n")
			b.WriteString("  " + entry.link.Path + "\n")
			b.WriteString("  " + mutedStyle.Render("→ "+entry.link.Target) + "\n")
		}
		b.WriteString("\n")
	}
	if entry, ok := m.currentSteamEntry(); ok {
		if entry.kind == steamEntryLibrary {
			b.WriteString(mutedStyle.Render(textSteamLibrarySelectHint))
		} else {
			b.WriteString(mutedStyle.Render(textSteamLibraryRemoveHint))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) steamLibraryConfirmContent() string {
	if m.selectedSteam.kind != steamEntryLibrary {
		return titleStyle.Render(textSteamLibraryConfirmTitle) + "\n\n" + errorStyle().Render("× "+textNoSteamLibrarySelected)
	}
	library := m.selectedSteam.library
	link := plugins.PlannedSteamLibraryLink(planner.Environment{
		XDGDataHome: m.steamDataHome,
	}, library)

	var b strings.Builder
	b.WriteString(titleStyle.Render(textSteamLibraryConfirmTitle))
	b.WriteString("\n\n")
	b.WriteString(textSteamLibraryConfirmIntro + "\n\n")
	b.WriteString(textSteamLibrarySourceLabel + library.Path + "\n")
	b.WriteString(textSteamLibraryMountLabel + library.MountPoint + "\n")
	b.WriteString(textSteamLibraryLinkLabel + link.Path + "\n\n")
	b.WriteString(textSteamLibrarySafety + "\n")
	b.WriteString(textSteamLibrarySteamHint + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Bold(true).Render(textSteamLibraryConfirmAction))
	b.WriteString("  " + mutedStyle.Render(textEscapeNoChanges))
	return b.String()
}

func (m model) steamLibraryRemoveConfirmContent() string {
	if m.selectedSteam.kind != steamEntryLink {
		return titleStyle.Render(textSteamLibraryRemoveTitle) + "\n\n" + errorStyle().Render("× "+textNoSteamLibraryLinkSelected)
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(textSteamLibraryRemoveTitle))
	b.WriteString("\n\n")
	b.WriteString(textSteamLibraryLinkLabel + m.selectedSteam.link.Path + "\n")
	b.WriteString(textSteamLibrarySourceLabel + m.selectedSteam.link.Target + "\n\n")
	b.WriteString(textSteamLibraryRemoveSafety + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(errorColor).Bold(true).Render(textSteamLibraryRemoveAction))
	b.WriteString("  " + mutedStyle.Render(textEscapeNoChanges))
	return b.String()
}

func (m model) steamLibraryResultContent() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(textSteamLibraryResultTitle))
	b.WriteString("\n\n")
	if m.err != nil {
		b.WriteString(errorStyle().Render("× " + m.err.Error()))
		return b.String()
	}
	if m.steamResult == nil {
		return b.String() + mutedStyle.Render(textNoSteamLibraryResult)
	}
	if m.steamRemoved {
		b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Bold(true).Render(textSteamLibraryRemoved))
	} else if m.steamResult.TransactionID == "" {
		b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Bold(true).Render(textSteamLibraryAlreadyLinked))
	} else {
		b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Bold(true).Render(textSteamLibraryLinked))
	}
	b.WriteString("\n")
	b.WriteString(textSteamLibraryLinkLabel + m.steamResult.Link.Path + "\n")
	b.WriteString(textSteamLibrarySourceLabel + m.steamResult.Link.Target + "\n")
	if m.steamResult.TransactionID != "" {
		b.WriteString(textTransactionLabel + m.steamResult.TransactionID + "\n")
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(textSteamLibraryResultHint))
	return strings.TrimRight(b.String(), "\n")
}

func (m model) platformAssetOverrideContent() string {
	if m.err != nil {
		return titleStyle.Render(textPlatformAssetOverrideTitle) + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(textPlatformAssetOverrideTitle))
	b.WriteString("\n\n")
	b.WriteString(textPlatformAssetOverrideIntro)
	b.WriteString("\n\n")
	if len(m.steamGames) == 0 {
		b.WriteString(mutedStyle.Render(textNoSteamGames))
		return b.String()
	}
	for index, game := range m.steamGames {
		cursor := "  "
		style := lipgloss.NewStyle()
		if index == m.gameCursor {
			cursor = "› "
			style = style.Bold(true).Foreground(accentColor)
		}
		b.WriteString(cursor + style.Render(game.Name) + "\n")
		b.WriteString("  " + mutedStyle.Render(fmt.Sprintf(textSteamGameMetadataFormat, game.AppID, game.LibraryPath)) + "\n\n")
	}
	b.WriteString(mutedStyle.Render(textPlatformAssetOverrideSelectHint))
	return strings.TrimRight(b.String(), "\n")
}

func (m model) platformAssetOverrideDetailsContent() string {
	if m.err != nil {
		return titleStyle.Render(textPlatformAssetOverrideAnalysisTitle) + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}
	if m.gameAnalysis == nil {
		return titleStyle.Render(textPlatformAssetOverrideAnalysisTitle) + "\n\n" + mutedStyle.Render(textNoPlatformAssetOverrideAnalysis)
	}
	analysis := m.gameAnalysis
	var b strings.Builder
	b.WriteString(titleStyle.Render(textPlatformAssetOverrideAnalysisTitle + " · " + analysis.Game.Name))
	b.WriteString("\n\n")
	b.WriteString(textSteamGamePathLabel + analysis.Game.InstallPath + "\n")
	b.WriteString(textDetectedEngineLabel + gameEngineLabel(analysis.Engine) + "\n\n")

	if analysis.PlatformPluginDescriptor != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Bold(true).Render(textPlatformPluginFound))
		b.WriteString("\n")
		b.WriteString("  " + analysis.PlatformPluginDescriptor + "\n\n")
	} else if analysis.PlatformPluginReferenced {
		b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Bold(true).Render(textPlatformPluginReferenced))
		b.WriteString("\n\n")
	} else {
		b.WriteString(mutedStyle.Render(textPlatformPluginNotFound))
		b.WriteString("\n\n")
	}

	if len(analysis.UnityAssets) > 0 {
		b.WriteString(mutedStyle.Render(textUnityAssetsFound))
		b.WriteString("\n")
		for _, asset := range analysis.UnityAssets {
			b.WriteString("  · " + asset + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(textPlatformAssetOverrideSafety + "\n\n")
	b.WriteString(mutedStyle.Render(textPlatformAssetOverrideVerifyHint))
	return strings.TrimRight(b.String(), "\n")
}

func (m model) installConfirmContent() string {
	if m.err != nil {
		return titleStyle.Render(textInstallConfirmTitle) + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}
	if m.plan == nil {
		return textNoDetails
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(textInstallConfirmTitle + " · " + m.plan.BundleName))
	b.WriteString("\n\n")
	if !m.plan.Ready {
		b.WriteString(errorStyle().Render(textInstallBlocked))
		b.WriteString("\n")
		for _, blocker := range m.plan.Blockers {
			b.WriteString("  " + mutedStyle.Render("· "+blocker) + "\n")
		}
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Bold(true).Render(textBeforeContinue))
	b.WriteString("\n\n")
	b.WriteString(textInstallBulletSteam + "\n")
	b.WriteString(textInstallBulletSource + "\n")
	b.WriteString(textInstallBulletScope + "\n")
	b.WriteString(textInstallBulletBackup + "\n")
	b.WriteString(textInstallBulletFail + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render(textInstallAction))
	b.WriteString("  " + mutedStyle.Render(textEscapeNoChanges))
	return b.String()
}

func (m model) installResultContent() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(textInstallResultTitle))
	b.WriteString("\n\n")
	if m.err != nil {
		b.WriteString(errorStyle().Render("× " + m.err.Error()))
		b.WriteString("\n\n")
	} else if m.installed != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Bold(true).Render(textInstalled))
		b.WriteString("\n")
		b.WriteString(textTransactionLabel + m.installed.TransactionID + "\n")
		b.WriteString(mutedStyle.Render(textSnapshotRetained))
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(m.log) != "" {
		b.WriteString(mutedStyle.Render(textOperationLog))
		b.WriteString("\n")
		b.WriteString(m.log)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) rollbackConfirmContent() string {
	if m.err != nil {
		return titleStyle.Render(textRollbackTitle) + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}
	journal := latestRestorable(m.history)
	if journal == nil {
		return titleStyle.Render(textRollbackTitle) + "\n\n" + mutedStyle.Render(textNoRollback)
	}
	heading := textRollbackTitle
	explanation := textRollbackExplanation
	confirmation := textRollbackAction
	if strings.HasPrefix(journal.Description, "uninstall ") {
		heading = textRecoveryTitle
		explanation = textRecoveryExplanation
		confirmation = textRecoveryAction
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(heading))
	b.WriteString("\n\n")
	b.WriteString(textSnapshotLabel + journal.ID + "\n")
	b.WriteString(textCreatedLabel + journal.CreatedAt.Local().Format("2006-01-02 15:04:05") + "\n")
	b.WriteString(textOperationLabel + journal.Description + "\n\n")
	b.WriteString(explanation + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Bold(true).Render(confirmation))
	b.WriteString("  " + mutedStyle.Render(textEscapeKeepsInstall))
	return b.String()
}

func (m model) rollbackResultContent() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(textRollbackResultTitle))
	b.WriteString("\n\n")
	if m.err != nil {
		b.WriteString(errorStyle().Render("× " + m.err.Error()))
		b.WriteString("\n\n")
	} else if m.rolledBack != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Bold(true).Render(textPreviousStateRestored))
		b.WriteString("\n")
		b.WriteString(textTransactionLabel + m.rolledBack.TransactionID + "\n\n")
	}
	if strings.TrimSpace(m.log) != "" {
		b.WriteString(mutedStyle.Render(textOperationLog))
		b.WriteString("\n")
		b.WriteString(m.log)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) uninstallConfirmContent() string {
	if m.err != nil {
		return titleStyle.Render(textRemoveTitle) + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}
	if m.removal == nil || !m.removal.Detected {
		return titleStyle.Render(textRemoveTitle) + "\n\n" + mutedStyle.Render(textNoManagedTraces)
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(textRemoveTitle))
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Bold(true).Render(textRemoveIsDifferent))
	b.WriteString("\n\n")
	b.WriteString(textRemoveScope + "\n\n")
	b.WriteString(textRemoveKeeps + "\n\n")
	b.WriteString(mutedStyle.Render(textDetectedTraces))
	b.WriteString("\n")
	for _, trace := range m.removal.Traces {
		b.WriteString("  · " + trace + "\n")
	}
	b.WriteString("\n")
	b.WriteString(textRemoveSafety + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(errorColor).Bold(true).Render(textRemoveAction))
	b.WriteString("  " + mutedStyle.Render(textEscapeNoChanges))
	return strings.TrimRight(b.String(), "\n")
}

func (m model) uninstallResultContent() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(textRemoveResultTitle))
	b.WriteString("\n\n")
	if m.err != nil {
		b.WriteString(errorStyle().Render("× " + m.err.Error()))
		b.WriteString("\n\n")
	} else if m.uninstalled != nil && m.uninstalled.Removed {
		b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Bold(true).Render(textRemoved))
		b.WriteString("\n")
		b.WriteString(textRemovedExplanation + "\n")
		b.WriteString(textSafetyTransactionLabel + m.uninstalled.TransactionID + "\n\n")
	} else {
		b.WriteString(mutedStyle.Render(textNothingRemoved))
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(m.log) != "" {
		b.WriteString(mutedStyle.Render(textOperationLog))
		b.WriteString("\n")
		b.WriteString(m.log)
	}
	return strings.TrimRight(b.String(), "\n")
}

func latestRestorable(history []transaction.Journal) *transaction.Journal {
	for index := range history {
		journal := &history[index]
		if journal.State == transaction.StateRolledBack {
			continue
		}
		if strings.HasPrefix(journal.Description, "uninstall ") {
			if journal.State == transaction.StateCommitted {
				return nil
			}
			return journal
		}
		if strings.HasPrefix(journal.Description, "install ") {
			return journal
		}
	}
	return nil
}

func makeSteamEntries(libraries []plugins.SteamLibrary, links []plugins.Link) []steamEntry {
	linkedTargets := make(map[string]bool, len(links))
	for _, link := range links {
		linkedTargets[filepath.Clean(link.Target)] = true
	}
	entries := make([]steamEntry, 0, len(libraries)+len(links))
	for _, library := range libraries {
		if !linkedTargets[filepath.Clean(library.Path)] {
			entries = append(entries, steamEntry{kind: steamEntryLibrary, library: library})
		}
	}
	for _, link := range links {
		entries = append(entries, steamEntry{kind: steamEntryLink, link: link})
	}
	return entries
}

func (m model) currentSteamEntry() (steamEntry, bool) {
	if m.steamCursor < 0 || m.steamCursor >= len(m.steamEntries) {
		return steamEntry{}, false
	}
	return m.steamEntries[m.steamCursor], true
}

func (m model) currentSteamGame() (plugins.SteamGame, bool) {
	if m.gameCursor < 0 || m.gameCursor >= len(m.steamGames) {
		return plugins.SteamGame{}, false
	}
	return m.steamGames[m.gameCursor], true
}

func gameEngineLabel(engine plugins.GameEngine) string {
	switch engine {
	case plugins.GameEngineUnreal:
		return textDetectedEngineUnreal
	case plugins.GameEngineUnity:
		return textDetectedEngineUnity
	default:
		return textDetectedEngineUnknown
	}
}

func compactHash(value string) string {
	if len(value) <= 24 {
		return value
	}
	return value[:16] + "…" + value[len(value)-8:]
}

func aboutView() string {
	return titleStyle.Render(textAboutTitle) + "\n\n" + textAboutBody + "\n\n" + mutedStyle.Render(textAboutState) + "\n\n" + titleStyle.Render(textAboutAuthor)
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
			return fetchMsg{err: fmt.Errorf("bundle luatools not found")}
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

func previewUninstall() tea.Cmd {
	return func() tea.Msg {
		env, err := planner.DetectEnvironment()
		if err != nil {
			return uninstallPreviewMsg{err: err}
		}
		preview, err := installer.PreviewUninstall(env)
		return uninstallPreviewMsg{preview: preview, err: err}
	}
}

func uninstallBundle(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		source, err := catalog.LoadStable()
		if err != nil {
			return uninstallMsg{err: err}
		}
		env, err := planner.DetectEnvironment()
		if err != nil {
			return uninstallMsg{err: err}
		}
		var output bytes.Buffer
		result, err := installer.Uninstall(ctx, source, env, installer.Options{Output: &output})
		return uninstallMsg{result: result, log: output.String(), err: err}
	}
}

func scanSteamLibraries() tea.Cmd {
	return func() tea.Msg {
		env, err := planner.DetectEnvironment()
		if err != nil {
			return steamLibrariesMsg{err: err}
		}
		libraries, err := plugins.DiscoverSteamLibraries(env)
		if err != nil {
			return steamLibrariesMsg{err: err}
		}
		links, err := plugins.ManagedLinks(env)
		return steamLibrariesMsg{libraries: libraries, links: links, dataHome: env.XDGDataHome, err: err}
	}
}

func createSteamLibraryLink(library plugins.SteamLibrary) tea.Cmd {
	return func() tea.Msg {
		env, err := planner.DetectEnvironment()
		if err != nil {
			return steamLinkMsg{err: err}
		}
		result, err := plugins.CreateSteamLibraryLink(env, library)
		return steamLinkMsg{result: result, err: err}
	}
}

func removeSteamLibraryLink(link plugins.Link) tea.Cmd {
	return func() tea.Msg {
		env, err := planner.DetectEnvironment()
		if err != nil {
			return steamLinkMsg{removed: true, err: err}
		}
		result, err := plugins.RemoveSteamLibraryLink(env, link)
		return steamLinkMsg{result: result, removed: true, err: err}
	}
}

func scanSteamGames() tea.Cmd {
	return func() tea.Msg {
		env, err := planner.DetectEnvironment()
		if err != nil {
			return steamGamesMsg{err: err}
		}
		games, err := plugins.DiscoverSteamGames(env)
		return steamGamesMsg{games: games, err: err}
	}
}

func analyzePlatformAssetOverride(game plugins.SteamGame) tea.Cmd {
	return func() tea.Msg {
		analysis, err := plugins.AnalyzePlatformAssetOverride(game)
		return assetOverrideAnalysisMsg{analysis: analysis, err: err}
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
