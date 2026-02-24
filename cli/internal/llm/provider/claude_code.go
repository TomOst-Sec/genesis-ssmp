package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	toolsPkg "github.com/genesis-ssmp/genesis/cli/internal/llm/tools"
	"github.com/genesis-ssmp/genesis/cli/internal/logging"
	"github.com/genesis-ssmp/genesis/cli/internal/message"
)

// claudeCodeClient proxies LLM calls through the Claude Code CLI,
// enabling Claude Pro/Max subscribers to use Genesis without separate API credits.
// Anthropic blocks third-party tools from using OAuth tokens directly against
// api.anthropic.com, but routing through the official Claude Code CLI is legitimate.
type claudeCodeClient struct {
	providerOptions providerClientOptions
	cliPath         string
}

type ClaudeCodeClient = ProviderClient

func newClaudeCodeClient(opts providerClientOptions) ClaudeCodeClient {
	cliPath, _ := exec.LookPath("claude")
	if cliPath == "" {
		// Also check common install locations
		for _, p := range []string{"/usr/local/bin/claude", "/usr/bin/claude"} {
			if _, err := exec.LookPath(p); err == nil {
				cliPath = p
				break
			}
		}
	}
	return &claudeCodeClient{
		providerOptions: opts,
		cliPath:         cliPath,
	}
}

