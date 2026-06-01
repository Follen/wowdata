package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"wowdata/internal/app"
	"wowdata/internal/blte"
	"wowdata/internal/casc"
	"wowdata/internal/dbd"
	"wowdata/internal/diagnostics"
	"wowdata/internal/listfile"
	appruntime "wowdata/internal/runtime"
	"wowdata/internal/tact"
	"wowdata/internal/wowdata"

	"github.com/spf13/cobra"
)

// Runtime holds the live state that handlers share.
type Runtime struct {
	mu                 sync.Mutex
	httpWarmupMu       sync.Mutex
	httpWarmupGate     bool
	httpWarmupInFlight bool
	CacheRoot          string
	CASC               *casc.CASCRemote
	Local              *casc.CASCLocal
	LF                 *listfile.Listfile
	DB2                *appruntime.MemoryDB2Store
	Keys               *tact.KeyRing
	Spell              *wowdata.SpellService
	Enc                *wowdata.EncounterService
	Item               *wowdata.ItemService
	Creat              *wowdata.CreatureService
	Decor              *wowdata.DecorService
	Diag               *diagnostics.DiagnosticsService
}

func NewRuntime() *Runtime {
	return &Runtime{
		CacheRoot: resolveCacheRoot("", os.Executable),
		LF:        listfile.New(),
		DB2:       appruntime.NewMemoryDB2Store(),
		Spell:     wowdata.NewSpellService(),
		Enc:       wowdata.NewEncounterService(),
		Item:      wowdata.NewItemService(),
		Creat:     wowdata.NewCreatureService(),
		Decor:     wowdata.NewDecorService(),
		Diag:      diagnostics.NewDiagnosticsService(),
	}
}

func main() {
	rt := NewRuntime()
	cmd := newRootCommandForRuntime(rt)
	if len(os.Args) == 2 && os.Args[1] == "--mcp" {
		cmd.SetArgs([]string{"mcp", "stdio"})
	}
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommandForRuntime(rt *Runtime) *cobra.Command {
	fileStore := appruntime.NewCASCFileStore(rt.LF, nil, rt)

	svc := &app.Service{
		Warmup:    warmupHandler(rt),
		Casc:      cascHandler(rt),
		DB2:       app.NewDB2HandlerWithStore(rt.DB2),
		Spell:     app.NewSpellHandler(wowdata.NewSpellServiceWithDB2(rt.DB2)),
		Encounter: app.NewEncounterHandler(wowdata.NewEncounterServiceWithDB2(rt.DB2)),
		File:      app.NewFileHandlerWithStore(fileStore),
		Icon:      app.NewIconHandlerWithStore(fileStore),
		Item:      app.NewItemHandler(wowdata.NewItemServiceWithDB2(rt.DB2)),
		Creature:  app.NewCreatureHandler(wowdata.NewCreatureServiceWithDB2(rt.DB2)),
		Decor:     app.NewDecorHandler(wowdata.NewDecorServiceWithDB2(rt.DB2)),
		Video:     app.NewVideoHandler(),
		Golden:    app.NewGoldenHandler(),
	}

	cmd := app.NewRootCommandWithService(svc)
	registerMCPCommand(cmd, rt)
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		autoWarmup, _ := commandBoolFlag(cmd, "auto-warmup")
		if !autoWarmup || cmd.CommandPath() == "wowdata warmup" || strings.HasPrefix(cmd.CommandPath(), "wowdata mcp") {
			return nil
		}
		opts := warmupOptionsFromCommand(cmd)
		_, err := rt.initialize(opts)
		return err
	}
	return cmd
}

func (rt *Runtime) ReadFileData(fileDataID uint32) ([]byte, error) {
	rt.mu.Lock()
	cascSource := rt.CASC
	localSource := rt.Local
	rt.mu.Unlock()
	if cascSource != nil {
		return cascSource.ReadFileData(fileDataID)
	}
	if localSource != nil {
		return localSource.ReadFileData(fileDataID)
	}
	if cascSource == nil {
		return nil, fmt.Errorf("CASC 未就绪，请先调用 wow_warmup")
	}
	return nil, fmt.Errorf("CASC 未就绪，请先调用 wow_warmup")
}

