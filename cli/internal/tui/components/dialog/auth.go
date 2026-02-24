package dialog

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/genesis-ssmp/genesis/cli/internal/auth"
	"github.com/genesis-ssmp/genesis/cli/internal/llm/models"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/layout"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/styles"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/theme"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/util"
)

// AuthMethod represents the authentication method chosen.
type AuthMethod string

const (
	AuthMethodAPIKey     AuthMethod = "api_key"
	AuthMethodDeviceFlow AuthMethod = "device_flow"
	AuthMethodBridge     AuthMethod = "bridge_import"
)

// AuthCompleteMsg is sent when authentication is completed.
type AuthCompleteMsg struct {
	Provider string
	Method   AuthMethod
	APIKey   string
	Token    string
	Error    error
}

// CloseAuthDialogMsg signals closing the auth dialog.
type CloseAuthDialogMsg struct{}

// AuthDialog is the interface for the provider authentication dialog.
type AuthDialog interface {
	tea.Model
	layout.Bindings
}

// -- internal async messages --

type authValidationResultMsg struct {
	valid bool
	err   error
}

type deviceFlowCodeMsg struct {
	code *auth.DeviceCodeResponse
	err  error
}

type deviceFlowTokenMsg struct {
	token string
	err   error
}

type bridgeDetectMsg struct {
	info *auth.BridgeInfo
}

type bridgeImportMsg struct {
	result *auth.BridgeResult
	err    error
}

type bridgeLoginDoneMsg struct {
	err error
}

type bridgeValidateMsg struct {
	valid bool
	err   error
}

// -- phases --

type authPhase int

const (
	authPhaseProvider authPhase = iota
	authPhaseMethod
	authPhaseAPIKeyInput
	authPhaseAPIKeyValidating
	authPhaseDeviceFlow
	authPhaseBridgeWarn
	authPhaseBridgeDetect
	authPhaseBridgeImport
	authPhaseBridgeValidating
	authPhaseBridgeLogin
	authPhaseDone
)

// -- method entry for rendering --

type methodEntry struct {
	method auth.AuthMethod
	config auth.MethodConfig
}

type authDialogCmp struct {
	phase       authPhase
	providerIdx int
	methodIdx   int

	providers []auth.ProviderCapability
	methods   []methodEntry // computed for selected provider

	apiKeyInput textinput.Model
	resultMsg   string
	resultErr   bool
	validErr    string // validation error to show on api key input
	width       int
	height      int
	urlOpened   bool

	// device flow state
	deviceCode *auth.DeviceCodeResponse
	pollCancel context.CancelFunc

	// bridge state
	bridgeInfo   *auth.BridgeInfo
	bridgeResult *auth.BridgeResult
}

type authKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Escape key.Binding
	Tab    key.Binding
}

var authKeys = authKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("up/k", "previous"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("down/j", "next"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "open key page"),
	),
}

func (a *authDialogCmp) Init() tea.Cmd {
	return textinput.Blink
}

func (a *authDialogCmp) selectedProvider() auth.ProviderCapability {
	return a.providers[a.providerIdx]
}

func (a *authDialogCmp) selectedMethod() methodEntry {
	return a.methods[a.methodIdx]
}

func (a *authDialogCmp) buildMethods() {
	cap := a.selectedProvider()
	a.methods = nil
	for _, m := range auth.EnabledMethods(cap) {
		a.methods = append(a.methods, methodEntry{method: m.Method, config: m.Config})
	}
	a.methodIdx = 0
}

func (a *authDialogCmp) enterMethodPhase() (tea.Model, tea.Cmd) {
	a.buildMethods()
	// If only 1 enabled method, skip method selection
	enabledCount := 0
	enabledIdx := 0
	for i, m := range a.methods {
		if m.config.Enabled {
			enabledCount++
			enabledIdx = i
		}
	}
	if enabledCount == 1 {
		a.methodIdx = enabledIdx
		return a.activateMethod()
	}
	a.phase = authPhaseMethod
	return a, nil
}