// claudeCodeJSONResult matches Claude Code's --output-format json structure.
type claudeCodeJSONResult struct {
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	Result     string `json:"result"`
	IsError    bool   `json:"is_error"`
	DurationMS int    `json:"duration_ms"`
	Usage      struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

// claudeCodeStreamEvent matches one line of Claude Code's --output-format stream-json.
type claudeCodeStreamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// For type="assistant"
	Message *struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
			ID   string `json:"id,omitempty"`
			Name string `json:"name,omitempty"`
			// tool_use input comes as raw JSON
			Input json.RawMessage `json:"input,omitempty"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message,omitempty"`

	// For type="result"
	Result  string `json:"result,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

func (c *claudeCodeClient) send(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) (*ProviderResponse, error) {
	if c.cliPath == "" {
		return nil, fmt.Errorf("claude CLI not found in PATH — install Claude Code: npm install -g @anthropic-ai/claude-code")
	}

	prompt := c.extractPrompt(messages)
	if prompt == "" {
		return nil, fmt.Errorf("no user message found")
	}

	args := []string{
		"-p",
		"--output-format", "json",
		"--model", c.modelAlias(),
		"--no-session-persistence",
	}
	if c.providerOptions.systemMessage != "" {
		args = append(args, "--system-prompt", c.providerOptions.systemMessage)
	}

	logging.Info("claude-code proxy: send",
		"model", c.modelAlias(),
		"prompt_len", len(prompt),
	)

	cmd := exec.CommandContext(ctx, c.cliPath, args...)
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("claude CLI failed (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("claude CLI: %w", err)
	}

	var result claudeCodeJSONResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse claude output: %w (raw: %s)", err, string(out[:min(len(out), 200)]))
	}

	if result.IsError {
		return nil, fmt.Errorf("claude CLI error: %s", result.Result)
	}

	return &ProviderResponse{
		Content: result.Result,
		Usage: TokenUsage{
			InputTokens:         result.Usage.InputTokens,
			OutputTokens:        result.Usage.OutputTokens,
			CacheCreationTokens: result.Usage.CacheCreationInputTokens,
			CacheReadTokens:     result.Usage.CacheReadInputTokens,
		},
		FinishReason: message.FinishReasonEndTurn,
	}, nil
}

func (c *claudeCodeClient) stream(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) <-chan ProviderEvent {
	eventChan := make(chan ProviderEvent)

	go func() {
		defer close(eventChan)

		if c.cliPath == "" {
			eventChan <- ProviderEvent{
				Type:  EventError,
				Error: fmt.Errorf("claude CLI not found in PATH"),
			}
			return
		}

		prompt := c.extractPrompt(messages)
		if prompt == "" {
			eventChan <- ProviderEvent{
				Type:  EventError,
				Error: fmt.Errorf("no user message found"),
			}
			return
		}

		args := []string{
			"-p",
			"--output-format", "stream-json",
			"--verbose",
			"--model", c.modelAlias(),
			"--no-session-persistence",
		}
		if c.providerOptions.systemMessage != "" {
			args = append(args, "--system-prompt", c.providerOptions.systemMessage)
		}

		logging.Info("claude-code proxy: stream",
			"model", c.modelAlias(),
			"prompt_len", len(prompt),
		)

		cmd := exec.CommandContext(ctx, c.cliPath, args...)
		cmd.Stdin = strings.NewReader(prompt)

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			eventChan <- ProviderEvent{Type: EventError, Error: fmt.Errorf("pipe: %w", err)}
			return
		}

		if err := cmd.Start(); err != nil {
			eventChan <- ProviderEvent{Type: EventError, Error: fmt.Errorf("start: %w", err)}
			return
		}

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB line buffer
		contentStarted := false
		var fullContent strings.Builder
		var totalUsage TokenUsage

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var evt claudeCodeStreamEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "assistant":
				if evt.Message == nil {
					continue
				}
				for _, block := range evt.Message.Content {
					switch block.Type {
					case "text":
						if !contentStarted {
							eventChan <- ProviderEvent{Type: EventContentStart}
							contentStarted = true
						}
						if block.Text != "" {
							fullContent.WriteString(block.Text)
							eventChan <- ProviderEvent{
								Type:    EventContentDelta,
								Content: block.Text,
							}
						}
					case "tool_use":
						// Emit tool use events — Claude Code may call its own tools
						inputStr := string(block.Input)
						eventChan <- ProviderEvent{
							Type: EventToolUseStart,
							ToolCall: &message.ToolCall{
								ID:   block.ID,
								Name: block.Name,
							},
						}
						eventChan <- ProviderEvent{
							Type: EventToolUseDelta,
							ToolCall: &message.ToolCall{
								ID:    block.ID,
								Name:  block.Name,
								Input: inputStr,
							},
						}
						eventChan <- ProviderEvent{
							Type: EventToolUseStop,
							ToolCall: &message.ToolCall{
								ID: block.ID,
							},
						}
					}
				}
				// Capture usage from assistant message
				totalUsage = TokenUsage{
					InputTokens:         evt.Message.Usage.InputTokens,
					OutputTokens:        evt.Message.Usage.OutputTokens,
					CacheCreationTokens: evt.Message.Usage.CacheCreationInputTokens,
					CacheReadTokens:     evt.Message.Usage.CacheReadInputTokens,
				}

			case "result":
				if contentStarted {
					eventChan <- ProviderEvent{Type: EventContentStop}
				}
				eventChan <- ProviderEvent{
					Type: EventComplete,
					Response: &ProviderResponse{
						Content:      fullContent.String(),
						Usage:        totalUsage,
						FinishReason: message.FinishReasonEndTurn,
					},
				}
			}
		}

		_ = cmd.Wait()
	}()

	return eventChan
}

// extractPrompt builds a text prompt from the message history.
// For the proxy, we concatenate the conversation — Claude Code doesn't accept
// the structured Anthropic messages array directly.
func (c *claudeCodeClient) extractPrompt(messages []message.Message) string {
	var parts []string
	for _, msg := range messages {
		content := msg.Content().String()
		if content == "" {
			continue
		}
		switch msg.Role {
		case message.User:
			parts = append(parts, content)
		case message.Assistant:
			parts = append(parts, "[Previous response]: "+content)
		case message.Tool:
			for _, r := range msg.ToolResults() {
				if r.Content != "" {
					parts = append(parts, "[Tool result]: "+r.Content)
				}
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// modelAlias maps Genesis model IDs to Claude Code model aliases.
// Claude Code accepts aliases like "opus", "sonnet", "haiku" or full model IDs.
func (c *claudeCodeClient) modelAlias() string {
	apiModel := c.providerOptions.model.APIModel
	// If it's already a claude-* model ID, pass it through directly
	// so Claude Code picks the exact version (e.g. "claude-opus-4-6")
	if strings.HasPrefix(apiModel, "claude-") {
		return apiModel
	}
	switch {
	case strings.Contains(apiModel, "opus"):
		return "opus"
	case strings.Contains(apiModel, "haiku"):
		return "haiku"
	default:
		return "sonnet"
	}
}

// IsOAuthToken returns true if the key is a Claude Code OAuth token.
func IsOAuthToken(apiKey string) bool {
	return strings.HasPrefix(apiKey, "sk-ant-o")
}

// HasClaudeCodeCLI checks if the Claude Code CLI is available.
func HasClaudeCodeCLI() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

// ClaudeCodeOption is a placeholder for future options.
type ClaudeCodeOption func(*struct{})