func (rt *Runtime) FileExists(fileDataID uint32) bool {
	rt.mu.Lock()
	cascSource := rt.CASC
	localSource := rt.Local
	rt.mu.Unlock()
	if cascSource != nil {
		return cascSource.FileExists(fileDataID)
	}
	if localSource != nil {
		return localSource.FileExists(fileDataID)
	}
	return false
}

func (rt *Runtime) GetFileEncodingInfo(fileDataID uint32) (*casc.FileInfo, error) {
	rt.mu.Lock()
	cascSource := rt.CASC
	localSource := rt.Local
	rt.mu.Unlock()
	if cascSource != nil {
		return cascSource.GetFileEncodingInfo(fileDataID)
	}
	if localSource != nil {
		return localSource.GetFileEncodingInfo(fileDataID)
	}
	return nil, fmt.Errorf("CASC 未就绪，请先调用 wow_warmup")
}

func (rt *Runtime) enableHTTPWarmupGate() {
	rt.httpWarmupMu.Lock()
	rt.httpWarmupGate = true
	rt.httpWarmupMu.Unlock()
}

func (rt *Runtime) beginHTTPWarmup() (func(), error) {
	rt.httpWarmupMu.Lock()
	defer rt.httpWarmupMu.Unlock()
	if !rt.httpWarmupGate {
		return func() {}, nil
	}
	if rt.httpWarmupInFlight {
		return nil, warmupStepError{
			Code: "warmup_in_progress",
			Err:  fmt.Errorf("another wow_warmup is already running; wait for it to finish and reuse the warmed server context"),
		}
	}
	rt.httpWarmupInFlight = true
	return func() {
		rt.httpWarmupMu.Lock()
		rt.httpWarmupInFlight = false
		rt.httpWarmupMu.Unlock()
	}, nil
}

func warmupHandler(rt *Runtime) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("source") && !cmd.Flags().Changed("path") && !cmd.Flags().Changed("region") && !cmd.Flags().Changed("product") {
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("warmup", warmupPrompt(rt))
		}

		opts := warmupOptionsFromCommand(cmd)
		result, err := rt.initialize(opts)
		if err != nil {
			return writeWarmupInitError(cmd, err)
		}

		return app.NewResponseWriter(cmd.OutOrStdout()).Success("warmup", result)
	}
}

type warmupOptions struct {
	Source          string
	Path            string
	Region          string
	Product         string
	Locale          string
	CacheRoot       string
	Tables          []string
	WarmListfile    bool
	ListfileFormat  string
	WarmDBDManifest bool
}

type warmupStepError struct {
	Code string
	Err  error
}

func (e warmupStepError) Error() string {
	return e.Err.Error()
}

func warmupOptionsFromCommand(cmd *cobra.Command) warmupOptions {
	source, _ := commandStringFlag(cmd, "source")
	path, _ := commandStringFlag(cmd, "path")
	region, _ := commandStringFlag(cmd, "region")
	product, _ := commandStringFlag(cmd, "product")
	locale, _ := commandStringFlag(cmd, "locale")
	cacheRoot, _ := commandStringFlag(cmd, "cache")
	tablesFlag, _ := commandStringFlag(cmd, "tables")
	warmListfile, _ := commandBoolFlag(cmd, "listfile")
	listfileFormat, _ := commandStringFlag(cmd, "listfile-format")
	warmDBDManifest, _ := commandBoolFlag(cmd, "dbd-manifest")
	return warmupOptions{
		Source:          source,
		Path:            path,
		Region:          region,
		Product:         product,
		Locale:          locale,
		CacheRoot:       resolveCacheRoot(cacheRoot, os.Executable),
		Tables:          splitCSV(tablesFlag),
		WarmListfile:    warmListfile,
		ListfileFormat:  listfileFormat,
		WarmDBDManifest: warmDBDManifest,
	}
}

func commandStringFlag(cmd *cobra.Command, name string) (string, error) {
	if flag := cmd.Flags().Lookup(name); flag != nil {
		return cmd.Flags().GetString(name)
	}
	if flag := cmd.InheritedFlags().Lookup(name); flag != nil {
		return cmd.InheritedFlags().GetString(name)
	}
	return cmd.Root().PersistentFlags().GetString(name)
}

