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

// codexCodeClient proxies LLM calls through the Codex CLI,
// enabling ChatGPT Plus/Pro subscribers to use Genesis with OpenAI models
// without a separate API key. Similar pattern to claudeCodeClient.
type codexCodeClient struct {
	providerOptions providerClientOptions
	cliPath         string
}

func newCodexCodeClient(opts providerClientOptions) ProviderClient {
	cliPath, _ := exec.LookPath("codex")
	if cliPath == "" {
		for _, p := range []string{"/usr/local/bin/codex", "/usr/bin/codex"} {
			if _, err := exec.LookPath(p); err == nil {
				cliPath = p
				break
			}
		}
	}
	return &codexCodeClient{
		providerOptions: opts,
		cliPath:         cliPath,
	}
}

// Codex CLI JSONL event types
type codexEvent struct {
	Type     string     `json:"type"`
	ThreadID string     `json:"thread_id,omitempty"`
	Item     *codexItem `json:"item,omitempty"`
	Usage    *codexUsage `json:"usage,omitempty"`
	Message  string     `json:"message,omitempty"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type codexItem struct {
	ID   string `json:"id"`
	Type string `json:"type"` // "reasoning", "agent_message", "tool_call", "tool_output"
	Text string `json:"text,omitempty"`
}

type codexUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
}

func (c *codexCodeClient) send(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) (*ProviderResponse, error) {
	if c.cliPath == "" {
		return nil, fmt.Errorf("codex CLI not found in PATH — install: npm install -g @openai/codex")
	}

	prompt := c.extractPrompt(messages)
	if prompt == "" {
		return nil, fmt.Errorf("no user message found")
	}

	model := c.providerOptions.model.APIModel
	args := []string{
		"exec",
		"--full-auto",
		"--json",
		"-m", model,
		"-",
	}

	logging.Info("codex-code proxy: send",
		"model", model,
		"prompt_len", len(prompt),
	)

	cmd := exec.CommandContext(ctx, c.cliPath, args...)
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("codex CLI failed (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("codex CLI: %w", err)
	}

	// Parse JSONL output — collect agent_message text and usage
	var content strings.Builder
	var usage TokenUsage
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt codexEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		if evt.Item != nil && evt.Item.Type == "agent_message" {
			content.WriteString(evt.Item.Text)
		}
		if evt.Usage != nil {
			usage = TokenUsage{
				InputTokens:    evt.Usage.InputTokens,
				OutputTokens:   evt.Usage.OutputTokens,
				CacheReadTokens: evt.Usage.CachedInputTokens,
			}
		}
		if evt.Type == "error" || evt.Type == "turn.failed" {
			errMsg := evt.Message
			if evt.Error != nil {
				errMsg = evt.Error.Message
			}
			return nil, fmt.Errorf("codex: %s", errMsg)
		}
	}

	return &ProviderResponse{
		Content:      content.String(),
		Usage:        usage,
		FinishReason: message.FinishReasonEndTurn,
	}, nil
}

func (c *codexCodeClient) stream(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) <-chan ProviderEvent {
	eventChan := make(chan ProviderEvent)

	go func() {
		defer close(eventChan)

		if c.cliPath == "" {
			eventChan <- ProviderEvent{
				Type:  EventError,
				Error: fmt.Errorf("codex CLI not found in PATH"),
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

		model := c.providerOptions.model.APIModel
		args := []string{
			"exec",
			"--full-auto",
			"--json",
			"-m", model,
			"-",
		}

		logging.Info("codex-code proxy: stream",
			"model", model,
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
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		contentStarted := false
		var fullContent strings.Builder
		var totalUsage TokenUsage

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var evt codexEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "item.completed":
				if evt.Item == nil {
					continue
				}
				switch evt.Item.Type {
				case "agent_message":
					if !contentStarted {
						eventChan <- ProviderEvent{Type: EventContentStart}
						contentStarted = true
					}
					if evt.Item.Text != "" {
						fullContent.WriteString(evt.Item.Text)
						eventChan <- ProviderEvent{
							Type:    EventContentDelta,
							Content: evt.Item.Text,
						}
					}
				case "reasoning":
					// Emit reasoning as thinking content
					if evt.Item.Text != "" {
						eventChan <- ProviderEvent{
							Type:     EventContentDelta,
							Thinking: evt.Item.Text,
						}
					}
				}

			case "turn.completed":
				if evt.Usage != nil {
					totalUsage = TokenUsage{
						InputTokens:    evt.Usage.InputTokens,
						OutputTokens:   evt.Usage.OutputTokens,
						CacheReadTokens: evt.Usage.CachedInputTokens,
					}
				}
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

			case "turn.failed", "error":
				errMsg := evt.Message
				if evt.Error != nil {
					errMsg = evt.Error.Message
				}
				eventChan <- ProviderEvent{
					Type:  EventError,
					Error: fmt.Errorf("codex: %s", errMsg),
				}
			}
		}

		_ = cmd.Wait()
	}()

	return eventChan
}

func (c *codexCodeClient) extractPrompt(messages []message.Message) string {
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

// IsCodexCLIToken returns true if the key looks like a Codex CLI OAuth JWT.
func IsCodexCLIToken(apiKey string) bool {
	return strings.HasPrefix(apiKey, "eyJ")
}

// HasCodexCLI checks if the Codex CLI is available.
func HasCodexCLI() bool {
	_, err := exec.LookPath("codex")
	return err == nil
}