func (a *authDialogCmp) activateMethod() (tea.Model, tea.Cmd) {
	m := a.selectedMethod()
	if !m.config.Enabled {
		return a, nil
	}

	switch m.method {
	case auth.MethodAPIKey:
		a.phase = authPhaseAPIKeyInput
		a.apiKeyInput.Placeholder = m.config.KeyHint
		if a.apiKeyInput.Placeholder == "" {
			a.apiKeyInput.Placeholder = "Paste API key..."
		}
		a.apiKeyInput.SetValue("")
		a.apiKeyInput.Focus()
		a.urlOpened = false
		a.validErr = ""
		return a, textinput.Blink

	case auth.MethodDeviceFlow:
		if m.config.ClientID == "" {
			a.resultMsg = "Device flow unavailable: no client_id configured.\nRegister a GitHub OAuth App and set its client_id."
			a.resultErr = true
			a.phase = authPhaseDone
			return a, nil
		}
		a.phase = authPhaseDeviceFlow
		a.deviceCode = nil
		cfg := m.config
		return a, func() tea.Msg {
			code, err := auth.RequestDeviceCode(context.Background(), cfg)
			return deviceFlowCodeMsg{code: code, err: err}
		}

	case auth.MethodBridgeImport:
		a.bridgeInfo = nil
		a.bridgeResult = nil
		// Unsupported bridges need explicit user acknowledgement
		if m.config.Bridge != nil && m.config.Bridge.SupportLevel == "unsupported" {
			a.phase = authPhaseBridgeWarn
			return a, nil
		}
		a.phase = authPhaseBridgeDetect
		bridgeCfg := m.config.Bridge
		return a, func() tea.Msg {
			info := auth.DetectBridge(bridgeCfg)
			return bridgeDetectMsg{info: info}
		}
	}

	return a, nil
}

func (a *authDialogCmp) goBackFromMethod() {
	a.phase = authPhaseMethod
	if len(a.methods) <= 1 {
		a.phase = authPhaseProvider
	}
}

