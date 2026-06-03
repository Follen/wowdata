package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	serverbootstrap "wowdata/internal/server/bootstrap"
	"wowdata/internal/server/config"
	"wowdata/internal/server/health"
	"wowdata/internal/server/mcphttp"
	"wowdata/internal/server/refresh"
	serverruntime "wowdata/internal/server/runtime"
	"wowdata/internal/server/service"
	"wowdata/internal/server/storage/artifacts"
	"wowdata/internal/server/storage/metadata"
	"wowdata/internal/server/storage/rawcache"
	"wowdata/internal/shared/mcpserver"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	defaultHTTPHost = "0.0.0.0"
	defaultHTTPPort = 9788
)

type httpOptions struct {
	ServiceName    string
	ConfigPath     string
	Host           string
	Port           int
	HostExplicit   bool
	PortExplicit   bool
	BaseURL        string
	ArtifactRoot   string
	MetadataDBPath string
	QueryService   service.QueryService
	Bootstrap      prepareBootstrap
}

type httpRunner func(httpOptions) error
type prepareBootstrap func(context.Context, serverbootstrap.Config, *sql.DB) error

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
		Host:        defaultHTTPHost,
		Port:        defaultHTTPPort,
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
			opts.HostExplicit = cmd.Flags().Changed("host")
			opts.PortExplicit = cmd.Flags().Changed("port")
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
	effectiveOpts, err := resolveHTTPOptions(opts)
	if err != nil {
		return err
	}
	opts = effectiveOpts
	mux, err := newHTTPHandlerStrict(opts, nil)
	if err != nil {
		return err
	}
	closer, closeable := mux.(interface{ Close() error })
	addr := fmt.Sprintf("%s:%d", opts.Host, opts.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		if closeable {
			_ = closer.Close()
		}
		return err
	}
	if closeable {
		defer closer.Close()
	}
	fmt.Fprintf(os.Stderr, "wowdata-server MCP HTTP listening on http://%s/mcp\n", addr)
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server.Serve(listener)
}

func resolveHTTPOptions(opts httpOptions) (httpOptions, error) {
	cfg, err := loadServerConfig(opts.ConfigPath)
	if err != nil {
		return opts, err
	}
	if !opts.HostExplicit && cfg.Server.Host != "" {
		opts.Host = cfg.Server.Host
	}
	if !opts.PortExplicit && cfg.Server.Port != 0 {
		opts.Port = cfg.Server.Port
	}
	if opts.BaseURL == "" && cfg.Server.BaseURL != "" {
		opts.BaseURL = cfg.Server.BaseURL
	}
	return opts, nil
}

func newHTTPHandler(opts httpOptions, healthProvider health.Provider) http.Handler {
	handler, err := newHTTPHandlerStrict(opts, healthProvider)
	if err != nil {
		return errorHandler(http.StatusInternalServerError, map[string]interface{}{
			"error":   "handler_unavailable",
			"message": err.Error(),
		})
	}
	return handler
}

func newHTTPHandlerStrict(opts httpOptions, healthProvider health.Provider) (http.Handler, error) {
	cfg, err := loadServerConfig(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	var sharedMetadataDB *sql.DB
	if healthProvider == nil {
		metadataDBPath := opts.MetadataDBPath
		if metadataDBPath == "" {
			metadataDBPath = cfg.Cache.MetadataDB
		}
		metadataDB, err := metadata.Open(metadataDBPath)
		if err != nil {
			return nil, err
		}
		sharedMetadataDB = metadataDB
		healthProvider = health.NewMetadataProviderWithDB(cfg, metadataDBPath, metadataDB)
		startBootstrap := opts.Bootstrap
		if startBootstrap == nil {
			startBootstrap = serverbootstrap.StartBackground
		}
		if err := startBootstrap(context.Background(), cfg, metadataDB); err != nil {
			_ = metadataDB.Close()
			return nil, err
		}
	}
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
	queryService := opts.QueryService
	var assetService service.AssetService
	if queryService == nil {
		if metadataProvider, ok := healthProvider.(health.MetadataProvider); ok {
			queryService = service.NewMetadataQueryServiceWithDBAndRuntime(metadataProvider.DB(), cfg.Cache.DuckDBPath, cfg.Cache.Root)
		} else {
			metadataDBPath := opts.MetadataDBPath
			if metadataDBPath == "" {
				metadataDBPath = cfg.Cache.MetadataDB
			}
			queryService = service.NewMetadataQueryServiceWithRuntime(metadataDBPath, cfg.Cache.DuckDBPath, cfg.Cache.Root)
		}
	}
	if metadataProvider, ok := healthProvider.(health.MetadataProvider); ok {
		artifactRoot := opts.ArtifactRoot
		if artifactRoot == "" {
			artifactRoot = cfg.Artifacts.Root
		}
		assetService = service.NewAssetService(
			metadataProvider.DB(),
			rawcache.NewWithLimits(cfg.Cache.RawDir, cfg.Cache.CASCDiskLimitMB*1024*1024, cfg.Cache.CASCDiskTargetMB*1024*1024),
			artifacts.NewStore(artifacts.Config{Root: artifactRoot, BaseURL: artifactBaseURL(cfg, opts.BaseURL)}, metadataProvider.DB()),
			nil,
		)
	}
	mcpServer := mcpserver.NewServer("wowdata", mcphttp.HTTPTools(mcphttp.Options{
		HealthProvider:   healthProvider,
		QueryService:     queryService,
		AssetService:     assetService,
		SupportedTargets: supportedMCPTargets(),
	}))
	mux.Handle("/mcp", mcpServer)
	mux.Handle("/mcp/", mcpServer)
	artifactRoot := opts.ArtifactRoot
	if artifactRoot == "" {
		artifactRoot = cfg.Artifacts.Root
	}
	if artifactRoot != "" {
		mux.Handle("/files/", artifacts.FileHandler(artifactRoot))
	}
	if cfg.Server.EnableUpdateFixtures {
		mux.HandleFunc("/test/update-flow/", updateFlowFixtureHandler)
	}
	if sharedMetadataDB != nil {
		return closeableHandler{Handler: mux, close: sharedMetadataDB.Close}, nil
	}
	return mux, nil
}

func supportedMCPTargets() []mcphttp.SupportedTarget {
	defaultCfg := config.Default()
	targets := make([]mcphttp.SupportedTarget, 0, len(defaultCfg.Prepare.Targets))
	for _, target := range defaultCfg.Prepare.Targets {
		targets = append(targets, mcphttp.SupportedTarget{
			Region:  target.Region,
			Product: target.Product,
			Locale:  target.Locale,
		})
	}
	return targets
}

func updateFlowFixtureHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	check := strings.TrimPrefix(r.URL.Path, "/test/update-flow/")
	if check == "" || strings.Contains(check, "/") {
		http.NotFound(w, r)
		return
	}
	result, err := refresh.RunUpdateFixture(r.Context(), check)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"passed": false, "error": err.Error()})
		return
	}
	status := http.StatusOK
	if !result.Passed {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, result)
}

func filesBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return ""
	}
	return baseURL + "/files"
}

func artifactBaseURL(cfg config.Config, serverBaseURL string) string {
	if cfg.Artifacts.BaseURL != "" {
		return cfg.Artifacts.BaseURL
	}
	return filesBaseURL(serverBaseURL)
}

type closeableHandler struct {
	http.Handler
	close func() error
}

func (h closeableHandler) Close() error {
	if h.close == nil {
		return nil
	}
	return h.close()
}

func loadServerConfig(path string) (config.Config, error) {
	cfg := config.Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func errorHandler(status int, payload interface{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, status, payload)
	})
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
