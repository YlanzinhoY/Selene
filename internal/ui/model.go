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
	screenPlatformAssetOverride
	screenPlatformAssetOverrideDetails
	screenPlatformAssetOverrideFixConfirm
	screenPlatformAssetOverrideFixResult
	screenCompatdata
	screenCompatdataPlan
	screenCompatdataSteamConfirm
	screenCompatdataResult
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

type steamGamesMsg struct {
	games []plugins.SteamGame
	err   error
}

type assetOverrideAnalysisMsg struct {
	analysis plugins.AssetOverrideAnalysis
	err      error
}

type assetOverrideFixMsg struct {
	fix plugins.PlatformAssetOverrideFix
	err error
}

type compatdataMsg struct {
	plans []plugins.CompatdataPlan
	err   error
}

type compatdataApplyMsg struct {
	result     plugins.CompatdataResult
	rolledBack bool
	err        error
}

type compatdataSteamClosedMsg struct {
	err error
}

type compatdataPendingAction int

const (
	compatdataActionNone compatdataPendingAction = iota
	compatdataActionConfigure
	compatdataActionRestore
)

type menuItem struct {
	title       string
	description string
}

type model struct {
	width            int
	height           int
	cursor           int
	pluginCursor     int
	gameCursor       int
	screen           screen
	ctx              context.Context
	cancel           context.CancelFunc
	checking         bool
	mutating         bool
	activity         string
	spinner          spinner.Model
	viewport         viewport.Model
	report           *doctor.Report
	plan             *planner.Plan
	fetched          []artifact.Result
	installed        *installer.Result
	rolledBack       *installer.RollbackResult
	removal          *installer.UninstallPreview
	uninstalled      *installer.UninstallResult
	history          []transaction.Journal
	log              string
	err              error
	items            []menuItem
	pluginItems      []menuItem
	steamGames       []plugins.SteamGame
	selectedGame     plugins.SteamGame
	gameAnalysis     *plugins.AssetOverrideAnalysis
	gameFix          *plugins.PlatformAssetOverrideFix
	compatPlans      []plugins.CompatdataPlan
	compatCursor     int
	selectedPlan     plugins.CompatdataPlan
	compatResult     *plugins.CompatdataResult
	compatRolledBack bool
	compatPending    compatdataPendingAction
	steamRunning     func() bool
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
		ctx:          ctx,
		cancel:       cancel,
		spinner:      s,
		viewport:     viewport.New(80, 15),
		items:        defaultMenuItems(),
		pluginItems:  defaultPluginItems(),
		steamRunning: plugins.SteamRunning,
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
	case assetOverrideFixMsg:
		m.checking = false
		m.mutating = false
		m.activity = ""
		m.err = msg.err
		m.gameFix = &msg.fix
		m.screen = screenPlatformAssetOverrideFixResult
		m.refreshViewport()
		return m, nil
	case compatdataMsg:
		m.checking = false
		m.activity = ""
		m.err = msg.err
		m.compatPlans = msg.plans
		m.compatCursor = 0
		m.compatResult = nil
		m.compatRolledBack = false
		m.screen = screenCompatdata
		m.refreshViewport()
		return m, nil
	case compatdataApplyMsg:
		m.checking = false
		m.mutating = false
		m.activity = ""
		m.err = msg.err
		m.compatResult = &msg.result
		m.compatRolledBack = msg.rolledBack
		m.compatPending = compatdataActionNone
		m.screen = screenCompatdataResult
		m.refreshViewport()
		return m, nil
	case compatdataSteamClosedMsg:
		m.err = msg.err
		if msg.err != nil {
			m.checking = false
			m.mutating = false
			m.activity = ""
			m.refreshViewport()
			return m, nil
		}
		m.checking = true
		m.mutating = true
		if m.compatPending == compatdataActionRestore {
			m.activity = textActivityRollbackCompatdata
		} else {
			m.activity = textActivityConfigureCompatdata
		}
		return m, tea.Batch(m.spinner.Tick, runCompatdataOperation(m.selectedPlan, m.compatPending))
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
					m.activity = textActivityScanCompatdata
					return m, tea.Batch(m.spinner.Tick, scanCompatdata())
				case 1:
					m.checking = true
					m.activity = textActivitySteamGames
					return m, tea.Batch(m.spinner.Tick, scanSteamGames())
				}
			}
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
			switch key {
			case "esc", "backspace":
				m.screen = screenPlatformAssetOverride
				m.refreshViewport()
				return m, nil
			case "f":
				if m.err == nil && m.gameAnalysis != nil && m.gameAnalysis.Engine == plugins.GameEngineUnreal && m.gameAnalysis.PlatformPluginDescriptor != "" {
					m.screen = screenPlatformAssetOverrideFixConfirm
					m.refreshViewport()
					return m, nil
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenPlatformAssetOverrideFixConfirm:
			switch key {
			case "esc", "backspace":
				m.screen = screenPlatformAssetOverrideDetails
				m.refreshViewport()
				return m, nil
			case "f":
				if m.err == nil && m.gameAnalysis != nil {
					m.checking = true
					m.mutating = true
					m.activity = textActivityFixPlatformAssetOverride
					return m, tea.Batch(m.spinner.Tick, fixPlatformAssetOverride(m.selectedGame))
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenPlatformAssetOverrideFixResult:
			switch key {
			case "esc", "backspace":
				m.screen = screenPlatformAssetOverride
				m.refreshViewport()
				return m, nil
			case "u":
				if m.err == nil && m.gameFix != nil {
					m.checking = true
					m.mutating = true
					m.activity = textActivityUndoPlatformAssetOverride
					return m, tea.Batch(m.spinner.Tick, undoPlatformAssetOverrideFix(m.selectedGame))
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenCompatdata:
			switch key {
			case "esc", "backspace":
				m.screen = screenPlugins
				return m, nil
			case "up", "k":
				if m.compatCursor > 0 {
					m.compatCursor--
					m.refreshViewport()
				}
			case "down", "j":
				if m.compatCursor < len(m.compatPlans)-1 {
					m.compatCursor++
					m.refreshViewport()
				}
			case "enter", " ":
				if plan, ok := m.currentCompatdataPlan(); ok && m.err == nil {
					m.selectedPlan = plan
					m.screen = screenCompatdataPlan
					m.refreshViewport()
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenCompatdataPlan:
			switch key {
			case "esc", "backspace":
				m.screen = screenCompatdata
				m.refreshViewport()
				return m, nil
			case "c":
				if m.err == nil && m.selectedPlan.Library.Path != "" && m.selectedPlan.BlockedReason == "" &&
					(m.selectedPlan.CurrentState == plugins.CompatdataDirectory || m.selectedPlan.CurrentState == plugins.CompatdataMissing ||
						m.selectedPlan.CurrentState == plugins.CompatdataExternalLink || m.selectedPlan.CurrentState == plugins.CompatdataBrokenLink) {
					m.compatPending = compatdataActionConfigure
					if m.isSteamRunning() {
						m.err = nil
						m.screen = screenCompatdataSteamConfirm
						m.refreshViewport()
						return m, nil
					}
					m.checking = true
					m.mutating = true
					m.activity = textActivityConfigureCompatdata
					return m, tea.Batch(m.spinner.Tick, runCompatdataOperation(m.selectedPlan, m.compatPending))
				}
			case "r":
				if m.err == nil && m.selectedPlan.Library.Path != "" && m.selectedPlan.BlockedReason == "" &&
					m.selectedPlan.CurrentState == plugins.CompatdataManagedLink && m.selectedPlan.RollbackAvailable {
					m.compatPending = compatdataActionRestore
					if m.isSteamRunning() {
						m.err = nil
						m.screen = screenCompatdataSteamConfirm
						m.refreshViewport()
						return m, nil
					}
					m.checking = true
					m.mutating = true
					m.activity = textActivityRollbackCompatdata
					return m, tea.Batch(m.spinner.Tick, runCompatdataOperation(m.selectedPlan, m.compatPending))
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenCompatdataSteamConfirm:
			switch key {
			case "esc", "backspace":
				m.err = nil
				m.compatPending = compatdataActionNone
				m.screen = screenCompatdataPlan
				m.refreshViewport()
				return m, nil
			case "c":
				if m.compatPending != compatdataActionNone && m.selectedPlan.Library.Path != "" {
					m.err = nil
					m.checking = true
					m.mutating = true
					m.activity = textActivityCloseSteam
					return m, tea.Batch(m.spinner.Tick, closeSteamForCompatdata(m.ctx))
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case screenCompatdataResult:
			if key == "esc" || key == "backspace" {
				m.screen = screenPlugins
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
			screenUninstallConfirm, screenUninstallResult, screenPlatformAssetOverride,
			screenPlatformAssetOverrideDetails, screenPlatformAssetOverrideFixConfirm,
			screenPlatformAssetOverrideFixResult, screenCompatdata, screenCompatdataPlan, screenCompatdataSteamConfirm, screenCompatdataResult:
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
	} else if m.screen == screenPlatformAssetOverride && !m.checking {
		footerText = textFooterPlatformAssetOverride
	} else if m.screen == screenPlatformAssetOverrideDetails && !m.checking {
		footerText = textFooterPlatformAssetOverrideDetails
	} else if m.screen == screenPlatformAssetOverrideFixConfirm && !m.checking {
		footerText = textFooterPlatformAssetOverrideFixConfirm
	} else if m.screen == screenPlatformAssetOverrideFixResult && !m.checking {
		footerText = textFooterPlatformAssetOverrideFixResult
	} else if m.screen == screenCompatdata && !m.checking {
		footerText = textFooterCompatdata
	} else if m.screen == screenCompatdataPlan && !m.checking {
		footerText = textFooterCompatdataPlan
		if m.selectedPlan.BlockedReason == "" &&
			(m.selectedPlan.CurrentState == plugins.CompatdataDirectory || m.selectedPlan.CurrentState == plugins.CompatdataMissing ||
				m.selectedPlan.CurrentState == plugins.CompatdataExternalLink || m.selectedPlan.CurrentState == plugins.CompatdataBrokenLink) {
			footerText = textFooterCompatdataPlanConfigure
		} else if m.selectedPlan.BlockedReason == "" &&
			m.selectedPlan.CurrentState == plugins.CompatdataManagedLink && m.selectedPlan.RollbackAvailable {
			footerText = textFooterCompatdataPlanRollback
		}
	} else if m.screen == screenCompatdataSteamConfirm && !m.checking {
		footerText = textFooterCompatdataSteamConfirm
	} else if m.screen == screenCompatdataResult && !m.checking {
		footerText = textFooterResult
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
	case screenPlatformAssetOverride:
		content = m.platformAssetOverrideContent()
	case screenPlatformAssetOverrideDetails:
		content = m.platformAssetOverrideDetailsContent()
	case screenPlatformAssetOverrideFixConfirm:
		content = m.platformAssetOverrideFixConfirmContent()
	case screenPlatformAssetOverrideFixResult:
		content = m.platformAssetOverrideFixResultContent()
	case screenCompatdata:
		content = m.compatdataContent()
	case screenCompatdataPlan:
		content = m.compatdataPlanContent()
	case screenCompatdataSteamConfirm:
		content = m.compatdataSteamConfirmContent()
	case screenCompatdataResult:
		content = m.compatdataResultContent()
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
	if analysis.Engine == plugins.GameEngineUnreal && analysis.PlatformPluginDescriptor != "" {
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render(textPlatformAssetOverrideFixAction))
		b.WriteString("  " + mutedStyle.Render(textEscapeNoChanges))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) platformAssetOverrideFixConfirmContent() string {
	if m.gameAnalysis == nil {
		return titleStyle.Render(textPlatformAssetOverrideFixConfirmTitle) + "\n\n" + mutedStyle.Render(textNoPlatformAssetOverrideAnalysis)
	}
	analysis := m.gameAnalysis
	var b strings.Builder
	b.WriteString(titleStyle.Render(textPlatformAssetOverrideFixConfirmTitle + " · " + analysis.Game.Name))
	b.WriteString("\n\n")
	b.WriteString(textPlatformAssetOverrideFixIntro + "\n\n")
	b.WriteString(textPlatformPluginFound + "\n")
	b.WriteString("  " + analysis.PlatformPluginDescriptor + "\n\n")
	b.WriteString(textPlatformAssetOverrideFixSafety + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Bold(true).Render(textPlatformAssetOverrideFixConfirmAction))
	b.WriteString("  " + mutedStyle.Render(textEscapeNoChanges))
	return strings.TrimRight(b.String(), "\n")
}

func (m model) platformAssetOverrideFixResultContent() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(textPlatformAssetOverrideFixResultTitle))
	b.WriteString("\n\n")
	if m.err != nil {
		b.WriteString(errorStyle().Render("× " + m.err.Error()))
		return b.String()
	}
	if m.gameFix == nil {
		return b.String() + mutedStyle.Render(textNoPlatformAssetOverrideFix)
	}
	b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Bold(true).Render(textPlatformAssetOverrideFixed))
	b.WriteString("\n")
	b.WriteString(textSteamGamePathLabel + m.gameFix.Game.Name + "\n")
	b.WriteString(textDisabledDescriptorLabel + m.gameFix.DisabledFile + "\n")
	if m.gameFix.TransactionID != "" {
		b.WriteString(textTransactionLabel + m.gameFix.TransactionID + "\n")
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(textPlatformAssetOverrideFixResultHint))
	return strings.TrimRight(b.String(), "\n")
}

func (m model) compatdataContent() string {
	if m.err != nil {
		return titleStyle.Render(textCompatdataTitle) + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(textCompatdataTitle))
	b.WriteString("\n\n")
	b.WriteString(textCompatdataIntro)
	b.WriteString("\n\n")
	if len(m.compatPlans) == 0 {
		b.WriteString(mutedStyle.Render(textNoCompatdataLibraries))
		return b.String()
	}
	for index, plan := range m.compatPlans {
		cursor := "  "
		style := lipgloss.NewStyle()
		if index == m.compatCursor {
			cursor = "› "
			style = style.Bold(true).Foreground(accentColor)
		}
		b.WriteString(cursor + style.Render(plan.Library.Path) + "\n")
		b.WriteString("  " + mutedStyle.Render(fmt.Sprintf(textCompatdataMetadataFormat, plan.Library.Filesystem, plan.Library.MountPoint)) + "\n")
		b.WriteString("  " + mutedStyle.Render(textCompatdataStateLabel+string(plan.CurrentState)) + "\n")
		if plan.BlockedReason != "" {
			b.WriteString("  " + errorStyle().Render(textCompatdataBlockedLabel+plan.BlockedReason) + "\n")
		}
		b.WriteString("\n")
	}
	if _, ok := m.currentCompatdataPlan(); ok {
		b.WriteString(mutedStyle.Render(textCompatdataSelectHint))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) compatdataPlanContent() string {
	plan := m.selectedPlan
	if plan.Library.Path == "" {
		return titleStyle.Render(textCompatdataPlanTitle) + "\n\n" + mutedStyle.Render(textNoCompatdataPlan)
	}
	if m.err != nil {
		return titleStyle.Render(textCompatdataPlanTitle) + "\n\n" + errorStyle().Render("× "+m.err.Error())
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(textCompatdataPlanTitle))
	b.WriteString("\n\n")
	b.WriteString(textSteamLibrarySourceLabel + plan.Library.Path + "\n")
	b.WriteString(textSteamLibraryMountLabel + plan.Library.MountPoint + "\n")
	b.WriteString(textCompatdataCurrentLabel + plan.Compatdata + "\n")
	if plan.LinkTarget != "" {
		b.WriteString(textCompatdataExistingLinkLabel + plan.LinkTarget + "\n")
	}
	if plan.ImportSource != "" {
		b.WriteString(textCompatdataImportSourceLabel + plan.ImportSource + "\n")
	}
	b.WriteString(textCompatdataTargetLabel + plan.NativeTarget + "\n")
	if plan.PreservedNativeTarget != "" {
		b.WriteString(textCompatdataPreservedLabel + plan.PreservedNativeTarget + "\n")
	}
	if plan.RequiresBackup || (plan.RollbackAvailable && plan.BackupPath != "") {
		b.WriteString(textCompatdataBackupLabel + plan.BackupPath + "\n")
	}
	b.WriteString("\n")
	if plan.BlockedReason != "" {
		b.WriteString(errorStyle().Render(textCompatdataBlockedLabel + plan.BlockedReason))
		b.WriteString("\n\n")
		b.WriteString(mutedStyle.Render(textCompatdataCannotConfigure))
		return strings.TrimRight(b.String(), "\n")
	}
	if plan.CurrentState == plugins.CompatdataManagedLink {
		b.WriteString(mutedStyle.Render(textCompatdataAlreadyManaged))
		b.WriteString("\n\n")
		if plan.RollbackAvailable {
			b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Bold(true).Render(textCompatdataRollbackAction))
			b.WriteString("  " + mutedStyle.Render(textCompatdataRollbackSafety))
		} else {
			b.WriteString(mutedStyle.Render(textCompatdataNoRollback))
		}
		return strings.TrimRight(b.String(), "\n")
	}
	if plan.PreservedNativeTarget != "" {
		b.WriteString(mutedStyle.Render(textCompatdataWillRecoverRollback))
	} else if plan.DetachesExistingLink && plan.RequiresCopy {
		b.WriteString(mutedStyle.Render(textCompatdataWillImportLink))
	} else if plan.DetachesExistingLink {
		b.WriteString(mutedStyle.Render(textCompatdataWillReplaceBroken))
	} else if plan.RequiresCopy {
		b.WriteString(mutedStyle.Render(textCompatdataWillCopy))
	} else {
		b.WriteString(mutedStyle.Render(textCompatdataWillLink))
	}
	b.WriteString("\n\n")
	if plan.PreservedNativeTarget != "" {
		b.WriteString(textCompatdataRecoverySafety + "\n\n")
	} else if plan.DetachesExistingLink {
		b.WriteString(textCompatdataManualLinkSafety + "\n\n")
	} else {
		b.WriteString(textCompatdataSafety + "\n\n")
	}
	if plan.CurrentState == plugins.CompatdataDirectory || plan.CurrentState == plugins.CompatdataMissing ||
		plan.CurrentState == plugins.CompatdataExternalLink || plan.CurrentState == plugins.CompatdataBrokenLink {
		action := textCompatdataConfigureAction
		if plan.DetachesExistingLink {
			action = textCompatdataReplaceLinkAction
		}
		b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Bold(true).Render(action))
		b.WriteString("  " + mutedStyle.Render(textEscapeNoChanges))
	} else {
		b.WriteString(mutedStyle.Render(textCompatdataCannotConfigure))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) compatdataSteamConfirmContent() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(textCompatdataSteamConfirmTitle))
	b.WriteString("\n\n")
	b.WriteString(textCompatdataSteamDetected)
	b.WriteString("\n\n")
	if m.compatPending == compatdataActionRestore {
		b.WriteString(textCompatdataSteamRestoreReason)
	} else {
		b.WriteString(textCompatdataSteamConfigureReason)
	}
	b.WriteString("\n\n")
	b.WriteString(textCompatdataSteamCloseBehavior)
	if m.err != nil {
		b.WriteString("\n\n")
		b.WriteString(errorStyle().Render("× " + m.err.Error()))
	}
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(warnColor).Bold(true).Render(textCompatdataSteamCloseAction))
	b.WriteString("  " + mutedStyle.Render(textEscapeNoChanges))
	return strings.TrimRight(b.String(), "\n")
}

func (m model) isSteamRunning() bool {
	if m.steamRunning == nil {
		return plugins.SteamRunning()
	}
	return m.steamRunning()
}

func (m model) compatdataResultContent() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(textCompatdataResultTitle))
	b.WriteString("\n\n")
	if m.err != nil {
		b.WriteString(errorStyle().Render("× " + m.err.Error()))
		return b.String()
	}
	if m.compatResult == nil {
		return b.String() + mutedStyle.Render(textNoCompatdataResult)
	}
	plan := m.compatResult.Plan
	if m.compatRolledBack {
		b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Bold(true).Render(textCompatdataRolledBack))
	} else if m.compatResult.TransactionID == "" {
		b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Bold(true).Render(textCompatdataAlreadyConfigured))
	} else {
		b.WriteString(lipgloss.NewStyle().Foreground(goodColor).Bold(true).Render(textCompatdataConfigured))
	}
	b.WriteString("\n")
	b.WriteString(textCompatdataSourceLabel + plan.Compatdata + "\n")
	b.WriteString(textCompatdataTargetLabel + plan.NativeTarget + "\n")
	if plan.PreservedNativeTarget != "" {
		b.WriteString(textCompatdataPreservedLabel + plan.PreservedNativeTarget + "\n")
	}
	if m.compatResult.TransactionID != "" {
		b.WriteString(textTransactionLabel + m.compatResult.TransactionID + "\n")
	}
	b.WriteString("\n")
	if m.compatRolledBack {
		b.WriteString(mutedStyle.Render(textCompatdataRollbackResultHint))
	} else if plan.PreservedNativeTarget != "" {
		b.WriteString(mutedStyle.Render(textCompatdataRecoveryResultHint))
	} else if plan.DetachesExistingLink {
		b.WriteString(mutedStyle.Render(textCompatdataManualResultHint))
	} else {
		b.WriteString(mutedStyle.Render(textCompatdataResultHint))
	}
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

func (m model) currentSteamGame() (plugins.SteamGame, bool) {
	if m.gameCursor < 0 || m.gameCursor >= len(m.steamGames) {
		return plugins.SteamGame{}, false
	}
	return m.steamGames[m.gameCursor], true
}

func (m model) currentCompatdataPlan() (plugins.CompatdataPlan, bool) {
	if m.compatCursor < 0 || m.compatCursor >= len(m.compatPlans) {
		return plugins.CompatdataPlan{}, false
	}
	return m.compatPlans[m.compatCursor], true
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

func scanCompatdata() tea.Cmd {
	return func() tea.Msg {
		env, err := planner.DetectEnvironment()
		if err != nil {
			return compatdataMsg{err: err}
		}
		libraries, err := plugins.DiscoverSteamLibraries(env)
		if err != nil {
			return compatdataMsg{err: err}
		}
		plans := make([]plugins.CompatdataPlan, 0, len(libraries))
		for _, library := range libraries {
			plan, planErr := plugins.PlanCompatdataMigration(env, library)
			if planErr != nil {
				return compatdataMsg{err: planErr}
			}
			plans = append(plans, plan)
		}
		return compatdataMsg{plans: plans}
	}
}

func runCompatdataOperation(plan plugins.CompatdataPlan, action compatdataPendingAction) tea.Cmd {
	return func() tea.Msg {
		env, err := planner.DetectEnvironment()
		if err != nil {
			return compatdataApplyMsg{err: err}
		}
		if action == compatdataActionRestore {
			result, err := plugins.RollbackLatestCompatdataMigration(env, plan.Library)
			return compatdataApplyMsg{result: result, rolledBack: true, err: err}
		}
		result, err := plugins.ApplyCompatdataMigration(env, plan)
		return compatdataApplyMsg{result: result, err: err}
	}
}

func closeSteamForCompatdata(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		return compatdataSteamClosedMsg{err: plugins.CloseSteam(ctx)}
	}
}

func analyzePlatformAssetOverride(game plugins.SteamGame) tea.Cmd {
	return func() tea.Msg {
		analysis, err := plugins.AnalyzePlatformAssetOverride(game)
		return assetOverrideAnalysisMsg{analysis: analysis, err: err}
	}
}

func fixPlatformAssetOverride(game plugins.SteamGame) tea.Cmd {
	return func() tea.Msg {
		env, err := planner.DetectEnvironment()
		if err != nil {
			return assetOverrideFixMsg{err: err}
		}
		fix, err := plugins.FixPlatformAssetOverride(env, game)
		return assetOverrideFixMsg{fix: fix, err: err}
	}
}

func undoPlatformAssetOverrideFix(game plugins.SteamGame) tea.Cmd {
	return func() tea.Msg {
		env, err := planner.DetectEnvironment()
		if err != nil {
			return assetOverrideFixMsg{err: err}
		}
		fix, err := plugins.UndoPlatformAssetOverrideFix(env, game)
		return assetOverrideFixMsg{fix: fix, err: err}
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