func (a *authDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch a.phase {
		case authPhaseProvider:
			switch {
			case key.Matches(msg, authKeys.Up):
				if a.providerIdx > 0 {
					a.providerIdx--
				}
			case key.Matches(msg, authKeys.Down):
				if a.providerIdx < len(a.providers)-1 {
					a.providerIdx++
				}
			case key.Matches(msg, authKeys.Enter):
				return a.enterMethodPhase()
			case key.Matches(msg, authKeys.Escape):
				return a, util.CmdHandler(CloseAuthDialogMsg{})
			}
			return a, nil

		case authPhaseMethod:
			switch {
			case key.Matches(msg, authKeys.Up):
				if a.methodIdx > 0 {
					a.methodIdx--
				}
			case key.Matches(msg, authKeys.Down):
				if a.methodIdx < len(a.methods)-1 {
					a.methodIdx++
				}
			case key.Matches(msg, authKeys.Enter):
				return a.activateMethod()
			case key.Matches(msg, authKeys.Escape):
				a.phase = authPhaseProvider
				return a, nil
			}
			return a, nil

		case authPhaseAPIKeyInput:
			switch {
			case key.Matches(msg, authKeys.Tab):
				m := a.selectedMethod()
				if m.config.KeyPageURL != "" {
					result := util.OpenURL(m.config.KeyPageURL)
					a.urlOpened = result.Opened
				}
				return a, nil
			case key.Matches(msg, authKeys.Enter):
				apiKey := a.apiKeyInput.Value()
				if apiKey == "" {
					return a, nil
				}
				a.phase = authPhaseAPIKeyValidating
				a.validErr = ""
				p := a.selectedProvider()
				m := a.selectedMethod()
				cfg := m.config
				return a, func() tea.Msg {
					result := auth.ValidateAPIKey(context.Background(), cfg, apiKey)
					if result.Valid {
						return AuthCompleteMsg{
							Provider: string(p.ProviderKey),
							Method:   AuthMethodAPIKey,
							APIKey:   apiKey,
						}
					}
					errMsg := "validation failed"
					if result.Error != nil {
						errMsg = result.Error.Error()
					}
					return authValidationResultMsg{valid: false, err: fmt.Errorf("%s", errMsg)}
				}
			case key.Matches(msg, authKeys.Escape):
				a.apiKeyInput.SetValue("")
				a.validErr = ""
				a.goBackFromMethod()
				return a, nil
			}
			var cmd tea.Cmd
			a.apiKeyInput, cmd = a.apiKeyInput.Update(msg)
			return a, cmd

		case authPhaseAPIKeyValidating:
			if key.Matches(msg, authKeys.Escape) {
				a.phase = authPhaseAPIKeyInput
				a.apiKeyInput.Focus()
				return a, textinput.Blink
			}
			return a, nil

		case authPhaseDeviceFlow:
			if key.Matches(msg, authKeys.Escape) {
				if a.pollCancel != nil {
					a.pollCancel()
				}
				a.goBackFromMethod()
				return a, nil
			}
			return a, nil

		case authPhaseBridgeWarn:
			switch {
			case key.Matches(msg, authKeys.Enter):
				// User accepted the warning — proceed to detect
				a.phase = authPhaseBridgeDetect
				m := a.selectedMethod()
				bridgeCfg := m.config.Bridge
				return a, func() tea.Msg {
					info := auth.DetectBridge(bridgeCfg)
					return bridgeDetectMsg{info: info}
				}
			case key.Matches(msg, authKeys.Escape):
				a.goBackFromMethod()
				return a, nil
			}
			return a, nil

		case authPhaseBridgeDetect:
			if key.Matches(msg, authKeys.Escape) {
				a.goBackFromMethod()
				return a, nil
			}
			return a, nil

		case authPhaseBridgeImport:
			if key.Matches(msg, authKeys.Enter) {
				if a.bridgeResult != nil {
					// Validate the imported token before accepting
					p := a.selectedProvider()
					apiKeyCfg, hasAPIKey := p.Methods[auth.MethodAPIKey]
					if hasAPIKey && apiKeyCfg.ValidationURL != "" && !strings.HasPrefix(a.bridgeResult.Token, "sk-ant-o") {
						a.phase = authPhaseBridgeValidating
						token := a.bridgeResult.Token
						return a, func() tea.Msg {
							result := auth.ValidateAPIKey(context.Background(), apiKeyCfg, token)
							return bridgeValidateMsg{valid: result.Valid, err: result.Error}
						}
					}
					// No validation endpoint — accept directly
					return a, util.CmdHandler(AuthCompleteMsg{
						Provider: string(p.ProviderKey),
						Method:   AuthMethodBridge,
						Token:    a.bridgeResult.Token,
					})
				}
			}
			if key.Matches(msg, authKeys.Escape) {
				a.goBackFromMethod()
				return a, nil
			}
			return a, nil

		case authPhaseBridgeValidating:
			if key.Matches(msg, authKeys.Escape) {
				a.phase = authPhaseBridgeImport
				return a, nil
			}
			return a, nil

		case authPhaseBridgeLogin:
			if key.Matches(msg, authKeys.Enter) {
				m := a.selectedMethod()
				if m.config.Bridge != nil {
					cmd, err := auth.BridgeLoginCmd(m.config.Bridge)
					if err != nil {
						a.resultMsg = fmt.Sprintf("Cannot launch CLI: %v", err)
						a.resultErr = true
						a.phase = authPhaseDone
						return a, nil
					}
					// tea.ExecProcess suspends TUI, gives CLI full terminal control
					return a, tea.ExecProcess(cmd, func(err error) tea.Msg {
						return bridgeLoginDoneMsg{err: err}
					})
				}
			}
			if key.Matches(msg, authKeys.Escape) {
				a.goBackFromMethod()
				return a, nil
			}
			return a, nil

		case authPhaseDone:
			if key.Matches(msg, authKeys.Enter) || key.Matches(msg, authKeys.Escape) {
				return a, util.CmdHandler(CloseAuthDialogMsg{})
			}
			return a, nil
		}

	// -- async message handlers --

	case authValidationResultMsg:
		if msg.valid {
			return a, nil
		}
		a.phase = authPhaseAPIKeyInput
		a.validErr = msg.err.Error()
		a.apiKeyInput.Focus()
		return a, textinput.Blink

	case deviceFlowCodeMsg:
		if msg.err != nil {
			a.resultMsg = fmt.Sprintf("Device flow error: %v", msg.err)
			a.resultErr = true
			a.phase = authPhaseDone
			return a, nil
		}
		a.deviceCode = msg.code
		util.OpenURL(msg.code.VerificationURI)
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(msg.code.ExpiresIn)*time.Second)
		a.pollCancel = cancel
		cfg := a.selectedMethod().config
		deviceCode := msg.code.DeviceCode
		interval := msg.code.Interval
		return a, func() tea.Msg {
			token, err := auth.PollForToken(ctx, cfg, deviceCode, interval)
			if err != nil {
				return deviceFlowTokenMsg{err: err}
			}
			return deviceFlowTokenMsg{token: token.AccessToken}
		}

	case deviceFlowTokenMsg:
		if a.pollCancel != nil {
			a.pollCancel()
		}
		if msg.err != nil {
			a.resultMsg = fmt.Sprintf("Device flow error: %v", msg.err)
			a.resultErr = true
			a.phase = authPhaseDone
			return a, nil
		}
		p := a.selectedProvider()
		a.phase = authPhaseDone
		a.resultMsg = fmt.Sprintf("Connected to %s", p.Name)
		return a, util.CmdHandler(AuthCompleteMsg{
			Provider: string(p.ProviderKey),
			Method:   AuthMethodDeviceFlow,
			Token:    msg.token,
		})

	case bridgeValidateMsg:
		if msg.valid {
			p := a.selectedProvider()
			a.phase = authPhaseDone
			a.resultMsg = fmt.Sprintf("Connected to %s [UNSUPPORTED]", p.Name)
			return a, util.CmdHandler(AuthCompleteMsg{
				Provider: string(p.ProviderKey),
				Method:   AuthMethodBridge,
				Token:    a.bridgeResult.Token,
			})
		}
		// Validation failed — token is rejected by the provider
		errMsg := "Token rejected by provider"
		if msg.err != nil {
			errMsg = msg.err.Error()
		}
		m := a.selectedMethod()
		note := ""
		if m.config.Bridge != nil && m.config.Bridge.SupportLevel == "unsupported" {
			note = "\n\nThis is expected — unsupported bridge tokens may not\nwork with third-party tools. Use API key instead."
		}
		a.resultMsg = fmt.Sprintf("Token validation failed: %s%s", errMsg, note)
		a.resultErr = true
		a.phase = authPhaseDone
		return a, nil

	case bridgeDetectMsg:
		a.bridgeInfo = msg.info
		m := a.selectedMethod()
		bridgeCfg := m.config.Bridge

		if msg.info.CacheFound || len(bridgeCfg.TokenArgs) > 0 {
			// Token might be available — try importing
			return a, func() tea.Msg {
				result, err := auth.ImportBridgeToken(context.Background(), bridgeCfg)
				return bridgeImportMsg{result: result, err: err}
			}
		}

		if msg.info.CLIFound {
			// CLI found but no cache — offer login
			a.phase = authPhaseBridgeLogin
			return a, nil
		}

		// Nothing found
		a.resultMsg = fmt.Sprintf("Install %s CLI to use this method.\nOr use API Key instead.", bridgeCfg.CLIName)
		a.resultErr = true
		a.phase = authPhaseDone
		return a, nil

	case bridgeImportMsg:
		if msg.err != nil {
			// Import failed — check if CLI is available for login
			if a.bridgeInfo != nil && a.bridgeInfo.CLIFound {
				a.phase = authPhaseBridgeLogin
				return a, nil
			}
			m := a.selectedMethod()
			cliName := "CLI"
			if m.config.Bridge != nil {
				cliName = m.config.Bridge.CLIName
			}
			a.resultMsg = fmt.Sprintf("Could not import credentials: %v\nInstall %s or use API Key.", msg.err, cliName)
			a.resultErr = true
			a.phase = authPhaseDone
			return a, nil
		}
		a.bridgeResult = msg.result
		a.phase = authPhaseBridgeImport
		return a, nil

	case bridgeLoginDoneMsg:
		if msg.err != nil {
			a.resultMsg = fmt.Sprintf("CLI login failed: %v\nTry API Key instead.", msg.err)
			a.resultErr = true
			a.phase = authPhaseDone
			return a, nil
		}
		// Login succeeded — try importing the token
		m := a.selectedMethod()
		bridgeCfg := m.config.Bridge
		a.phase = authPhaseBridgeDetect // show detecting while we import
		return a, func() tea.Msg {
			result, err := auth.ImportBridgeToken(context.Background(), bridgeCfg)
			return bridgeImportMsg{result: result, err: err}
		}

	case AuthCompleteMsg:
		a.phase = authPhaseDone
		if msg.Error != nil {
			a.resultMsg = fmt.Sprintf("Error: %v", msg.Error)
			a.resultErr = true
		} else {
			provider := msg.Provider
			if cap := auth.FindCapability(models.ModelProvider(provider)); cap != nil {
				provider = cap.Name
			}
			a.resultMsg = fmt.Sprintf("Connected to %s", provider)
		}
		return a, nil

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
	}

	return a, nil
}

