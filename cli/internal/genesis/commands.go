package genesis

import (
	"encoding/json"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/components/chat"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/components/dialog"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/util"
)

// OpenSwarmAgentDialogMsg signals the TUI to show the swarm agent list dialog.
type OpenSwarmAgentDialogMsg struct{}

// OpenRoleModelDialogMsg signals the TUI to show the per-role model picker.
type OpenRoleModelDialogMsg struct{}

// OpenAuthDialogMsg signals the TUI to show the provider auth dialog.
type OpenAuthDialogMsg struct{}

// GenesisCommands returns all Genesis-specific slash commands for the command palette.
func GenesisCommands(client *Client) []dialog.Command {
	return []dialog.Command{
		{
			ID:          "heaven-status",
			Title:       "HeavenStatus",
			Description: "Show Heaven state revision, leases, and clocks",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return func() tea.Msg {
					status, err := client.Status()
					if err != nil {
						return util.InfoMsg{Type: util.InfoTypeError, Msg: fmt.Sprintf("HeavenStatus: %v", err)}
					}
					summary := fmt.Sprintf("state_rev=%d  leases=%d  clocks=%d",
						status.StateRev, len(status.Leases), len(status.Clocks))
					return util.InfoMsg{Type: util.InfoTypeInfo, Msg: summary}
				}
			},
		},
		{
			ID:          "heaven-logs",
			Title:       "HeavenLogs",
			Description: "Show last 20 Heaven events",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return func() tea.Msg {
					events, err := client.TailEvents(20)
					if err != nil {
						return util.InfoMsg{Type: util.InfoTypeError, Msg: fmt.Sprintf("HeavenLogs: %v", err)}
					}
					summary := fmt.Sprintf("Fetched %d events", len(events))
					if len(events) > 0 {
						last, _ := json.MarshalIndent(events[len(events)-1], "", "  ")
						summary += "\nLatest: " + string(last)
					}
					return util.InfoMsg{Type: util.InfoTypeInfo, Msg: summary}
				}
			},
		},
		{
			ID:          "index-repo",
			Title:       "IndexRepo",
			Description: "Build IR index for the current working directory",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return func() tea.Msg {
					cwd, _ := os.Getwd()
					symbols, err := client.IRBuild(cwd)
					if err != nil {
						return util.InfoMsg{Type: util.InfoTypeError, Msg: fmt.Sprintf("IndexRepo: %v", err)}
					}
					return util.InfoMsg{Type: util.InfoTypeInfo, Msg: fmt.Sprintf("Indexed %d symbols", symbols)}
				}
			},
		},
		{
			ID:          "sym-search",
			Title:       "SymSearch",
			Description: "Search for symbols by query",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return util.CmdHandler(chat.SendMsg{
					Text: "/sym-search: Please provide a search query and I will search the IR index for matching symbols.",
				})
			},
		},
		{
			ID:          "sym-def",
			Title:       "SymDef",
			Description: "Look up symbol definitions by name",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return util.CmdHandler(chat.SendMsg{
					Text: "/sym-def: Please provide a symbol name and I will look up its definition in the IR index.",
				})
			},
		},
		{
			ID:          "callers",
			Title:       "Callers",
			Description: "Find callers of a symbol",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return util.CmdHandler(chat.SendMsg{
					Text: "/callers: Please provide a symbol name and I will find its callers in the IR index.",
				})
			},
		},
		{
			ID:          "lease-acquire",
			Title:       "LeaseAcquire",
			Description: "Acquire a file lease on a scope",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return util.CmdHandler(chat.SendMsg{
					Text: "/lease-acquire: Please specify the owner, mission, and file scopes for the lease.",
				})
			},
		},
		{
			ID:          "validate-manifest",
			Title:       "ValidateManifest",
			Description: "Validate the current manifest",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return util.CmdHandler(chat.SendMsg{
					Text: "/validate-manifest: I will validate the current manifest against the Heaven schema.",
				})
			},
		},
		{
			ID:          "plan-mission",
			Title:       "PlanMission",
			Description: "Plan a mission: build a task DAG via the planner",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return util.CmdHandler(chat.SendMsg{
					Text: "/plan-mission: Please describe the task you want to plan. I will build a mission DAG using the Genesis planner.",
				})
			},
		},
		{
			ID:          "run-mission",
			Title:       "RunMission",
			Description: "Full plan+execute+verify cycle for a mission",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return func() tea.Msg {
					cwd, _ := os.Getwd()
					taskDesc, err := ReadMissionFile(cwd)
					if err != nil {
						return util.InfoMsg{Type: util.InfoTypeError,
							Msg: "Create MISSION.md in CWD with your task description, then run /run-mission again."}
					}

					runner := NewMissionRunner(cwd)
					result, err := runner.Run(taskDesc)
					if err != nil {
						os.WriteFile("/tmp/genesis-mission-error.log", []byte("ERR: "+err.Error()), 0644)
						return util.InfoMsg{Type: util.InfoTypeError,
							Msg: fmt.Sprintf("Mission failed: %v", err)}
					}
					// Also log result errors
					if result != nil && !result.Success {
						os.WriteFile("/tmp/genesis-mission-error.log", []byte("RESULT: "+result.Error), 0644)
					}
					return util.InfoMsg{Type: util.InfoTypeInfo,
						Msg: FormatMissionResult(result)}
				}
			},
		},
		{
			ID:          "swarm-agent-list",
			Title:       "SwarmAgentList",
			Description: "View active swarm angel workers",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return util.CmdHandler(OpenSwarmAgentDialogMsg{})
			},
		},
		{
			ID:          "connect-provider",
			Title:       "ConnectProvider",
			Description: "Connect an LLM provider",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return util.CmdHandler(OpenAuthDialogMsg{})
			},
		},
		{
			ID:          "select-model",
			Title:       "SelectModel",
			Description: "Assign models per Genesis role (God/Angel/Oracle)",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return util.CmdHandler(OpenRoleModelDialogMsg{})
			},
		},
		{
			ID:          "blob-store",
			Title:       "BlobStore",
			Description: "Upload content to the Heaven blob store",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return util.CmdHandler(chat.SendMsg{
					Text: "/blob-store: Please provide the content you want to store in the blob store.",
				})
			},
		},
	}
}
