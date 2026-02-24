package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/genesis-ssmp/genesis/cli/internal/genesis"
	"github.com/genesis-ssmp/genesis/cli/internal/version"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerTools(srv *server.MCPServer, client *genesis.Client, heavenAddr string) {
	srv.AddTool(pingTool(), pingHandler(heavenAddr))
	srv.AddTool(statusTool(), statusHandler(client))
	srv.AddTool(eventsTool(), eventsHandler(client))
	srv.AddTool(irSearchTool(), irSearchHandler(client))
	srv.AddTool(irSymdefTool(), irSymdefHandler(client))
	srv.AddTool(irCallersTool(), irCallersHandler(client))
	srv.AddTool(irBuildTool(), irBuildHandler(client))
	srv.AddTool(leaseAcquireTool(), leaseAcquireHandler(client))
	srv.AddTool(validateManifestTool(), validateManifestHandler(client))
	srv.AddTool(blobStoreTool(), blobStoreHandler(client))
}

// ToolCount returns the number of tools registered.
const ToolCount = 10

// --- genesis.ping ---

func pingTool() mcp.Tool {
	return mcp.NewTool("genesis.ping",
		mcp.WithDescription("Health check. Returns server name, version, Heaven address, and current time."),
	)
}

func pingHandler(heavenAddr string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp := map[string]string{
			"server":      "genesis-mcp",
			"version":     version.Version,
			"heaven_addr": heavenAddr,
			"time":        time.Now().UTC().Format(time.RFC3339),
		}
		data, _ := json.Marshal(resp)
		return textResult(string(data)), nil
	}
}

// --- genesis.status ---

func statusTool() mcp.Tool {
	return mcp.NewTool("genesis.status",
		mcp.WithDescription("Get Heaven server status including state revision, active leases, and file clocks."),
	)
}

func statusHandler(client *genesis.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		status, err := client.Status()
		if err != nil {
			return toolError(fmt.Sprintf("heaven status: %v", err)), nil
		}
		data, _ := json.MarshalIndent(status, "", "  ")
		return textResult(string(data)), nil
	}
}

// --- genesis.events ---

func eventsTool() mcp.Tool {
	return mcp.NewTool("genesis.events",
		mcp.WithDescription("Tail recent Heaven events. Returns the last N events from the event log."),
		mcp.WithNumber("n", mcp.Description("Number of recent events to return (default 10, max 100)")),
	)
}

func eventsHandler(client *genesis.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		n := intParam(req, "n", 10)
		if n < 1 {
			n = 1
		}
		if n > 100 {
			n = 100
		}
		events, err := client.TailEvents(n)
		if err != nil {
			return toolError(fmt.Sprintf("tail events: %v", err)), nil
		}
		data, _ := json.MarshalIndent(events, "", "  ")
		return textResult(string(data)), nil
	}
}

// --- genesis.ir_search ---

func irSearchTool() mcp.Tool {
	return mcp.NewTool("genesis.ir_search",
		mcp.WithDescription("Semantic symbol search across the indexed repository. Returns matching symbols ranked by relevance."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query for symbol lookup")),
		mcp.WithNumber("top_k", mcp.Description("Maximum number of results (default 10, max 50)")),
	)
}

func irSearchHandler(client *genesis.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := stringParam(req, "query")
		topK := intParam(req, "top_k", 10)
		if topK > 50 {
			topK = 50
		}
		results, err := client.IRSearch(query, topK)
		if err != nil {
			return toolError(fmt.Sprintf("ir search: %v", err)), nil
		}
		data, _ := json.MarshalIndent(results, "", "  ")
		return textResult(string(data)), nil
	}
}

// --- genesis.ir_symdef ---

func irSymdefTool() mcp.Tool {
	return mcp.NewTool("genesis.ir_symdef",
		mcp.WithDescription("Look up symbol definitions by name. Returns file path, line, and column for each definition."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Symbol name to look up")),
	)
}

func irSymdefHandler(client *genesis.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := stringParam(req, "name")
		results, err := client.IRSymdef(name)
		if err != nil {
			return toolError(fmt.Sprintf("ir symdef: %v", err)), nil
		}
		data, _ := json.MarshalIndent(results, "", "  ")
		return textResult(string(data)), nil
	}
}

// --- genesis.ir_callers ---