func (a *authDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	maxWidth := 58

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(maxWidth).
		Padding(0, 1).
		Render("Connect Provider")

	var body string
	switch a.phase {
	case authPhaseProvider:
		items := make([]string, len(a.providers))
		for i, p := range a.providers {
			itemStyle := baseStyle.Width(maxWidth).Padding(0, 1)
			if i == a.providerIdx {
				itemStyle = itemStyle.Background(t.Primary()).Foreground(t.Background()).Bold(true)
			}
			items[i] = itemStyle.Render(p.Name)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, items...)

	case authPhaseMethod:
		p := a.selectedProvider()
		var lines []string

		header := baseStyle.Foreground(t.Primary()).Bold(true).Padding(0, 1).Render(
			fmt.Sprintf("Connect to %s — choose method:", p.Name))
		lines = append(lines, header)
		lines = append(lines, "")

		for i, m := range a.methods {
			label := auth.MethodDisplayName(m.method)
			// Use bridge label if available
			if m.method == auth.MethodBridgeImport && m.config.Bridge != nil && m.config.Bridge.Label != "" {
				label = m.config.Bridge.Label
			}

			itemStyle := baseStyle.Width(maxWidth).Padding(0, 1)

			if !m.config.Enabled {
				// Disabled: grayed out with reason
				itemStyle = itemStyle.Foreground(t.TextMuted())
				reason := m.config.DisabledReason
				if m.config.Bridge != nil && m.config.Bridge.SupportLevel == "blocked" {
					reason = m.config.Bridge.SupportNote
				}
				label = fmt.Sprintf("%s  (%s)", label, reason)
			} else {
				// Show support level badge for bridges
				if m.config.Bridge != nil {
					badge := supportBadge(t, baseStyle, m.config.Bridge.SupportLevel)
					if badge != "" {
						label = fmt.Sprintf("%s  %s", label, badge)
					}
				}
				if i == a.methodIdx {
					itemStyle = itemStyle.Background(t.Primary()).Foreground(t.Background()).Bold(true)
				}
			}
			lines = append(lines, itemStyle.Render(label))
		}
		body = lipgloss.JoinVertical(lipgloss.Left, lines...)

	case authPhaseAPIKeyInput:
		m := a.selectedMethod()
		p := a.selectedProvider()
		var lines []string

		header := baseStyle.Foreground(t.Primary()).Bold(true).Padding(0, 1).Render(
			fmt.Sprintf("Connect to %s", p.Name))
		lines = append(lines, header)
		lines = append(lines, "")

		if m.config.KeyPageURL != "" {
			urlLine := baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
				fmt.Sprintf("Get your key: %s", m.config.KeyPageURL))
			lines = append(lines, urlLine)

			if a.urlOpened {
				lines = append(lines, baseStyle.Foreground(t.Success()).Padding(0, 1).Render(
					"  Key page opened in browser"))
			} else {
				lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
					"  Press Tab to open in browser"))
			}
			lines = append(lines, "")
		}

		lines = append(lines, baseStyle.Foreground(t.Text()).Padding(0, 1).Render(
			"Paste API key:"))
		lines = append(lines, baseStyle.Padding(0, 1).Render(a.apiKeyInput.View()))

		if a.validErr != "" {
			lines = append(lines, "")
			lines = append(lines, baseStyle.Foreground(t.Error()).Padding(0, 1).Render(
				fmt.Sprintf("Error: %s", a.validErr)))
		}

		lines = append(lines, "")
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
			"Enter = validate & save  |  Esc = back  |  Tab = open key page"))

		body = lipgloss.JoinVertical(lipgloss.Left, lines...)

	case authPhaseAPIKeyValidating:
		var lines []string
		lines = append(lines, baseStyle.Foreground(t.Primary()).Bold(true).Padding(0, 1).Render(
			"Validating API key..."))
		lines = append(lines, "")
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
			"Press Esc to cancel"))
		body = lipgloss.JoinVertical(lipgloss.Left, lines...)

	case authPhaseDeviceFlow:
		var lines []string
		p := a.selectedProvider()

		lines = append(lines, baseStyle.Foreground(t.Primary()).Bold(true).Padding(0, 1).Render(
			fmt.Sprintf("Connect to %s — Device Flow", p.Name)))
		lines = append(lines, "")

		if a.deviceCode != nil {
			lines = append(lines, baseStyle.Foreground(t.Text()).Padding(0, 1).Render(
				"Open this URL in your browser:"))
			lines = append(lines, baseStyle.Foreground(t.Primary()).Bold(true).Padding(0, 1).Render(
				fmt.Sprintf("  %s", a.deviceCode.VerificationURI)))
			lines = append(lines, "")
			lines = append(lines, baseStyle.Foreground(t.Text()).Padding(0, 1).Render(
				"Enter this code:"))
			lines = append(lines, baseStyle.Foreground(t.Success()).Bold(true).Padding(0, 1).Render(
				fmt.Sprintf("  %s", a.deviceCode.UserCode)))
			lines = append(lines, "")
			lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
				"Waiting for authorization..."))
		} else {
			lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
				"Requesting device code..."))
		}
		lines = append(lines, "")
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
			"Esc = cancel"))
		body = lipgloss.JoinVertical(lipgloss.Left, lines...)

	case authPhaseBridgeWarn:
		var lines []string
		p := a.selectedProvider()
		m := a.selectedMethod()

		lines = append(lines, baseStyle.Foreground(lipgloss.Color("#FF6600")).Bold(true).Padding(0, 1).Render(
			"WARNING: UNSUPPORTED AUTH METHOD"))
		lines = append(lines, "")

		cliName := "CLI"
		if m.config.Bridge != nil {
			cliName = m.config.Bridge.CLIName
		}

		lines = append(lines, baseStyle.Foreground(t.Text()).Padding(0, 1).Width(maxWidth-2).Render(
			fmt.Sprintf("You are about to import credentials from the %s CLI for %s.", cliName, p.Name)))
		lines = append(lines, "")

		lines = append(lines, baseStyle.Foreground(lipgloss.Color("#FF6600")).Padding(0, 1).Width(maxWidth-2).Render(
			"This method is UNSUPPORTED and BEST-EFFORT:"))
		lines = append(lines, baseStyle.Foreground(t.Text()).Padding(0, 1).Width(maxWidth-2).Render(
			" - May break at any time without notice"))
		lines = append(lines, baseStyle.Foreground(t.Text()).Padding(0, 1).Width(maxWidth-2).Render(
			" - Not endorsed by the provider"))
		lines = append(lines, baseStyle.Foreground(t.Text()).Padding(0, 1).Width(maxWidth-2).Render(
			" - Token may be rejected by the API"))
		lines = append(lines, baseStyle.Foreground(t.Text()).Padding(0, 1).Width(maxWidth-2).Render(
			" - Use API key for reliable access"))
		lines = append(lines, "")
		lines = append(lines, baseStyle.Foreground(t.Text()).Bold(true).Padding(0, 1).Render(
			"Enter = I understand, proceed  |  Esc = go back"))
		body = lipgloss.JoinVertical(lipgloss.Left, lines...)

	case authPhaseBridgeDetect:
		var lines []string
		p := a.selectedProvider()
		m := a.selectedMethod()
		label := auth.BridgeDisplayName(m.config)

		lines = append(lines, baseStyle.Foreground(t.Primary()).Bold(true).Padding(0, 1).Render(
			fmt.Sprintf("Connect to %s — %s", p.Name, label)))
		if m.config.Bridge != nil {
			lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
				supportBadge(t, baseStyle, m.config.Bridge.SupportLevel)))
		}
		lines = append(lines, "")
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
			fmt.Sprintf("Detecting %s CLI...", m.config.Bridge.CLIName)))
		lines = append(lines, "")
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
			"Esc = cancel"))
		body = lipgloss.JoinVertical(lipgloss.Left, lines...)

	case authPhaseBridgeImport:
		var lines []string
		p := a.selectedProvider()
		m := a.selectedMethod()
		label := auth.BridgeDisplayName(m.config)

		lines = append(lines, baseStyle.Foreground(t.Primary()).Bold(true).Padding(0, 1).Render(
			fmt.Sprintf("Connect to %s — %s", p.Name, label)))
		if m.config.Bridge != nil {
			lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
				supportBadge(t, baseStyle, m.config.Bridge.SupportLevel)))
		}
		lines = append(lines, "")

		if a.bridgeResult != nil {
			lines = append(lines, baseStyle.Foreground(t.Success()).Padding(0, 1).Render(
				"Found existing session:"))
			lines = append(lines, baseStyle.Foreground(t.Text()).Padding(0, 1).Render(
				fmt.Sprintf("  Source: %s", a.bridgeResult.SourcePath)))
			lines = append(lines, baseStyle.Foreground(t.Text()).Padding(0, 1).Render(
				fmt.Sprintf("  Key:    %s", auth.RedactKey(a.bridgeResult.Token))))
			if m.config.Bridge != nil && m.config.Bridge.SupportNote != "" {
				lines = append(lines, "")
				lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
					m.config.Bridge.SupportNote))
			}
			lines = append(lines, "")
			lines = append(lines, baseStyle.Foreground(t.Text()).Bold(true).Padding(0, 1).Render(
				"Enter = validate & import  |  Esc = back"))
		}
		body = lipgloss.JoinVertical(lipgloss.Left, lines...)

	case authPhaseBridgeValidating:
		var lines []string
		lines = append(lines, baseStyle.Foreground(t.Primary()).Bold(true).Padding(0, 1).Render(
			"Validating imported token..."))
		lines = append(lines, "")
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
			"Checking if the token is accepted by the provider API."))
		lines = append(lines, "")
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
			"Esc = cancel"))
		body = lipgloss.JoinVertical(lipgloss.Left, lines...)

	case authPhaseBridgeLogin:
		var lines []string
		p := a.selectedProvider()
		m := a.selectedMethod()
		label := auth.BridgeDisplayName(m.config)

		lines = append(lines, baseStyle.Foreground(t.Primary()).Bold(true).Padding(0, 1).Render(
			fmt.Sprintf("Connect to %s — %s", p.Name, label)))
		if m.config.Bridge != nil {
			lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
				supportBadge(t, baseStyle, m.config.Bridge.SupportLevel)))
		}
		lines = append(lines, "")

		lines = append(lines, baseStyle.Foreground(t.Text()).Padding(0, 1).Render(
			"No existing session found."))

		if a.bridgeInfo != nil && a.bridgeInfo.CLIPath != "" {
			lines = append(lines, baseStyle.Foreground(t.Text()).Padding(0, 1).Render(
				fmt.Sprintf("%s CLI detected at %s", m.config.Bridge.CLIName, a.bridgeInfo.CLIPath)))
			lines = append(lines, "")
			lines = append(lines, baseStyle.Foreground(t.Text()).Bold(true).Padding(0, 1).Render(
				fmt.Sprintf("Press Enter to login via %s", m.config.Bridge.CLIName)))
			lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
				"(Browser will open for authentication)"))
		}
		lines = append(lines, "")
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Padding(0, 1).Render(
			"Esc = back"))
		body = lipgloss.JoinVertical(lipgloss.Left, lines...)

	case authPhaseDone:
		fg := t.Success()
		if a.resultErr {
			fg = t.Error()
		}
		body = baseStyle.Foreground(fg).Padding(0, 1).Width(maxWidth).Render(a.resultMsg)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(maxWidth).Render(""),
		body,
		baseStyle.Width(maxWidth).Render(""),
	)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

func supportBadge(t theme.Theme, baseStyle lipgloss.Style, level string) string {
	switch level {
	case "supported":
		return baseStyle.Foreground(t.Success()).Render("[Supported]")
	case "unsupported":
		return baseStyle.Foreground(lipgloss.Color("#FF6600")).Render("[UNSUPPORTED]")
	case "experimental":
		return baseStyle.Foreground(lipgloss.Color("#FFAA00")).Render("[Experimental]")
	case "blocked":
		return baseStyle.Foreground(t.Error()).Render("[Blocked]")
	default:
		return ""
	}
}

func (a *authDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(authKeys)
}

// NewAuthDialogCmp creates a new provider authentication dialog.
func NewAuthDialogCmp() AuthDialog {
	ti := textinput.New()
	ti.Placeholder = "sk-..."
	ti.EchoMode = textinput.EchoPassword
	ti.Width = 48

	return &authDialogCmp{
		phase:       authPhaseProvider,
		providers:   auth.InteractiveProviders(),
		apiKeyInput: ti,
	}
}
