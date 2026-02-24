package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/genesis-ssmp/genesis/cli/internal/genesis"
	"github.com/genesis-ssmp/genesis/cli/internal/mcpserver"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP server commands",
	Long:  `Run Genesis as a Remote MCP server for Claude Custom Connectors.`,
}

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Genesis MCP server",
	Long: `Start a Remote MCP server exposing Genesis Heaven/God tools.
Claude (or any MCP client) can connect via the SSE endpoint.

Examples:
  # Dev mode (no auth, localhost only)
  genesis mcp serve

  # Production (bearer token auth)
  genesis mcp serve --auth bearer --bearer-token "my-secret"

  # Custom address
  genesis mcp serve --addr 0.0.0.0:8080 --heaven-addr 10.0.0.1:4444`,
	RunE: func(cmd *cobra.Command, args []string) error {
		addr, _ := cmd.Flags().GetString("addr")
		heavenAddr, _ := cmd.Flags().GetString("heaven-addr")
		baseURL, _ := cmd.Flags().GetString("base-url")
		authMode, _ := cmd.Flags().GetString("auth")
		bearerToken, _ := cmd.Flags().GetString("bearer-token")
		allowOrigins, _ := cmd.Flags().GetStringSlice("allow-origins")

		cfg := mcpserver.Config{
			Addr:         addr,
			HeavenAddr:   heavenAddr,
			BaseURL:      baseURL,
			AuthMode:     authMode,
			BearerToken:  bearerToken,
			AllowOrigins: allowOrigins,
		}

		handler, sseSrv, err := mcpserver.New(cfg)
		if err != nil {
			return fmt.Errorf("create MCP server: %w", err)
		}

		srv := &http.Server{
			Addr:    cfg.Addr,
			Handler: handler,
		}

		// Graceful shutdown on signal
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

		go func() {
			<-stop
			fmt.Println("\nShutting down MCP server...")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			sseSrv.Shutdown(ctx)
			srv.Shutdown(ctx)
		}()

		fmt.Println("Genesis MCP Server")
		fmt.Println("========================================")
		fmt.Printf("  Listen:      %s\n", cfg.Addr)
		fmt.Printf("  Heaven:      %s\n", cfg.HeavenAddr)
		fmt.Printf("  Auth:        %s\n", cfg.AuthMode)
		fmt.Printf("  Tools:       %d\n", mcpserver.ToolCount)
		displayBase := fmt.Sprintf("http://%s", cfg.Addr)
		if cfg.BaseURL != "" {
			displayBase = cfg.BaseURL
		}
		fmt.Printf("  SSE URL:     %s/sse\n", displayBase)
		fmt.Printf("  Message URL: %s/message\n", displayBase)
		fmt.Println("========================================")
		fmt.Println("Paste this into Claude 'Add custom connector':")
		fmt.Printf("  URL: %s/sse\n", displayBase)
		fmt.Println()

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	},
}

var mcpDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check MCP server readiness",
	RunE: func(cmd *cobra.Command, args []string) error {
		addr, _ := cmd.Flags().GetString("addr")
		heavenAddr, _ := cmd.Flags().GetString("heaven-addr")
		authMode, _ := cmd.Flags().GetString("auth")

		fmt.Println("Genesis MCP Doctor")
		fmt.Println("========================================")
		fmt.Printf("  Listen addr:  %s\n", addr)
		fmt.Printf("  Heaven addr:  %s\n", heavenAddr)
		fmt.Printf("  Auth mode:    %s\n", authMode)
		fmt.Printf("  Tools:        %d\n", mcpserver.ToolCount)
		fmt.Println()

		// Check Heaven connectivity
		fmt.Print("  Heaven status: ")
		client := genesis.NewClient(heavenAddr)
		status, err := client.Status()
		if err != nil {
			fmt.Printf("UNREACHABLE (%v)\n", err)
		} else {
			fmt.Printf("OK (state_rev=%d, leases=%d, clocks=%d)\n",
				status.StateRev, len(status.Leases), len(status.Clocks))
		}

		fmt.Println()
		fmt.Println("Claude Custom Connector URL:")
		fmt.Printf("  http://%s/sse\n", addr)
		fmt.Println()
		fmt.Println("========================================")
		return nil
	},
}

func init() {
	// Serve flags
	mcpServeCmd.Flags().String("addr", "127.0.0.1:5555", "Listen address")
	mcpServeCmd.Flags().String("heaven-addr", "127.0.0.1:4444", "Heaven API address")
	mcpServeCmd.Flags().String("base-url", "", "External base URL (e.g. https tunnel URL) for SSE message endpoint")
	mcpServeCmd.Flags().String("auth", "none", "Auth mode: 'none' or 'bearer'")
	mcpServeCmd.Flags().String("bearer-token", "", "Bearer token (required if --auth=bearer)")
	mcpServeCmd.Flags().StringSlice("allow-origins", []string{"*"}, "CORS allowed origins")

	// Doctor flags (same defaults for display)
	mcpDoctorCmd.Flags().String("addr", "127.0.0.1:5555", "Listen address")
	mcpDoctorCmd.Flags().String("heaven-addr", "127.0.0.1:4444", "Heaven API address")
	mcpDoctorCmd.Flags().String("auth", "none", "Auth mode")

	mcpCmd.AddCommand(mcpServeCmd)
	mcpCmd.AddCommand(mcpDoctorCmd)
	rootCmd.AddCommand(mcpCmd)
}