func irCallersTool() mcp.Tool {
	return mcp.NewTool("genesis.ir_callers",
		mcp.WithDescription("Find callers of a symbol. Returns calling sites with file paths and positions."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Symbol name to find callers for")),
		mcp.WithNumber("top_k", mcp.Description("Maximum number of results (default 10, max 50)")),
	)
}

func irCallersHandler(client *genesis.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := stringParam(req, "name")
		topK := intParam(req, "top_k", 10)
		if topK > 50 {
			topK = 50
		}
		results, err := client.IRCallers(name, topK)
		if err != nil {
			return toolError(fmt.Sprintf("ir callers: %v", err)), nil
		}
		data, _ := json.MarshalIndent(results, "", "  ")
		return textResult(string(data)), nil
	}
}

// --- genesis.ir_build ---

func irBuildTool() mcp.Tool {
	return mcp.NewTool("genesis.ir_build",
		mcp.WithDescription("Index a repository for symbol search. Builds the IR index from source files."),
		mcp.WithString("repo_path", mcp.Required(), mcp.Description("Absolute path to the repository to index")),
	)
}

func irBuildHandler(client *genesis.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		repoPath := stringParam(req, "repo_path")
		symbols, err := client.IRBuild(repoPath)
		if err != nil {
			return toolError(fmt.Sprintf("ir build: %v", err)), nil
		}
		data, _ := json.Marshal(map[string]int{"symbols_indexed": symbols})
		return textResult(string(data)), nil
	}
}

// --- genesis.lease_acquire ---

func leaseAcquireTool() mcp.Tool {
	return mcp.NewTool("genesis.lease_acquire",
		mcp.WithDescription("Acquire file leases for a mission. Locks files for read or write access."),
		mcp.WithString("owner_id", mcp.Required(), mcp.Description("Identifier of the lease owner (agent ID)")),
		mcp.WithString("mission_id", mcp.Required(), mcp.Description("Mission identifier")),
		mcp.WithArray("scopes", mcp.Required(), mcp.Description("File scopes to lock: [{path, mode}] where mode is 'read' or 'write'")),
	)
}

func leaseAcquireHandler(client *genesis.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ownerID := stringParam(req, "owner_id")
		missionID := stringParam(req, "mission_id")

		var scopes []genesis.Scope
		if raw, ok := req.Params.Arguments["scopes"]; ok {
			scopeData, _ := json.Marshal(raw)
			json.Unmarshal(scopeData, &scopes)
		}

		result, err := client.LeaseAcquire(ownerID, missionID, scopes)
		if err != nil {
			return toolError(fmt.Sprintf("lease acquire: %v", err)), nil
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		return textResult(string(data)), nil
	}
}

// --- genesis.validate_manifest ---

func validateManifestTool() mcp.Tool {
	return mcp.NewTool("genesis.validate_manifest",
		mcp.WithDescription("Validate a mission manifest against the Heaven schema. Returns validation errors if any."),
		mcp.WithObject("manifest", mcp.Required(), mcp.Description("The mission manifest object to validate")),
	)
}

func validateManifestHandler(client *genesis.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		manifest := req.Params.Arguments["manifest"]
		result, err := client.ValidateManifest(manifest)
		if err != nil {
			return toolError(fmt.Sprintf("validate manifest: %v", err)), nil
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		return textResult(string(data)), nil
	}
}

// --- genesis.blob_store ---

func blobStoreTool() mcp.Tool {
	return mcp.NewTool("genesis.blob_store",
		mcp.WithDescription("Store content in the Heaven blob store. Returns the content-addressed hash."),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to store in the blob store")),
	)
}

func blobStoreHandler(client *genesis.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		content := stringParam(req, "content")
		hash, err := client.PutBlob([]byte(content))
		if err != nil {
			return toolError(fmt.Sprintf("blob store: %v", err)), nil
		}
		data, _ := json.Marshal(map[string]string{"hash": hash})
		return textResult(string(data)), nil
	}
}

// --- helpers ---

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.TextContent{Type: "text", Text: text}},
	}
}

func toolError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.TextContent{Type: "text", Text: msg}},
		IsError: true,
	}
}

func stringParam(req mcp.CallToolRequest, key string) string {
	if v, ok := req.Params.Arguments[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func intParam(req mcp.CallToolRequest, key string, defaultVal int) int {
	if v, ok := req.Params.Arguments[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return int(i)
			}
		}
	}
	return defaultVal
}
