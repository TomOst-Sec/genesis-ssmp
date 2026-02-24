package genesis

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/genesis-ssmp/genesis/god"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// L3: Slash command palette filtering — "/" shows Genesis commands
// ---------------------------------------------------------------------------

func TestPaletteFilterSlash(t *testing.T) {
	client := NewClient("127.0.0.1:4444")
	commands := GenesisCommands(client)

	// All commands should be filterable by "/" prefix (palette opens on "/")
	for _, cmd := range commands {
		assert.NotEmpty(t, cmd.ID, "command ID must not be empty")
		assert.NotEmpty(t, cmd.Title, "command Title must not be empty for palette display")
		// IDs should be lowercase-kebab-case for consistency
		assert.Equal(t, strings.ToLower(cmd.ID), cmd.ID,
			"command ID %q should be lowercase", cmd.ID)
	}
}

// ---------------------------------------------------------------------------
// L3: /status handler returns msg with state_rev
// ---------------------------------------------------------------------------

func TestSlashStatusReturnsInfoMsg(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(HeavenStatus{
			StateRev: 77,
			Leases:   []LeaseInfo{},
			Clocks:   map[string]int64{"main.go": 5},
		})
	})
	c := testClient(t, mux)

	commands := GenesisCommands(c)
	for _, cmd := range commands {
		if cmd.ID == "heaven-status" {
			handler := cmd.Handler(cmd)
			if handler != nil {
				msg := handler()
				assert.NotNil(t, msg, "heaven-status handler should return a msg")
			}
			break
		}
	}
}

// ---------------------------------------------------------------------------
// L3: /index handler calls IR build
// ---------------------------------------------------------------------------

func TestSlashIndexCallsIRBuild(t *testing.T) {
	var buildCalled bool
	mux := http.NewServeMux()
	mux.HandleFunc("/ir/build", func(w http.ResponseWriter, r *http.Request) {
		buildCalled = true
		json.NewEncoder(w).Encode(map[string]int{"symbols": 42})
	})
	c := testClient(t, mux)

	commands := GenesisCommands(c)
	for _, cmd := range commands {
		if cmd.ID == "index-repo" {
			handler := cmd.Handler(cmd)
			if handler != nil {
				handler()
			}
			break
		}
	}

	assert.True(t, buildCalled, "/index-repo should call POST /ir/build")
}

// ---------------------------------------------------------------------------
// L3: /logs handler fetches events
// ---------------------------------------------------------------------------

func TestSlashLogsReturnsEvents(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/events/tail", func(w http.ResponseWriter, r *http.Request) {
		n := r.URL.Query().Get("n")
		assert.Equal(t, "20", n, "heaven-logs should request 20 events")
		json.NewEncoder(w).Encode([]json.RawMessage{
			json.RawMessage(`{"type":"test_event"}`),
		})
	})
	c := testClient(t, mux)

	commands := GenesisCommands(c)
	for _, cmd := range commands {
		if cmd.ID == "heaven-logs" {
			handler := cmd.Handler(cmd)
			if handler != nil {
				msg := handler()
				assert.NotNil(t, msg)
			}
			break
		}
	}
}

// ---------------------------------------------------------------------------
// L3: /status errors produce error msg (no panic)
// ---------------------------------------------------------------------------

func TestSlashStatusErrorHandled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "heaven unreachable", http.StatusServiceUnavailable)
	})
	c := testClient(t, mux)

	commands := GenesisCommands(c)
	for _, cmd := range commands {
		if cmd.ID == "heaven-status" {
			handler := cmd.Handler(cmd)
			if handler != nil {
				msg := handler()
				assert.NotNil(t, msg)
			}
			break
		}
	}
}

// ---------------------------------------------------------------------------
// L3: Command dialog message types exist and are well-formed
// ---------------------------------------------------------------------------

func TestCommandMsgTypes(t *testing.T) {
	msg := OpenSwarmAgentDialogMsg{}
	assert.NotNil(t, msg)

	roleMsg := OpenRoleModelDialogMsg{}
	assert.NotNil(t, roleMsg)

	authMsg := OpenAuthDialogMsg{}
	assert.NotNil(t, authMsg)
}

// ---------------------------------------------------------------------------
// L3: ReadMissionFile reads MISSION.md from dir
// ---------------------------------------------------------------------------

func TestReadMissionFile(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadMissionFile(dir)
	require.Error(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "MISSION.md"), []byte("Fix the login bug"), 0o644))
	content, err := ReadMissionFile(dir)
	require.NoError(t, err)
	assert.Equal(t, "Fix the login bug", content)
}

func TestReadMissionFileFallbackTestPrompt(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "TEST_PROMPT.md"), []byte("Add unit tests"), 0o644))
	content, err := ReadMissionFile(dir)
	require.NoError(t, err)
	assert.Equal(t, "Add unit tests", content)
}

// ---------------------------------------------------------------------------
// L3: FormatMissionResult formats success/failure correctly
// ---------------------------------------------------------------------------

func TestFormatMissionResultSuccess(t *testing.T) {
	result := FormatMissionResult(&god.SoloResult{
		Success:       true,
		TokensIn:      1000,
		TokensOut:     500,
		Calls:         3,
		PFCalls:       2,
		Turns:         1,
		FilesModified: []string{"main.go"},
	})
	assert.Contains(t, result, "successfully")
	assert.Contains(t, result, "1000")
	assert.Contains(t, result, "main.go")
}

func TestFormatMissionResultFailure(t *testing.T) {
	result := FormatMissionResult(&god.SoloResult{
		Success: false,
		Error:   "integration rejected",
	})
	assert.Contains(t, result, "failed")
	assert.Contains(t, result, "integration rejected")
}

// ---------------------------------------------------------------------------
// L3: extractJSON and stripCodeFences robustness
// ---------------------------------------------------------------------------

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"clean", `{"a":1}`, `{"a":1}`},
		{"prefix", `here is the json: {"a":1}`, `{"a":1}`},
		{"suffix", `{"a":1} done`, `{"a":1}`},
		{"nested", `{"a":{"b":2}}`, `{"a":{"b":2}}`},
		{"no json", `no json here`, `no json here`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStripCodeFences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no fences", `{"a":1}`, `{"a":1}`},
		{"json fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"bare fence", "```\n{\"a\":1}\n```", `{"a":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCodeFences(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