func commandBoolFlag(cmd *cobra.Command, name string) (bool, error) {
	if flag := cmd.Flags().Lookup(name); flag != nil {
		return cmd.Flags().GetBool(name)
	}
	if flag := cmd.InheritedFlags().Lookup(name); flag != nil {
		return cmd.InheritedFlags().GetBool(name)
	}
	return cmd.Root().PersistentFlags().GetBool(name)
}

func writeWarmupInitError(cmd *cobra.Command, err error) error {
	if step, ok := err.(warmupStepError); ok {
		return app.NewResponseWriter(cmd.OutOrStdout()).Error("warmup", step.Code, step.Err.Error())
	}
	return app.NewResponseWriter(cmd.OutOrStdout()).Error("warmup", "init_failed", err.Error())
}

func (rt *Runtime) initialize(opts warmupOptions) (map[string]interface{}, error) {
	if opts.CacheRoot != "" {
		rt.CacheRoot = opts.CacheRoot
	}
	result := map[string]interface{}{
		"source":  opts.Source,
		"region":  opts.Region,
		"product": opts.Product,
		"locale":  opts.Locale,
		"cache":   filepath.ToSlash(opts.CacheRoot),
		"warm": map[string]interface{}{
			"listfile":       opts.WarmListfile,
			"listfileFormat": opts.ListfileFormat,
			"dbdManifest":    opts.WarmDBDManifest,
			"tables":         opts.Tables,
		},
	}

	switch opts.Source {
	case "remote":
		remote := casc.NewCASCRemote(opts.Region)
		remote.CacheRoot = opts.CacheRoot
		if err := applyWarmupLocale(remote.CASCSource, opts.Locale); err != nil {
			return result, warmupStepError{Code: "invalid_locale", Err: err}
		}
		if err := remote.Init(); err != nil {
			return result, warmupStepError{Code: "init_failed", Err: err}
		}
		buildIdx := buildIndexByProduct(remote.Builds, opts.Product)
		if buildIdx < 0 {
			result["status"] = "no_build"
			result["builds"] = convertProducts(remote.GetProductList())
			return result, nil
		}
		if err := remote.Preload(buildIdx); err != nil {
			return result, warmupStepError{Code: "preload_failed", Err: err}
		}

		rt.mu.Lock()
		rt.CASC = remote
		rt.Local = nil
		rt.DB2.Reset()
		rt.Diag.SetInfo(diagnostics.CASCInfo{
			Source:             opts.Source,
			Region:             opts.Region,
			Product:            opts.Product,
			Locale:             remote.Locale.Name(),
			BuildName:          remote.GetBuildName(),
			BuildKey:           remote.GetBuildKey(),
			CachePath:          remote.Cache.Root(),
			CDNHost:            remote.Host,
			ArchiveCount:       len(remote.Archives),
			RootEntryCount:     len(remote.RootEntries),
			EncodingEntryCount: len(remote.EncodingEntries),
		})
		rt.Diag.SetProducts(diagnostics.CASCProducts{
			Source:   opts.Source,
			Products: convertProducts(remote.GetProductList()),
		})
		rt.mu.Unlock()

		result["status"] = "ok"
		result["buildName"] = remote.GetBuildName()
		result["buildKey"] = remote.GetBuildKey()
		result["rootEntryCount"] = len(remote.RootEntries)
		result["encodingEntryCount"] = len(remote.EncodingEntries)
		result["archiveCount"] = len(remote.Archives)
		addWarmupSuccessFields(result, buildIdx, remote.GetBuildName())

	case "local":
		if opts.Path == "" {
			return result, warmupStepError{Code: "missing_argument", Err: fmt.Errorf("--path is required for local source")}
		}
		local := casc.NewCASCLocal(opts.Path)
		if err := applyWarmupLocale(local.CASCSource, opts.Locale); err != nil {
			return result, warmupStepError{Code: "invalid_locale", Err: err}
		}
		if err := local.Init(); err != nil {
			return result, warmupStepError{Code: "init_failed", Err: err}
		}
		buildIdx := buildIndexByProduct(local.Builds, opts.Product)
		if buildIdx < 0 {
			result["status"] = "no_build"
			result["builds"] = convertProducts(local.GetProductList())
			return result, nil
		}
		if err := local.Load(buildIdx); err != nil {
			return result, warmupStepError{Code: "preload_failed", Err: err}
		}

		rt.mu.Lock()
		rt.Local = local
		rt.CASC = nil
		rt.DB2.Reset()
		rt.Diag.SetInfo(diagnostics.CASCInfo{
			Source:             opts.Source,
			Region:             opts.Region,
			Product:            opts.Product,
			Locale:             local.Locale.Name(),
			BuildName:          local.GetBuildName(),
			BuildKey:           local.GetBuildKey(),
			CachePath:          filepath.ToSlash(opts.Path),
			ArchiveCount:       len(local.Archives),
			RootEntryCount:     len(local.RootEntries),
			EncodingEntryCount: len(local.EncodingEntries),
		})
		rt.Diag.SetProducts(diagnostics.CASCProducts{
			Source:   opts.Source,
			Products: convertProducts(local.GetProductList()),
		})
		rt.mu.Unlock()

		result["status"] = "ok"
		result["buildName"] = local.GetBuildName()
		result["buildKey"] = local.GetBuildKey()
		result["rootEntryCount"] = len(local.RootEntries)
		result["encodingEntryCount"] = len(local.EncodingEntries)
		result["localIndexCount"] = len(local.LocalIndexes)
		addWarmupSuccessFields(result, buildIdx, local.GetBuildName())

	default:
		return result, warmupStepError{Code: "invalid_source", Err: fmt.Errorf("source must be remote or local")}
	}

	if opts.WarmListfile {
		if err := rt.warmListfile(opts.ListfileFormat); err != nil {
			return result, warmupStepError{Code: "listfile_failed", Err: err}
		}
	}
	if err := rt.warmTACTKeys(); err != nil {
		return result, warmupStepError{Code: "tact_keys_failed", Err: err}
	}
	if opts.WarmDBDManifest {
		if err := rt.warmDBDManifest(); err != nil {
			return result, warmupStepError{Code: "dbd_manifest_failed", Err: err}
		}
	}
	if len(opts.Tables) > 0 {
		if err := rt.warmDB2Tables(opts.Product, opts.Tables); err != nil {
			return result, warmupStepError{Code: "db2_warm_failed", Err: err}
		}
	}
	return result, nil
}

