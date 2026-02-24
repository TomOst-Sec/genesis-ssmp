package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/genesis-ssmp/genesis/cli/internal/app"
	"github.com/genesis-ssmp/genesis/cli/internal/config"
	"github.com/genesis-ssmp/genesis/cli/internal/detect"
	genesispkg "github.com/genesis-ssmp/genesis/cli/internal/genesis"
	"github.com/genesis-ssmp/genesis/cli/internal/llm/agent"
	"github.com/genesis-ssmp/genesis/cli/internal/llm/models"
	"github.com/genesis-ssmp/genesis/cli/internal/logging"
	"github.com/genesis-ssmp/genesis/cli/internal/permission"
	"github.com/genesis-ssmp/genesis/cli/internal/pubsub"
	"github.com/genesis-ssmp/genesis/cli/internal/session"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/brand"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/components/chat"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/components/core"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/components/dialog"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/components/sidebar"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/components/toast"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/layout"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/page"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/theme"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/util"
	"github.com/genesis-ssmp/genesis/cli/internal/version"
)

type keyMap struct {
	Logs          key.Binding
	Quit          key.Binding
	Help          key.Binding
	SwitchSession key.Binding
	Commands      key.Binding
	Filepicker    key.Binding
	Models        key.Binding
	SwitchTheme   key.Binding
	Sidebar       key.Binding
	NewSession    key.Binding
}

type startCompactSessionMsg struct{}

// Internal messages for slash command triggers
type openModelDialogMsg struct{}
type openGodModelDialogMsg struct{}
type openAngelsDialogMsg struct{}
type openThemeDialogMsg struct{}
type openHelpDialogMsg struct{}
type openSessionDialogMsg struct{}

const (
	quitKey = "q"
)

var keys = keyMap{
	Logs: key.NewBinding(
		key.WithKeys("ctrl+l"),
		key.WithHelp("ctrl+l", "logs"),
	),

	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("ctrl+_", "ctrl+h"),
		key.WithHelp("ctrl+?", "toggle help"),
	),

	SwitchSession: key.NewBinding(
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "switch session"),
	),

	Commands: key.NewBinding(
		key.WithKeys("ctrl+k"),
		key.WithHelp("ctrl+k", "commands"),
	),
	Filepicker: key.NewBinding(
		key.WithKeys("ctrl+f"),
		key.WithHelp("ctrl+f", "select files to upload"),
	),
	Models: key.NewBinding(
		key.WithKeys("ctrl+o"),
		key.WithHelp("ctrl+o", "model selection"),
	),

	SwitchTheme: key.NewBinding(
		key.WithKeys("ctrl+t"),
		key.WithHelp("ctrl+t", "switch theme"),
	),
	Sidebar: key.NewBinding(
		key.WithKeys("ctrl+b"),
		key.WithHelp("ctrl+b", "toggle sidebar"),
	),
	NewSession: key.NewBinding(
		key.WithKeys("ctrl+n"),
		key.WithHelp("ctrl+n", "new session"),
	),
}

var helpEsc = key.NewBinding(
	key.WithKeys("?"),
	key.WithHelp("?", "toggle help"),
)

var returnKey = key.NewBinding(
	key.WithKeys("esc"),
	key.WithHelp("esc", "close"),
)

var logsKeyReturnKey = key.NewBinding(
	key.WithKeys("esc", "backspace", quitKey),
	key.WithHelp("esc/q", "go back"),
)

type appModel struct {
	width, height   int
	currentPage     page.PageID
	previousPage    page.PageID
	pages           map[page.PageID]tea.Model
	loadedPages     map[page.PageID]bool
	status          core.StatusCmp
	app             *app.App
	selectedSession session.Session

	showPermissions bool
	permissions     dialog.PermissionDialogCmp

	showHelp bool
	help     dialog.HelpCmp

	showQuit bool
	quit     dialog.QuitDialog

	showSessionDialog bool
	sessionDialog     dialog.SessionDialog

	showCommandDialog bool
	commandDialog     dialog.CommandDialog
	commands          []dialog.Command

	showModelDialog bool
	modelDialog     dialog.ModelDialog

	showInitDialog bool
	initDialog     dialog.InitDialogCmp

	showFilepicker bool
	filepicker     dialog.FilepickerCmp

	showThemeDialog bool
	themeDialog     dialog.ThemeDialog

	showMultiArgumentsDialog bool
	multiArgumentsDialog     dialog.MultiArgumentsDialogCmp

	showSwarmDialog bool
	swarmDialog     dialog.SwarmAgentDialog

	showRoleModelDialog bool
	roleModelDialog     dialog.RoleModelDialog

	showAuthDialog bool
	authDialog     dialog.AuthDialog

	showWelcomeDialog bool
	welcomeDialog     dialog.WelcomeDialog

	showSidebar bool
	sidebarCmp  sidebar.FileTreeCmp

	toastCmp toast.ToastCmp

	genesisClient *genesispkg.Client

	isCompacting      bool
	compactingMessage string
}

