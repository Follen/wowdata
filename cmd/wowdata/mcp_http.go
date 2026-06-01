package main

import (
	"fmt"
	"net/http"
	"time"

	"wowdata/internal/config"

	"github.com/spf13/cobra"
)

func newMCPHTTPCommand(rt *Runtime) *cobra.Command {
	var configPath string
	var httpHost, httpBaseURL string
	var httpArtifactRoot, httpArtifactBaseURL string
	var httpPort int
	var httpMaxContexts int

	httpCmd := &cobra.Command{
		Use:   "http",
		Short: "Serve MCP tools over Streamable HTTP.",
		Long: `Serve MCP tools over Streamable HTTP.

Use this for remote deployments with a domain, TLS, reverse proxy, or shared service.
The MCP endpoint is /mcp. Health and agent-readable setup guidance are available at /health and /help.

Codex CLI:
  codex mcp add wowdata --url https://mcp.example.com:9443/mcp

Claude Code:
  claude mcp add --transport http wowdata https://mcp.example.com:9443/mcp

Claude Code stdio fallback:
  claude mcp add --transport stdio wowdata -- wowdata mcp stdio

cc-switch custom MCP:
  {
    "type": "http",
    "url": "https://mcp.example.com:9443/mcp"
  }

Compatibility notes:
  The HTTP server accepts GET, HEAD, OPTIONS, and POST on /mcp.
  JSON-RPC notifications such as notifications/initialized return HTTP 202 with no JSON-RPC error.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			loaded, err := config.LoadHTTPConfig(configPath)
			if err != nil {
				return err
			}
			cfg := loaded
			applyHTTPFlagOverrides(cmd, &cfg)

			syncRuntimeFromPersistentFlags(cmd, rt)
			rt.enableHTTPWarmupGate()
			rt.enableContextCache(cfg.Contexts.MaxContexts)
			svc, closeService, err := NewHTTPServiceRuntime(cfg, rt)
			if err != nil {
				return err
			}
			defer closeService()

			server := newMCPHTTPServerForService(svc, rt, artifactConfig{
				root:    cfg.Artifacts.Root,
				baseURL: cfg.Artifacts.BaseURL,
			})
			addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
			mux := http.NewServeMux()
			registerMCPHTTPHandlers(mux, server, cfg.Server.BaseURL, cfg.Cache.Root, cfg.Artifacts.Root)
			httpServer := &http.Server{
				Addr:              addr,
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "wowdata MCP HTTP listening on http://%s/mcp\n", addr)
			return httpServer.ListenAndServe()
		},
	}
	httpCmd.Flags().StringVar(&configPath, "config", "", "HTTP service config file")
	httpCmd.Flags().StringVar(&httpHost, "host", "127.0.0.1", "Host/interface to bind")
	httpCmd.Flags().IntVar(&httpPort, "port", 9788, "Port to bind")
	httpCmd.Flags().StringVar(&httpBaseURL, "base-url", "", "Public base URL used in help output, such as https://mcp.example.com:9443")
	httpCmd.Flags().StringVar(&httpArtifactRoot, "artifact-root", "", "Local directory exposed by the reverse proxy for exported artifacts")
	httpCmd.Flags().StringVar(&httpArtifactBaseURL, "artifact-base-url", "", "Public base URL for exported artifacts, such as https://mcp.example.com:9443/files")
	httpCmd.Flags().IntVar(&httpMaxContexts, "max-contexts", 1, "Maximum warmed build contexts to keep in memory for HTTP MCP")

	return httpCmd
}

func applyHTTPFlagOverrides(cmd *cobra.Command, cfg *config.HTTPConfig) {
	if cmd.Flags().Changed("host") || cmd.Flags().Lookup("config").Value.String() == "" {
		value, _ := cmd.Flags().GetString("host")
		cfg.Server.Host = value
	}
	if cmd.Flags().Changed("port") || cmd.Flags().Lookup("config").Value.String() == "" {
		value, _ := cmd.Flags().GetInt("port")
		cfg.Server.Port = value
	}
	if cmd.Flags().Changed("base-url") || cmd.Flags().Lookup("config").Value.String() == "" {
		value, _ := cmd.Flags().GetString("base-url")
		cfg.Server.BaseURL = value
	}
	if cmd.Flags().Changed("artifact-root") || cmd.Flags().Lookup("config").Value.String() == "" {
		value, _ := cmd.Flags().GetString("artifact-root")
		cfg.Artifacts.Root = value
	}
	if cmd.Flags().Changed("artifact-base-url") || cmd.Flags().Lookup("config").Value.String() == "" {
		value, _ := cmd.Flags().GetString("artifact-base-url")
		cfg.Artifacts.BaseURL = value
	}
	if cmd.Flags().Changed("max-contexts") || cmd.Flags().Lookup("config").Value.String() == "" {
		value, _ := cmd.Flags().GetInt("max-contexts")
		cfg.Contexts.MaxContexts = value
	}
}