func addWarmupSuccessFields(result map[string]interface{}, buildIndex int, buildName string) {
	result["success"] = true
	result["message"] = "warmup 完成"
	result["buildIndex"] = buildIndex
	result["buildName"] = buildName
	result["note"] = "首次预热可能接近 240 秒；后续会优先使用缓存。"
	warmed := map[string]interface{}{}
	if warm, ok := result["warm"].(map[string]interface{}); ok {
		warmed["listfile"], _ = warm["listfile"]
		warmed["dbdManifest"], _ = warm["dbdManifest"]
		warmed["tables"] = normalizeWarmupTables(warm["tables"])
	}
	result["warmed"] = warmed
}

func resolveCacheRoot(cacheFlag string, executable func() (string, error)) string {
	if strings.TrimSpace(cacheFlag) != "" {
		return cacheFlag
	}
	exe, err := executable()
	if err != nil || exe == "" {
		return "cache"
	}
	return filepath.Join(filepath.Dir(exe), "cache")
}

func applyWarmupLocale(source *casc.CASCSource, locale string) error {
	if source == nil {
		return nil
	}
	flag, ok := casc.LocaleFlagByNameOK(locale)
	if !ok {
		return fmt.Errorf("unsupported locale %q", locale)
	}
	source.Locale = flag
	return nil
}

func normalizeWarmupTables(tables interface{}) interface{} {
	switch v := tables.(type) {
	case []string:
		if len(v) == 0 {
			return []string{}
		}
		return v
	case []interface{}:
		if len(v) == 0 {
			return []interface{}{}
		}
		return v
	case nil:
		return []string{}
	default:
		return v
	}
}