func (a appModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmd := a.pages[a.currentPage].Init()
	a.loadedPages[a.currentPage] = true
	cmds = append(cmds, cmd)
	cmd = a.status.Init()
	cmds = append(cmds, cmd)
	cmd = a.quit.Init()
	cmds = append(cmds, cmd)
	cmd = a.help.Init()
	cmds = append(cmds, cmd)
	cmd = a.sessionDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.commandDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.modelDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.initDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.filepicker.Init()
	cmds = append(cmds, cmd)
	cmd = a.themeDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.swarmDialog.Init()
	cmds = append(cmds, cmd)

	// Check if we should show the welcome dialog (first run ever)
	cmds = append(cmds, func() tea.Msg {
		if config.ShouldShowWelcome() {
			return dialog.ShowWelcomeDialogMsg{Show: true}
		}
		return nil
	})

	// Check if we should show the init dialog
	cmds = append(cmds, func() tea.Msg {
		shouldShow, err := config.ShouldShowInitDialog()
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  "Failed to check init status: " + err.Error(),
			}
		}
		return dialog.ShowInitDialogMsg{Show: shouldShow}
	})

	// Auto-show auth dialog if no provider is configured
	if config.NeedsAuth() {
		cmds = append(cmds, func() tea.Msg {
			return genesispkg.OpenAuthDialogMsg{}
		})
	}

	return tea.Batch(cmds...)
}

