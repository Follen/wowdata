package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"wowdata/internal/server/config"
	"wowdata/internal/server/health"
	"wowdata/internal/server/mcphttp"
	serverruntime "wowdata/internal/server/runtime"
	"wowdata/internal/server/storage/artifacts"

	"github.com/spf13/cobra"
)

type httpOptions struct {
	ServiceName  string
	ConfigPath   string
	Host         string
	Port         int
	BaseURL      string
	ArtifactRoot string
}

type httpRunner func(httpOptions) error

func main() {
	cmd := newRootCommand()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	return newRootCommandWithHTTPRunner(runHTTPServer)
}

func newRootCommandWithHTTPRunner(runHTTP httpRunner) *cobra.Command {
	rt := serverruntime.New()
	cmd := &cobra.Command{
		Use:          "wowdata-server",
		Short:        "Serve wowdata MCP over HTTP.",
		SilenceUsage: true,
	}
	cmd.AddCommand(newMCPCommand(rt, runHTTP))
	return cmd
}

func newMCPCommand(rt *serverruntime.Runtime, runHTTP httpRunner) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run wowdata-server as an MCP server.",
	}
	cmd.AddCommand(newMCPHTTPCommand(rt, runHTTP))
	return cmd
}

func newMCPHTTPCommand(rt *serverruntime.Runtime, runHTTP httpRunner) *cobra.Command {
	opts := httpOptions{
		ServiceName: rt.ServiceName,
		Host:        "0.0.0.0",
		Port:        9788,
	}
	cmd := &cobra.Command{
		Use:   "http",
		Short: "Serve MCP tools over Streamable HTTP.",
		Long: `Serve MCP tools over Streamable HTTP.

Use this for remote deployments with a domain, TLS, reverse proxy, or shared service.
The MCP endpoint is /mcp. Health is available at /health.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if rt.ServiceName == "" {
				return fmt.Errorf("server runtime name is empty")
			}
			return runHTTP(opts)
		},
	}
	cmd.Flags().StringVar(&opts.ConfigPath, "config", "", "HTTP service config file")
	cmd.Flags().StringVar(&opts.Host, "host", opts.Host, "Host/interface to bind")
	cmd.Flags().IntVar(&opts.Port, "port", opts.Port, "Port to bind")
	cmd.Flags().StringVar(&opts.BaseURL, "base-url", "", "Public base URL used in help output")
	cmd.Flags().StringVar(&opts.ArtifactRoot, "artifact-root", "", "Directory served under /files/")
	return cmd
}

func runHTTPServer(opts httpOptions) error {
	addr := fmt.Sprintf("%s:%d", opts.Host, opts.Port)
	mux := newHTTPHandler(opts, defaultHealthProvider())
	fmt.Fprintf(os.Stderr, "wowdata-server MCP HTTP listening on http://%s/mcp\n", addr)
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server.ListenAndServe()
}

func newHTTPHandler(opts httpOptions, healthProvider health.Provider) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := mcphttp.HealthSnapshot(r.Context(), healthProvider)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"error":   "health_unavailable",
				"message": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotImplemented, map[string]interface{}{
			"error":   "mcp_not_wired",
			"message": "server MCP tools will be wired by the server service task",
		})
	})
	if opts.ArtifactRoot != "" {
		mux.Handle("/files/", artifacts.FileHandler(opts.ArtifactRoot))
	}
	return mux
}

func defaultHealthProvider() health.Provider {
	cfg := config.Default()
	targets := make([]health.TargetInput, 0, len(cfg.Prepare.Targets))
	for _, target := range cfg.Prepare.Targets {
		targets = append(targets, health.TargetInput{
			Label:   target.Label,
			Region:  target.Region,
			Product: target.Product,
			Locale:  target.Locale,
			Strict:  target.Strict,
			State:   health.StatePreparing,
		})
	}
	return health.StaticProvider{Snapshot: health.BuildSnapshot(health.Input{
		Targets: targets,
		Memory: health.Memory{
			MemorySoftLimitMB: cfg.Limits.MemorySoftLimitMB,
			MemoryHardLimitMB: cfg.Limits.MemoryHardLimitMB,
		},
	})}
}

func writeJSON(w http.ResponseWriter, code int, payload interface{}) {
	data, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(data)
}

func publicURL(baseURL, path string) string {
	if baseURL == "" {
		return path
	}
	return baseURL + path
}