func (rt *Runtime) warmTACTKeys() error {
	keyRing, err := tact.LoadKeyRing(filepath.Join(rt.CacheRoot, "tact.json"), []string{
		"https://raw.githubusercontent.com/wowdev/TACTKeys/master/WoW.txt",
		"https://www.kruithne.net/wow.export/data/tact/wow",
	})
	if err != nil {
		return err
	}
	blte.SetDefaultKeyProvider(keyRing)
	rt.mu.Lock()
	rt.Keys = keyRing
	info := rt.Diag.GetInfo()
	info.TACTKeyCount = keyRing.Count()
	rt.Diag.SetInfo(info)
	rt.mu.Unlock()
	return nil
}

func buildIndexByProduct(builds []casc.VersionEntry, product string) int {
	for i, build := range builds {
		if build.Product == product {
			return i
		}
	}
	return -1
}

func warmupPrompt(rt *Runtime) map[string]interface{} {
	connected := false
	products := []diagnostics.Product{}
	if rt != nil {
		rt.mu.Lock()
		connected = rt.CASC != nil || rt.Local != nil
		if rt.CASC != nil {
			products = convertProducts(rt.CASC.GetProductList())
		} else if rt.Local != nil {
			products = convertProducts(rt.Local.GetProductList())
		}
		rt.mu.Unlock()
	}
	return map[string]interface{}{
		"action":  "choose_warmup_target",
		"message": "请选择要预热的 WoW 数据源和客户端版本。预热可能需要较久，模型应耐心等待，建议工具超时 240 秒。",
		"sources": []map[string]interface{}{
			{"source": "local", "label": "本地客户端", "required": []string{"path"}, "optional": []string{"region"}},
			{"source": "remote", "label": "远端 CDN", "required": []string{"region"}, "optional": []string{}},
		},
		"regions": []map[string]interface{}{
			{"region": "cn", "label": "中国"},
			{"region": "us", "label": "美洲"},
			{"region": "eu", "label": "欧洲"},
			{"region": "kr", "label": "韩国"},
			{"region": "tw", "label": "台湾"},
		},
		"productHints": []map[string]interface{}{
			{"product": "wow", "label": "Retail"},
			{"product": "wow_classic", "label": "Classic"},
			{"product": "wow_classic_titan", "label": "Titan Reforged"},
			{"product": "wow_classic_era", "label": "Classic Era"},
			{"product": "wowt", "label": "PTR"},
			{"product": "wowxptr", "label": "Beta"},
		},
		"connected": connected,
		"products":  products,
		"examples": []map[string]interface{}{
			{"source": "remote", "region": "cn"},
			{"source": "remote", "region": "cn", "product": "wow"},
			{"source": "local", "path": "D:/World of Warcraft/_retail_", "region": "cn", "product": "wow"},
		},
	}
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (rt *Runtime) warmDB2Tables(product string, tables []string) error {
	rt.mu.Lock()
	cascSource := rt.CASC
	localSource := rt.Local
	store := rt.DB2
	rt.mu.Unlock()
	if cascSource == nil && localSource == nil {
		return fmt.Errorf("CASC runtime is not initialized")
	}
	manifest, err := rt.loadDBDManifest()
	if err != nil {
		return err
	}
	dbdSource := appruntime.NewHTTPDBDSource(rt.dbdCacheDir(), []string{
		"https://raw.githubusercontent.com/wowdev/WoWDBDefs/refs/heads/master/definitions/%s.dbd",
		"https://www.kruithne.net/wow.export/data/dbd/?def=%s",
	})
	var reader interface {
		ReadFileData(uint32) ([]byte, error)
		GetBuildName() string
	}
	if cascSource != nil {
		reader = cascSource
	} else {
		reader = localSource
	}
	loader := appruntime.NewDB2Loader(manifest, dbdSource, reader, reader.GetBuildName())
	for _, table := range tables {
		if err := loader.LoadTable(store, table); err != nil {
			return fmt.Errorf("warm DB2 table %s: %w", table, err)
		}
	}
	return nil
}

func (rt *Runtime) warmDBDManifest() error {
	_, err := rt.loadDBDManifest()
	return err
}

func (rt *Runtime) warmListfile(format string) error {
	source := appruntime.NewHTTPListfileSource(filepath.Join(rt.CacheRoot, "listfile"), []string{
		"https://github.com/wowdev/wow-listfile/releases/latest/download/community-listfile.csv",
		"https://www.kruithne.net/wow.export/data/listfile/master",
	})
	if format == "" || format == "binary" {
		source = source.WithBinaryURLs([]string{
			"https://www.kruithne.net/wow.export/data/listfile/bin/%s",
		})
	} else if format != "text" {
		return fmt.Errorf("--listfile-format must be binary or text")
	} else {
		source = source.WithTextOnly()
	}
	lf, err := source.Listfile()
	if err != nil {
		return err
	}
	rt.mu.Lock()
	rt.LF.ReplaceFrom(lf)
	rt.mu.Unlock()
	return nil
}

func rootEntryMap(source *casc.CASCSource) map[uint32]bool {
	valid := make(map[uint32]bool)
	if source == nil {
		return valid
	}
	for fdid := range source.RootEntries {
		valid[fdid] = true
	}
	return valid
}

func (rt *Runtime) loadDBDManifest() (*dbd.Manifest, error) {
	manifestSource := appruntime.NewHTTPDBDManifestSource(rt.dbdCacheDir(), []string{
		"https://raw.githubusercontent.com/wowdev/WoWDBDefs/refs/heads/master/manifest.json",
		"https://www.kruithne.net/wow.export/data/dbd",
	})
	return manifestSource.Manifest()
}

func (rt *Runtime) dbdCacheDir() string {
	return filepath.Join(rt.CacheRoot, "dbd")
}

func cascHandler(rt *Runtime) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		use := cmd.Name()
		switch use {
		case "info":
			if rt.Diag.GetInfo().Source == "" {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("casc info", "not_ready", "CASC 未就绪，请先调用 wow_warmup")
			}
			info := rt.Diag.GetInfo()
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("casc info", info)

		case "products":
			source, _ := cmd.Flags().GetString("source")
			path, _ := cmd.Flags().GetString("path")
			region, _ := cmd.Flags().GetString("region")

			var products []diagnostics.Product
			if source == "local" {
				if path == "" {
					rt.mu.Lock()
					local := rt.Local
					rt.mu.Unlock()
					if local == nil {
						return app.NewResponseWriter(cmd.OutOrStdout()).Error("casc products", "missing_argument", "--path is required for local source unless a local source is already warmed")
					}
					products = convertProducts(local.GetProductList())
				} else {
					local := casc.NewCASCLocal(path)
					if err := local.Init(); err != nil {
						return app.NewResponseWriter(cmd.OutOrStdout()).Error("casc products", "init_failed", err.Error())
					}
					products = convertProducts(local.GetProductList())
				}
			} else if rt.CASC != nil {
				products = convertProducts(rt.CASC.GetProductList())
			} else {
				remote := casc.NewCASCRemote(region)
				if err := remote.Init(); err != nil {
					return app.NewResponseWriter(cmd.OutOrStdout()).Error("casc products", "init_failed", err.Error())
				}
				products = convertProducts(remote.GetProductList())
			}

			return app.NewResponseWriter(cmd.OutOrStdout()).Success("casc products", diagnostics.CASCProducts{
				Source:   source,
				Products: products,
			})

		case "diagnose":
			if rt.Diag.GetInfo().Source == "" {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("casc diagnose", "not_ready", "CASC 未就绪，请先调用 wow_warmup")
			}
			d := rt.Diag.Diagnose()
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("casc diagnose", d)

		default:
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("casc", map[string]interface{}{
				"subcommands": []string{"info", "products", "diagnose"},
				"note":        "use 'wowdata warmup' first to initialize a CASC context",
			})
		}
	}
}

func convertProducts(src []casc.Product) []diagnostics.Product {
	out := make([]diagnostics.Product, len(src))
	for i, p := range src {
		out[i] = diagnostics.Product{Label: p.Label, BuildIndex: p.BuildIndex}
	}
	return out
}

var _ = (*cobra.Command)(nil)