func (a appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		msg.Height -= 1 // Make space for the status bar
		a.width, a.height = msg.Width, msg.Height

		s, _ := a.status.Update(msg)
		a.status = s.(core.StatusCmp)
		a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
		cmds = append(cmds, cmd)

		prm, permCmd := a.permissions.Update(msg)
		a.permissions = prm.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permCmd)

		help, helpCmd := a.help.Update(msg)
		a.help = help.(dialog.HelpCmp)
		cmds = append(cmds, helpCmd)

		session, sessionCmd := a.sessionDialog.Update(msg)
		a.sessionDialog = session.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)

		command, commandCmd := a.commandDialog.Update(msg)
		a.commandDialog = command.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)

		filepicker, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = filepicker.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

		a.initDialog.SetSize(msg.Width, msg.Height)

		if a.showSidebar {
			a.sidebarCmp.SetSize(30, msg.Height)
		}
		a.toastCmp.SetSize(msg.Width, msg.Height)

		if a.showMultiArgumentsDialog {
			a.multiArgumentsDialog.SetSize(msg.Width, msg.Height)
			args, argsCmd := a.multiArgumentsDialog.Update(msg)
			a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
			cmds = append(cmds, argsCmd, a.multiArgumentsDialog.Init())
		}

		return a, tea.Batch(cmds...)
	// Status
	case util.InfoMsg:
		s, cmd := a.status.Update(msg)
		a.status = s.(core.StatusCmp)
		cmds = append(cmds, cmd)
		return a, tea.Batch(cmds...)
	case pubsub.Event[logging.LogMessage]:
		if msg.Payload.Persist {
			switch msg.Payload.Level {
			case "error":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeError,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			case "info":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)

			case "warn":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeWarn,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})

				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			default:
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			}
		}
	case util.ClearStatusMsg:
		s, _ := a.status.Update(msg)
		a.status = s.(core.StatusCmp)

	// Permission
	case pubsub.Event[permission.PermissionRequest]:
		a.showPermissions = true
		return a, a.permissions.SetPermissions(msg.Payload)
	case dialog.PermissionResponseMsg:
		var cmd tea.Cmd
		switch msg.Action {
		case dialog.PermissionAllow:
			a.app.Permissions.Grant(msg.Permission)
		case dialog.PermissionAllowForSession:
			a.app.Permissions.GrantPersistant(msg.Permission)
		case dialog.PermissionDeny:
			a.app.Permissions.Deny(msg.Permission)
		}
		a.showPermissions = false
		return a, cmd

	case page.PageChangeMsg:
		return a, a.moveToPage(msg.ID)

	case dialog.CloseQuitMsg:
		a.showQuit = false
		return a, nil

	case dialog.CloseSessionDialogMsg:
		a.showSessionDialog = false
		return a, nil

	case dialog.OpenCommandDialogMsg:
		if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog {
			if len(a.commands) == 0 {
				return a, util.ReportWarn("No commands available")
			}
			a.commandDialog.SetCommands(a.commands)
			a.showCommandDialog = true
		}
		return a, nil

	case dialog.CloseCommandDialogMsg:
		a.showCommandDialog = false
		return a, nil

	case startCompactSessionMsg:
		// Start compacting the current session
		a.isCompacting = true
		a.compactingMessage = "Starting summarization..."

		if a.selectedSession.ID == "" {
			a.isCompacting = false
			return a, util.ReportWarn("No active session to summarize")
		}

		// Start the summarization process
		return a, func() tea.Msg {
			ctx := context.Background()
			a.app.CoderAgent.Summarize(ctx, a.selectedSession.ID)
			return nil
		}

	case pubsub.Event[agent.AgentEvent]:
		payload := msg.Payload
		if payload.Error != nil {
			a.isCompacting = false
			return a, util.ReportError(payload.Error)
		}

		a.compactingMessage = payload.Progress

		if payload.Done && payload.Type == agent.AgentEventTypeSummarize {
			a.isCompacting = false
			return a, util.ReportInfo("Session summarization complete")
		} else if payload.Done && payload.Type == agent.AgentEventTypeResponse && a.selectedSession.ID != "" {
			model := a.app.CoderAgent.Model()
			contextWindow := model.ContextWindow
			tokens := a.selectedSession.CompletionTokens + a.selectedSession.PromptTokens
			if (tokens >= int64(float64(contextWindow)*0.95)) && config.Get().AutoCompact {
				return a, util.CmdHandler(startCompactSessionMsg{})
			}
		}
		// Continue listening for events
		return a, nil

	case dialog.CloseThemeDialogMsg:
		a.showThemeDialog = false
		return a, nil

	case dialog.ThemeChangedMsg:
		a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
		a.showThemeDialog = false
		return a, tea.Batch(cmd, util.ReportInfo("Theme changed to: "+msg.ThemeName))

	case dialog.CloseModelDialogMsg:
		a.showModelDialog = false
		return a, nil

	case dialog.ModelSelectedMsg:
		a.showModelDialog = false

		model, err := a.app.CoderAgent.Update(config.AgentCoder, msg.Model.ID)
		if err != nil {
			return a, util.ReportError(err)
		}

		return a, util.ReportInfo(fmt.Sprintf("Model changed to %s", model.Name))

	case dialog.ShowInitDialogMsg:
		a.showInitDialog = msg.Show
		return a, nil

	case dialog.CloseInitDialogMsg:
		a.showInitDialog = false
		if msg.Initialize {
			// Run the initialization command
			for _, cmd := range a.commands {
				if cmd.ID == "init" {
					// Mark the project as initialized
					if err := config.MarkProjectInitialized(); err != nil {
						return a, util.ReportError(err)
					}
					return a, cmd.Handler(cmd)
				}
			}
		} else {
			// Mark the project as initialized without running the command
			if err := config.MarkProjectInitialized(); err != nil {
				return a, util.ReportError(err)
			}
		}
		return a, nil

	case chat.SessionSelectedMsg:
		a.selectedSession = msg
		a.sessionDialog.SetSelectedSession(msg.ID)

	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == a.selectedSession.ID {
			a.selectedSession = msg.Payload
		}
	case dialog.SessionSelectedMsg:
		a.showSessionDialog = false
		if a.currentPage == page.ChatPage {
			return a, util.CmdHandler(chat.SessionSelectedMsg(msg.Session))
		}
		return a, nil

	case dialog.CommandSelectedMsg:
		a.showCommandDialog = false
		// Execute the command handler if available
		if msg.Command.Handler != nil {
			return a, msg.Command.Handler(msg.Command)
		}
		return a, util.ReportInfo("Command selected: " + msg.Command.Title)

	case dialog.ShowMultiArgumentsDialogMsg:
		// Show multi-arguments dialog
		a.multiArgumentsDialog = dialog.NewMultiArgumentsDialogCmp(msg.CommandID, msg.Content, msg.ArgNames)
		a.showMultiArgumentsDialog = true
		return a, a.multiArgumentsDialog.Init()

	case dialog.CloseMultiArgumentsDialogMsg:
		// Close multi-arguments dialog
		a.showMultiArgumentsDialog = false

		// If submitted, replace all named arguments and run the command
		if msg.Submit {
			content := msg.Content

			// Replace each named argument with its value
			for name, value := range msg.Args {
				placeholder := "$" + name
				content = strings.ReplaceAll(content, placeholder, value)
			}

			// Execute the command with arguments
			return a, util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: content,
				Args:    msg.Args,
			})
		}
		return a, nil

	// Genesis: Swarm Agent Dialog
	case genesispkg.OpenSwarmAgentDialogMsg:
		if a.genesisClient != nil {
			a.showSwarmDialog = true
			// Fetch agents from Heaven status
			return a, func() tea.Msg {
				status, err := a.genesisClient.Status()
				if err != nil {
					return util.InfoMsg{Type: util.InfoTypeError, Msg: fmt.Sprintf("SwarmAgentList: %v", err)}
				}
				var agents []dialog.SwarmAgent
				for _, lease := range status.Leases {
					agents = append(agents, dialog.SwarmAgent{
						MissionID: lease.MissionID,
						Goal:      lease.OwnerID,
						Status:    "active",
						Provider:  "heaven",
					})
				}
				return dialog.OpenSwarmAgentDialogMsg{}
			}
		}
		return a, util.ReportWarn("Genesis client not configured")

	case dialog.OpenSwarmAgentDialogMsg:
		a.showSwarmDialog = true
		return a, nil

	case dialog.CloseSwarmAgentDialogMsg:
		a.showSwarmDialog = false
		return a, nil

	case dialog.SwarmAgentSelectedMsg:
		a.showSwarmDialog = false
		return a, util.ReportInfo(fmt.Sprintf("Agent: %s — %s", msg.Agent.MissionID, msg.Agent.Goal))

	// Genesis: Role Model Dialog
	case genesispkg.OpenRoleModelDialogMsg:
		a.showRoleModelDialog = true
		return a, a.roleModelDialog.Init()

	case dialog.CloseRoleModelDialogMsg:
		a.showRoleModelDialog = false
		return a, nil

	case dialog.RoleModelSelectedMsg:
		a.showRoleModelDialog = false
		return a, util.ReportInfo(fmt.Sprintf("Role %s → model %s", msg.Role, msg.Model.Name))

	// Genesis: Auth Dialog
	case genesispkg.OpenAuthDialogMsg:
		a.showAuthDialog = true
		return a, a.authDialog.Init()

	case dialog.CloseAuthDialogMsg:
		a.showAuthDialog = false
		return a, nil

	case dialog.AuthCompleteMsg:
		a.showAuthDialog = false
		if msg.Error != nil {
			return a, util.ReportError(msg.Error)
		}
		// Save credential (API key or device flow token)
		credential := msg.APIKey
		if credential == "" {
			credential = msg.Token
		}
		if credential != "" {
			if err := config.SaveProviderAPIKey(msg.Provider, credential); err != nil {
				return a, util.ReportError(fmt.Errorf("failed to save credential: %w", err))
			}
		}
		// Initialize the coder agent now that we have a provider
		if err := a.app.InitCoderAgent(); err != nil {
			logging.Warn("Could not init agent after auth", "error", err)
		}
		return a, util.ReportInfo(fmt.Sprintf("Connected to %s", msg.Provider))

	// BlueGenesis: Welcome dialog
	case dialog.ShowWelcomeDialogMsg:
		a.showWelcomeDialog = msg.Show
		return a, nil

	case dialog.CloseWelcomeDialogMsg:
		a.showWelcomeDialog = false
		_ = config.MarkWelcomeShown()
		return a, nil

	// BlueGenesis: Sidebar toggle
	case sidebar.ToggleSidebarMsg:
		a.showSidebar = !a.showSidebar
		if a.showSidebar {
			a.sidebarCmp.SetSize(30, a.height)
			return a, a.sidebarCmp.Init()
		}
		return a, nil

	// BlueGenesis: Toast notifications
	case toast.ShowToastMsg:
		t, cmd := a.toastCmp.Update(msg)
		a.toastCmp = t.(toast.ToastCmp)
		return a, cmd

	// BlueGenesis: Slash command dialog triggers
	case openModelDialogMsg:
		a.showModelDialog = true
		return a, a.modelDialog.Init()
	case openGodModelDialogMsg:
		a.showModelDialog = true
		a.modelDialog.Init()
		a.modelDialog.SetProviderFilter([]models.ModelProvider{models.ProviderLocal})
		return a, nil
	case openAngelsDialogMsg:
		a.showModelDialog = true
		a.modelDialog.Init()
		a.modelDialog.SetProviderFilter([]models.ModelProvider{
			models.ProviderAnthropic,
			models.ProviderOpenAI,
			models.ProviderGemini,
			models.ProviderCopilot,
			models.ProviderGROQ,
			models.ProviderOpenRouter,
		})
		return a, nil
	case openThemeDialogMsg:
		a.showThemeDialog = true
		return a, a.themeDialog.Init()
	case openHelpDialogMsg:
		a.showHelp = !a.showHelp
		return a, nil
	case openSessionDialogMsg:
		a.showSessionDialog = true
		sessions, err := a.app.Sessions.List(context.Background())
		if err == nil {
			a.sessionDialog.SetSessions(sessions)
		}
		return a, nil

	case tea.KeyMsg:
		// If multi-arguments dialog is open, let it handle the key press first
		if a.showMultiArgumentsDialog {
			args, cmd := a.multiArgumentsDialog.Update(msg)
			a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
			return a, cmd
		}

		switch {

		case key.Matches(msg, keys.Quit):
			a.showQuit = !a.showQuit
			if a.showHelp {
				a.showHelp = false
			}
			if a.showSessionDialog {
				a.showSessionDialog = false
			}
			if a.showCommandDialog {
				a.showCommandDialog = false
			}
			if a.showFilepicker {
				a.showFilepicker = false
				a.filepicker.ToggleFilepicker(a.showFilepicker)
			}
			if a.showModelDialog {
				a.showModelDialog = false
			}
			if a.showMultiArgumentsDialog {
				a.showMultiArgumentsDialog = false
			}
			if a.showSidebar {
				a.showSidebar = false
			}
			return a, nil
		case key.Matches(msg, keys.SwitchSession):
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showCommandDialog {
				// Load sessions and show the dialog
				sessions, err := a.app.Sessions.List(context.Background())
				if err != nil {
					return a, util.ReportError(err)
				}
				if len(sessions) == 0 {
					return a, util.ReportWarn("No sessions available")
				}
				a.sessionDialog.SetSessions(sessions)
				a.showSessionDialog = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.Commands):
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showThemeDialog && !a.showFilepicker {
				// Show commands dialog
				if len(a.commands) == 0 {
					return a, util.ReportWarn("No commands available")
				}
				a.commandDialog.SetCommands(a.commands)
				a.showCommandDialog = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.Models):
			if a.showModelDialog {
				a.showModelDialog = false
				return a, nil
			}
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
				a.showModelDialog = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.SwitchTheme):
			if !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
				// Show theme switcher dialog
				a.showThemeDialog = true
				// Theme list is dynamically loaded by the dialog component
				return a, a.themeDialog.Init()
			}
			return a, nil
		case key.Matches(msg, returnKey) || key.Matches(msg):
			if msg.String() == quitKey {
				if a.currentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
			} else if !a.filepicker.IsCWDFocused() {
				if a.showQuit {
					a.showQuit = !a.showQuit
					return a, nil
				}
				if a.showHelp {
					a.showHelp = !a.showHelp
					return a, nil
				}
				if a.showInitDialog {
					a.showInitDialog = false
					// Mark the project as initialized without running the command
					if err := config.MarkProjectInitialized(); err != nil {
						return a, util.ReportError(err)
					}
					return a, nil
				}
				if a.showFilepicker {
					a.showFilepicker = false
					a.filepicker.ToggleFilepicker(a.showFilepicker)
					return a, nil
				}
				if a.currentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
			}
		case key.Matches(msg, keys.Logs):
			return a, a.moveToPage(page.LogsPage)
		case key.Matches(msg, keys.Help):
			if a.showQuit {
				return a, nil
			}
			a.showHelp = !a.showHelp
			return a, nil
		case key.Matches(msg, helpEsc):
			if a.app.CoderAgent.IsBusy() {
				if a.showQuit {
					return a, nil
				}
				a.showHelp = !a.showHelp
				return a, nil
			}
		case key.Matches(msg, keys.Filepicker):
			a.showFilepicker = !a.showFilepicker
			a.filepicker.ToggleFilepicker(a.showFilepicker)
			return a, nil
		case key.Matches(msg, keys.Sidebar):
			a.showSidebar = !a.showSidebar
			if a.showSidebar {
				a.sidebarCmp.SetSize(30, a.height)
			}
			return a, nil
		case key.Matches(msg, keys.NewSession):
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions {
				return a, func() tea.Msg {
					sess, err := a.app.Sessions.Create(context.Background(), "New Session")
					if err != nil {
						return util.InfoMsg{Type: util.InfoTypeError, Msg: "Failed to create session: " + err.Error()}
					}
					return chat.SessionSelectedMsg(sess)
				}
			}
			return a, nil
		}
	default:
		f, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

	}

	if a.showFilepicker {
		f, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showQuit {
		q, quitCmd := a.quit.Update(msg)
		a.quit = q.(dialog.QuitDialog)
		cmds = append(cmds, quitCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}
	if a.showPermissions {
		d, permissionsCmd := a.permissions.Update(msg)
		a.permissions = d.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permissionsCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showSessionDialog {
		d, sessionCmd := a.sessionDialog.Update(msg)
		a.sessionDialog = d.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showCommandDialog {
		d, commandCmd := a.commandDialog.Update(msg)
		a.commandDialog = d.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showModelDialog {
		d, modelCmd := a.modelDialog.Update(msg)
		a.modelDialog = d.(dialog.ModelDialog)
		cmds = append(cmds, modelCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showInitDialog {
		d, initCmd := a.initDialog.Update(msg)
		a.initDialog = d.(dialog.InitDialogCmp)
		cmds = append(cmds, initCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showThemeDialog {
		d, themeCmd := a.themeDialog.Update(msg)
		a.themeDialog = d.(dialog.ThemeDialog)
		cmds = append(cmds, themeCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showSwarmDialog {
		d, swarmCmd := a.swarmDialog.Update(msg)
		a.swarmDialog = d.(dialog.SwarmAgentDialog)
		cmds = append(cmds, swarmCmd)
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showRoleModelDialog {
		d, roleCmd := a.roleModelDialog.Update(msg)
		a.roleModelDialog = d.(dialog.RoleModelDialog)
		cmds = append(cmds, roleCmd)
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showAuthDialog {
		d, authCmd := a.authDialog.Update(msg)
		a.authDialog = d.(dialog.AuthDialog)
		cmds = append(cmds, authCmd)
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showWelcomeDialog {
		d, welcomeCmd := a.welcomeDialog.Update(msg)
		a.welcomeDialog = d.(dialog.WelcomeDialog)
		cmds = append(cmds, welcomeCmd)
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showSidebar {
		d, sidebarCmd := a.sidebarCmp.Update(msg)
		a.sidebarCmp = d.(sidebar.FileTreeCmp)
		cmds = append(cmds, sidebarCmd)
	}

	// Toast always receives messages for dismiss timers
	t, toastCmd := a.toastCmp.Update(msg)
	a.toastCmp = t.(toast.ToastCmp)
	cmds = append(cmds, toastCmd)

	s, _ := a.status.Update(msg)
	a.status = s.(core.StatusCmp)
	a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
	cmds = append(cmds, cmd)
	return a, tea.Batch(cmds...)
}

// RegisterCommand adds a command to the command dialog
func (a *appModel) RegisterCommand(cmd dialog.Command) {
	a.commands = append(a.commands, cmd)
}

func (a *appModel) findCommand(id string) (dialog.Command, bool) {
	for _, cmd := range a.commands {
		if cmd.ID == id {
			return cmd, true
		}
	}
	return dialog.Command{}, false
}

func (a *appModel) moveToPage(pageID page.PageID) tea.Cmd {
	if a.app.CoderAgent.IsBusy() {
		// For now we don't move to any page if the agent is busy
		return util.ReportWarn("Agent is busy, please wait...")
	}

	var cmds []tea.Cmd
	if _, ok := a.loadedPages[pageID]; !ok {
		cmd := a.pages[pageID].Init()
		cmds = append(cmds, cmd)
		a.loadedPages[pageID] = true
	}
	a.previousPage = a.currentPage
	a.currentPage = pageID
	if sizable, ok := a.pages[a.currentPage].(layout.Sizeable); ok {
		cmd := sizable.SetSize(a.width, a.height)
		cmds = append(cmds, cmd)
	}

	return tea.Batch(cmds...)
}

func (a appModel) View() string {
	pageView := a.pages[a.currentPage].View()

	// Render sidebar alongside page content if visible
	if a.showSidebar {
		sidebarView := a.sidebarCmp.View()
		pageView = lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, pageView)
	}

	components := []string{pageView}
	components = append(components, a.status.View())

	appView := lipgloss.JoinVertical(lipgloss.Top, components...)

	if a.showPermissions {
		overlay := a.permissions.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showFilepicker {
		overlay := a.filepicker.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)

	}

	// Show compacting status overlay
	if a.isCompacting {
		t := theme.CurrentTheme()
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.BorderFocused()).
			BorderBackground(t.Background()).
			Padding(1, 2).
			Background(t.Background()).
			Foreground(t.Text())

		overlay := style.Render("Summarizing\n" + a.compactingMessage)
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showHelp {
		bindings := layout.KeyMapToSlice(keys)
		if p, ok := a.pages[a.currentPage].(layout.Bindings); ok {
			bindings = append(bindings, p.BindingKeys()...)
		}
		if a.showPermissions {
			bindings = append(bindings, a.permissions.BindingKeys()...)
		}
		if a.currentPage == page.LogsPage {
			bindings = append(bindings, logsKeyReturnKey)
		}
		if !a.app.CoderAgent.IsBusy() {
			bindings = append(bindings, helpEsc)
		}
		a.help.SetBindings(bindings)

		overlay := a.help.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showQuit {
		overlay := a.quit.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showSessionDialog {
		overlay := a.sessionDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showModelDialog {
		overlay := a.modelDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showCommandDialog {
		overlay := a.commandDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showInitDialog {
		overlay := a.initDialog.View()
		appView = layout.PlaceOverlay(
			a.width/2-lipgloss.Width(overlay)/2,
			a.height/2-lipgloss.Height(overlay)/2,
			overlay,
			appView,
			true,
		)
	}

	if a.showThemeDialog {
		overlay := a.themeDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showMultiArgumentsDialog {
		overlay := a.multiArgumentsDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showSwarmDialog {
		overlay := a.swarmDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(col, row, overlay, appView, true)
	}

	if a.showRoleModelDialog {
		overlay := a.roleModelDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(col, row, overlay, appView, true)
	}

	if a.showAuthDialog {
		overlay := a.authDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(col, row, overlay, appView, true)
	}

	if a.showWelcomeDialog {
		overlay := a.welcomeDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(col, row, overlay, appView, true)
	}

	// Toast notifications rendered in bottom-right corner
	toastView := a.toastCmp.View()
	if toastView != "" {
		row := lipgloss.Height(appView) - lipgloss.Height(toastView) - 2
		col := lipgloss.Width(appView) - lipgloss.Width(toastView) - 2
		if row < 0 {
			row = 0
		}
		if col < 0 {
			col = 0
		}
		appView = layout.PlaceOverlay(col, row, toastView, appView, false)
	}

	return appView
}

func New(app *app.App) tea.Model {
	startPage := page.ChatPage
	// Determine Heaven address from config
	cfg := config.Get()
	heavenAddr := "127.0.0.1:4444"
	if cfg != nil && cfg.Genesis.HeavenAddr != "" {
		heavenAddr = cfg.Genesis.HeavenAddr
	}
	genesisClient := genesispkg.NewClient(heavenAddr)

	model := &appModel{
		currentPage:     startPage,
		loadedPages:     make(map[page.PageID]bool),
		status:          core.NewStatusCmp(app.LSPClients),
		help:            dialog.NewHelpCmp(),
		quit:            dialog.NewQuitCmp(),
		sessionDialog:   dialog.NewSessionDialogCmp(),
		commandDialog:   dialog.NewCommandDialogCmp(),
		modelDialog:     dialog.NewModelDialogCmp(),
		permissions:     dialog.NewPermissionDialogCmp(),
		initDialog:      dialog.NewInitDialogCmp(),
		themeDialog:     dialog.NewThemeDialogCmp(),
		swarmDialog:     dialog.NewSwarmAgentDialogCmp(),
		roleModelDialog: dialog.NewRoleModelDialogCmp(),
		authDialog:      dialog.NewAuthDialogCmp(),
		welcomeDialog:   dialog.NewWelcomeDialogCmp(),
		sidebarCmp:      sidebar.NewFileTreeCmp(),
		toastCmp:        toast.NewToastCmp(),
		genesisClient:   genesisClient,
		app:             app,
		commands:        []dialog.Command{},
		pages: map[page.PageID]tea.Model{
			page.ChatPage: page.NewChatPage(app),
			page.LogsPage: page.NewLogsPage(),
		},
		filepicker: dialog.NewFilepickerCmp(app),
	}

	model.RegisterCommand(dialog.Command{
		ID:          "init",
		Title:       "Initialize Project",
		Description: "Create/Update the Genesis.md memory file",
		Handler: func(cmd dialog.Command) tea.Cmd {
			prompt := `Please analyze this codebase and create a Genesis.md file containing:
1. Build/lint/test commands - especially for running a single test
2. Code style guidelines including imports, formatting, types, naming conventions, error handling, etc.

The file you create will be given to agentic coding agents (such as yourself) that operate in this repository. Make it about 20 lines long.
If there's already a genesis.md, improve it.
If there are Cursor rules (in .cursor/rules/ or .cursorrules) or Copilot rules (in .github/copilot-instructions.md), make sure to include them.`
			return tea.Batch(
				util.CmdHandler(chat.SendMsg{
					Text: prompt,
				}),
			)
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "compact",
		Title:       "Compact Session",
		Description: "Summarize the current session and create a new one with the summary",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				return startCompactSessionMsg{}
			}
		},
	})
	// --- BlueGenesis Extended Commands ---

	// /about — Show BlueGenesis version, logo, system info
	model.RegisterCommand(dialog.Command{
		ID:          "about",
		Title:       "About",
		Description: "Show BlueGenesis version, logo, and system info",
		Handler: func(cmd dialog.Command) tea.Cmd {
			cwd, _ := os.Getwd()
			proj := detect.DetectProject(cwd)
			modelID := config.Get().Agents[config.AgentCoder].Model
			m := models.SupportedModels[modelID]
			info := fmt.Sprintf("%s\n  Runtime: %s %s/%s\n  Model: %s (%s)\n  Project: %s\n  CWD: %s",
				brand.RenderLogoWithVersion(),
				runtime.Version(), runtime.GOOS, runtime.GOARCH,
				m.Name, m.Provider,
				proj.Type.Badge(),
				cwd,
			)
			return util.CmdHandler(chat.SendMsg{Text: info})
		},
	})

	// /clear — Clear current conversation (starts new session)
	model.RegisterCommand(dialog.Command{
		ID:          "clear",
		Title:       "Clear",
		Description: "Start a fresh session (keep files)",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.ReportInfo("Use ctrl+s to switch to a new session")
		},
	})

	// /diff — Show git diff of current session changes
	model.RegisterCommand(dialog.Command{
		ID:          "diff",
		Title:       "Diff",
		Description: "Show git diff of current session changes",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				out, err := exec.Command("git", "diff").Output()
				if err != nil {
					return util.InfoMsg{Type: util.InfoTypeError, Msg: fmt.Sprintf("git diff: %v", err)}
				}
				diff := string(out)
				if diff == "" {
					diff = "No changes detected."
				}
				return chat.SendMsg{Text: "```diff\n" + diff + "\n```"}
			}
		},
	})

	// /test — Run project test suite (auto-detect)
	model.RegisterCommand(dialog.Command{
		ID:          "test",
		Title:       "Test",
		Description: "Run project test suite (auto-detect: go test, npm test, pytest, cargo test)",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				cwd, _ := os.Getwd()
				proj := detect.DetectProject(cwd)
				if proj.TestCommand == "" {
					return util.InfoMsg{Type: util.InfoTypeWarn, Msg: "Could not detect test command for this project"}
				}
				return chat.SendMsg{Text: fmt.Sprintf("Please run `%s` and report the results.", proj.TestCommand)}
			}
		},
	})

	// /lint — Run linter (auto-detect)
	model.RegisterCommand(dialog.Command{
		ID:          "lint",
		Title:       "Lint",
		Description: "Run linter (auto-detect: golangci-lint, eslint, ruff, clippy)",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				cwd, _ := os.Getwd()
				proj := detect.DetectProject(cwd)
				if proj.LintCommand == "" {
					return util.InfoMsg{Type: util.InfoTypeWarn, Msg: "Could not detect lint command for this project"}
				}
				return chat.SendMsg{Text: fmt.Sprintf("Please run `%s` and fix any issues found.", proj.LintCommand)}
			}
		},
	})

	// /build — Run build command (auto-detect)
	model.RegisterCommand(dialog.Command{
		ID:          "build",
		Title:       "Build",
		Description: "Run build command (auto-detect from project type)",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				cwd, _ := os.Getwd()
				proj := detect.DetectProject(cwd)
				if proj.BuildCommand == "" {
					return util.InfoMsg{Type: util.InfoTypeWarn, Msg: "Could not detect build command for this project"}
				}
				return chat.SendMsg{Text: fmt.Sprintf("Please run `%s` and report the results.", proj.BuildCommand)}
			}
		},
	})

	// /git <args> — Run git command with output in chat
	model.RegisterCommand(dialog.Command{
		ID:          "git",
		Title:       "Git",
		Description: "Run a git command with output in chat",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(chat.SendMsg{
				Text: "Please run the git command I specify and show the output.",
			})
		},
	})

	// /export — Export session as markdown
	model.RegisterCommand(dialog.Command{
		ID:          "export",
		Title:       "Export",
		Description: "Export current session as markdown",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(chat.SendMsg{
				Text: "Please export this entire conversation as a well-formatted markdown document.",
			})
		},
	})

	// /model — Quick model switch (all providers)
	model.RegisterCommand(dialog.Command{
		ID:          "model",
		Title:       "Model",
		Description: "Quick model switch (all providers)",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(openModelDialogMsg{})
		},
	})

	// /godmodel — Select local God model (Qwen, Ollama, LM Studio)
	model.RegisterCommand(dialog.Command{
		ID:          "godmodel",
		Title:       "God Model",
		Description: "Select local God model (Qwen, Ollama, LM Studio)",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(openGodModelDialogMsg{})
		},
	})

	// /angels — Select cloud Angel model (Opus 4.6, Codex 5.2/5.3, etc.)
	model.RegisterCommand(dialog.Command{
		ID:          "angels",
		Title:       "Angels",
		Description: "Select cloud Angel model (Opus 4.6, Codex 5.2/5.3, Gemini, etc.)",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(openAngelsDialogMsg{})
		},
	})

	// /theme — Quick theme switch
	model.RegisterCommand(dialog.Command{
		ID:          "theme",
		Title:       "Theme",
		Description: "Quick theme switch (opens theme selector)",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(openThemeDialogMsg{})
		},
	})

	// /provider — Show provider status and costs
	model.RegisterCommand(dialog.Command{
		ID:          "provider",
		Title:       "Provider",
		Description: "Show provider status and cost information",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				cfg := config.Get()
				var lines []string
				lines = append(lines, "**Provider Status:**")
				for provider, p := range cfg.Providers {
					status := "configured"
					if p.Disabled {
						status = "disabled"
					}
					if p.APIKey == "" {
						status = "no key"
					}
					lines = append(lines, fmt.Sprintf("  - **%s**: %s", provider, status))
				}
				return chat.SendMsg{Text: strings.Join(lines, "\n")}
			}
		},
	})

	// /cost — Show session token usage and cost
	model.RegisterCommand(dialog.Command{
		ID:          "cost",
		Title:       "Cost",
		Description: "Show current session token usage and cost breakdown",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				cfg := config.Get()
				modelID := cfg.Agents[config.AgentCoder].Model
				m := models.SupportedModels[modelID]
				return util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg: fmt.Sprintf("Model: %s | Context: %dK | Cost/1M in: $%.2f | Cost/1M out: $%.2f",
						m.Name, m.ContextWindow/1000, m.CostPer1MIn, m.CostPer1MOut),
				}
			}
		},
	})

	// /help — Enhanced help
	model.RegisterCommand(dialog.Command{
		ID:          "help",
		Title:       "Help",
		Description: "Show keyboard shortcuts and help",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(openHelpDialogMsg{})
		},
	})

	// /keybindings — Show all keyboard shortcuts
	model.RegisterCommand(dialog.Command{
		ID:          "keybindings",
		Title:       "Keybindings",
		Description: "Show all keyboard shortcuts",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(openHelpDialogMsg{})
		},
	})

	// /sessions — List all sessions
	model.RegisterCommand(dialog.Command{
		ID:          "sessions",
		Title:       "Sessions",
		Description: "List all sessions with previews",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(openSessionDialogMsg{})
		},
	})

	// /version — Show version info
	model.RegisterCommand(dialog.Command{
		ID:          "version",
		Title:       "Version",
		Description: "Show BlueGenesis version",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(util.InfoMsg{Type: util.InfoTypeInfo, Msg: "Genesis " + version.Version})
		},
	})

	// Register Genesis SSMP commands
	for _, cmd := range genesispkg.GenesisCommands(genesisClient) {
		model.RegisterCommand(cmd)
	}

	// Load custom commands
	customCommands, err := dialog.LoadCustomCommands()
	if err != nil {
		logging.Warn("Failed to load custom commands", "error", err)
	} else {
		for _, cmd := range customCommands {
			model.RegisterCommand(cmd)
		}
	}

	return model
}
