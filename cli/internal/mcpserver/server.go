package mcpserver

import (
	"fmt"
	"net/http"

	"github.com/genesis-ssmp/genesis/cli/internal/genesis"
	"github.com/genesis-ssmp/genesis/cli/internal/version"
	"github.com/mark3labs/mcp-go/server"
)

// Config holds MCP server configuration.
type Config struct {
	Addr         string   // listen address (default "127.0.0.1:5555")
	HeavenAddr   string   // Heaven API address (default "127.0.0.1:4444")
	BaseURL      string   // external base URL for SSE message endpoint (e.g. tunnel URL)
	AuthMode     string   // "none" or "bearer"
	BearerToken  string   // required when AuthMode="bearer"
	AllowOrigins []string // CORS allowed origins
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Addr:         "127.0.0.1:5555",
		HeavenAddr:   "127.0.0.1:4444",
		AuthMode:     "none",
		AllowOrigins: []string{"*"},
	}
}

// Validate checks that the config is valid.
func (c Config) Validate() error {
	if c.Addr == "" {
		return fmt.Errorf("listen address is required")
	}
	if c.HeavenAddr == "" {
		return fmt.Errorf("heaven address is required")
	}
	switch c.AuthMode {
	case "none":
		// ok
	case "bearer":
		if c.BearerToken == "" {
			return fmt.Errorf("--bearer-token is required when --auth=bearer")
		}
	default:
		return fmt.Errorf("unknown auth mode: %q (use 'none' or 'bearer')", c.AuthMode)
	}
	return nil
}

// New creates and configures the MCP SSE server with all Genesis tools registered.
// Returns an http.Handler ready to serve, and the underlying SSEServer for lifecycle management.
func New(cfg Config) (http.Handler, *server.SSEServer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}

	// Create the MCP protocol server
	mcpSrv := server.NewMCPServer(
		"genesis-mcp",
		version.Version,
		server.WithToolCapabilities(false),
		server.WithInstructions("Genesis SSMP MCP server. Exposes Heaven/God APIs for code analysis, symbol search, lease management, and mission orchestration."),
	)

	// Register all Genesis tools
	client := genesis.NewClient(cfg.HeavenAddr)
	registerTools(mcpSrv, client, cfg.HeavenAddr)

	// Create SSE transport server
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://%s", cfg.Addr)
	}
	sseSrv := server.NewSSEServer(mcpSrv,
		server.WithBaseURL(baseURL),
	)

	// Build middleware chain
	var handler http.Handler = sseSrv

	// Rate limiting: 120 requests/minute
	handler = rateLimitMiddleware(120, handler)

	// CORS
	handler = corsMiddleware(cfg.AllowOrigins, handler)

	// Auth
	if cfg.AuthMode == "bearer" {
		handler = authMiddleware(cfg.BearerToken, handler)
	}

	// Logging (outermost)
	handler = loggingMiddleware(handler)

	return handler, sseSrv, nil
}
