package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wowdata/internal/app"
	"wowdata/internal/blte"
	"wowdata/internal/casc"
	"wowdata/internal/dbd"
	"wowdata/internal/diagnostics"
	"wowdata/internal/listfile"
	appruntime "wowdata/internal/runtime"
	"wowdata/internal/storage"
	"wowdata/internal/tact"
	"wowdata/internal/wowdata"

	"github.com/spf13/cobra"
)

// Runtime holds the live state that handlers share.
type Runtime struct {
	mu        sync.Mutex
	CacheRoot string
	CASC      *casc.CASCRemote
	Local     *casc.CASCLocal
	LF        *listfile.Listfile
	DB2       *appruntime.MemoryDB2Store
	Keys      *tact.KeyRing
	Spell     *wowdata.SpellService
	Enc       *wowdata.EncounterService
	Item      *wowdata.ItemService
	Creat     *wowdata.CreatureService
	Decor     *wowdata.DecorService
	Diag      *diagnostics.DiagnosticsService
	Layout    storage.Layout
	Config    storage.Config
	Target    storage.Target
	DBDCommit string
}

func NewRuntime() *Runtime {
	layout, _ := storage.Resolve("")
	config, err := layout.LoadConfig()
	if err != nil {
		config = storage.Config{Schema: "wowdata.config.v1", CacheMaxBytes: storage.DefaultCacheMaxBytes, Workers: storage.DefaultWorkers}
	}
	return &Runtime{
		CacheRoot: layout.Cache,
		LF:        listfile.New(),
		DB2:       appruntime.NewMemoryDB2Store(),
		Spell:     wowdata.NewSpellService(),
		Enc:       wowdata.NewEncounterService(),
		Item:      wowdata.NewItemService(),
		Creat:     wowdata.NewCreatureService(),
		Decor:     wowdata.NewDecorService(),
		Diag:      diagnostics.NewDiagnosticsService(),
		Layout:    layout,
		Config:    config,
	}
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	rt := NewRuntime()
	cmd := newRootCommandForRuntime(rt)
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	if err := cmd.Execute(); err != nil {
		if !app.IsCommandError(err) {
			fmt.Fprintln(stderr, err)
		}
		return 1
	}
	return 0
}

func newRootCommandForRuntime(rt *Runtime) *cobra.Command {
	fileStore := appruntime.NewCASCFileStore(rt.LF, nil, rt)

	prepare := func(handler func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
		return prepareThen(rt, handler)
	}
	svc := &app.Service{
		Warmup:    warmupHandler(rt),
		Casc:      prepare(cascHandler(rt)),
		DB2:       prepare(app.NewDB2HandlerWithStore(rt.DB2)),
		Spell:     prepare(app.NewSpellHandler(wowdata.NewSpellServiceWithDB2(rt.DB2))),
		Encounter: prepare(app.NewEncounterHandler(wowdata.NewEncounterServiceWithDB2(rt.DB2))),
		File:      prepare(app.NewFileHandlerWithStore(fileStore)),
		Icon:      prepare(app.NewIconHandlerWithStore(fileStore)),
		Item:      prepare(app.NewItemHandler(wowdata.NewItemServiceWithDB2(rt.DB2))),
		Creature:  prepare(app.NewCreatureHandler(wowdata.NewCreatureServiceWithDB2(rt.DB2))),
		Decor:     prepare(app.NewDecorHandler(wowdata.NewDecorServiceWithDB2(rt.DB2))),
		Video:     app.NewVideoHandler(),
		Golden:    app.NewGoldenHandler(),
		Profile:   profileHandler(rt),
		Cache:     cacheHandler(rt),
		Doctor:    doctorHandler(rt),
		Update:    updateHandler(rt),
		Uninstall: uninstallHandler(rt),
	}

	cmd := app.NewRootCommandWithService(svc)
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
		return nil, fmt.Errorf("CASC 未就绪，请提供完整目标或检查准备错误")
	}
	return nil, fmt.Errorf("CASC 未就绪，请提供完整目标或检查准备错误")
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
	return nil, fmt.Errorf("CASC 未就绪，请提供完整目标或检查准备错误")
}

func warmupHandler(rt *Runtime) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := rt.Layout.Ensure(); err != nil {
			return app.NewResponseWriter(cmd.OutOrStdout()).Error("warmup", "home_init_failed", err.Error())
		}
		resolved, err := resolveTarget(cmd, rt.Layout)
		if err != nil {
			missing := []string{}
			if targetErr, ok := err.(targetRequiredError); ok {
				missing = targetErr.Missing
			}
			return app.NewResponseWriter(cmd.OutOrStdout()).Error("warmup", "target_required", targetErrorMessage(err, missing))
		}
		opts := warmupOptionsFromCommand(cmd)
		opts.Source = resolved.Target.Source
		opts.Path = resolved.Target.Path
		opts.Region = resolved.Target.Region
		opts.Product = resolved.Target.Product
		opts.Build = resolved.Target.Build
		opts.Locale = resolved.Target.Locale
		opts.Profile = resolved.ProfileName
		fmt.Fprintf(cmd.ErrOrStderr(), "prepare target=%s/%s/%s/%s build=%s\n", opts.Source, opts.Region, opts.Product, opts.Locale, opts.Build)
		lock, err := rt.Layout.AcquireLock(fmt.Sprintf("%s|%s|%s|%s|%s|%s", opts.Source, opts.Path, opts.Region, opts.Product, opts.Build, opts.Locale), 2*time.Minute)
		if err != nil {
			return app.NewResponseWriter(cmd.OutOrStdout()).Error("warmup", "cache_lock_failed", err.Error())
		}
		defer lock.Release()
		casc.SetDownloadProgressWriter(cmd.ErrOrStderr())
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
	Build           string
	Locale          string
	Profile         string
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
	build, _ := commandStringFlag(cmd, "build")
	locale, _ := commandStringFlag(cmd, "locale")
	profile, _ := commandStringFlag(cmd, "profile")
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
		Build:           build,
		Locale:          locale,
		Profile:         profile,
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
		"build":   opts.Build,
		"cache":   filepath.ToSlash(opts.CacheRoot),
		"warm": map[string]interface{}{
			"listfile":       opts.WarmListfile,
			"listfileFormat": opts.ListfileFormat,
			"dbdManifest":    opts.WarmDBDManifest,
			"tables":         opts.Tables,
		},
	}
	target := storage.Target{Source: opts.Source, Path: opts.Path, Region: opts.Region, Product: opts.Product, Build: opts.Build, Locale: opts.Locale}
	if err := target.Validate(); err != nil {
		return result, warmupStepError{Code: "target_required", Err: err}
	}
	var selectedBuild casc.VersionEntry

	switch opts.Source {
	case "remote":
		remote := casc.NewCASCRemote(opts.Region)
		remote.CacheRoot = opts.CacheRoot
		remote.Workers = rt.Config.Workers
		if err := applyWarmupLocale(remote.CASCSource, opts.Locale); err != nil {
			return result, warmupStepError{Code: "invalid_locale", Err: err}
		}
		if err := remote.Init(); err != nil {
			return result, warmupStepError{Code: "init_failed", Err: err}
		}
		buildIdx := buildIndexBySelection(remote.Builds, opts.Product, opts.Build)
		if buildIdx < 0 {
			result["status"] = "no_build"
			result["builds"] = convertProducts(remote.GetProductList())
			return result, nil
		}
		if err := remote.Preload(buildIdx); err != nil {
			return result, warmupStepError{Code: "preload_failed", Err: err}
		}
		selectedBuild = *remote.Build

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
		buildIdx := buildIndexBySelection(local.Builds, opts.Product, opts.Build)
		if buildIdx < 0 {
			result["status"] = "no_build"
			result["builds"] = convertProducts(local.GetProductList())
			return result, nil
		}
		if err := local.Load(buildIdx); err != nil {
			return result, warmupStepError{Code: "preload_failed", Err: err}
		}
		selectedBuild = *local.Build

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
	rt.Target = target
	buildRef, err := rt.persistResolvedTarget(opts.Profile, target, selectedBuild)
	if err != nil {
		return result, warmupStepError{Code: "state_write_failed", Err: err}
	}
	result["buildRef"] = buildRef
	result["resolvedBuild"] = resolvedBuildVersion(selectedBuild)
	buildKey := selectedBuild.BuildConfig
	if buildKey == "" {
		buildKey = selectedBuild.BuildKey
	}
	if prune, pruneErr := rt.Layout.PruneCacheKeeping(rt.Config.CacheMaxBytes, []string{buildKey}); pruneErr == nil && len(prune.Removed) > 0 {
		result["cachePrune"] = prune
	}
	return result, nil
}

func (rt *Runtime) persistResolvedTarget(profileName string, target storage.Target, build casc.VersionEntry) (string, error) {
	buildConfigKey := build.BuildConfig
	if buildConfigKey == "" {
		buildConfigKey = build.BuildKey
	}
	buildRef, err := rt.Layout.SaveBuild(storage.BuildSnapshot{
		Source: target.Source, Path: target.Path, Region: target.Region, Product: target.Product,
		Locale: target.Locale, Version: resolvedBuildVersion(build), BuildID: resolvedBuildID(build),
		BuildConfigKey: buildConfigKey, CDNConfigKey: build.CDNConfig,
	})
	if err != nil {
		return "", err
	}
	if profileName == "" {
		return buildRef, nil
	}
	profile, err := rt.Layout.LoadProfile(profileName)
	if err != nil {
		return "", err
	}
	profile.ResolvedBuild = resolvedBuildVersion(build)
	profile.BuildConfigKey = buildConfigKey
	profile.CDNConfigKey = build.CDNConfig
	profile.RecentBuilds = prependBuildRef(profile.RecentBuilds, buildRef)
	if err := rt.Layout.SaveProfile(profile); err != nil {
		return "", err
	}
	return buildRef, nil
}

func prependBuildRef(existing []string, value string) []string {
	out := []string{value}
	for _, item := range existing {
		if item != value {
			out = append(out, item)
		}
		if len(out) == 2 {
			break
		}
	}
	return out
}

func resolvedBuildVersion(build casc.VersionEntry) string {
	if build.VersionsName != "" {
		return build.VersionsName
	}
	return build.Version
}

func resolvedBuildID(build casc.VersionEntry) string {
	version := resolvedBuildVersion(build)
	if dot := strings.LastIndex(version, "."); dot >= 0 && dot+1 < len(version) {
		return version[dot+1:]
	}
	return ""
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
	_ = executable
	if strings.TrimSpace(cacheFlag) != "" {
		return cacheFlag
	}
	layout, err := storage.Resolve("")
	if err != nil {
		return "cache"
	}
	return layout.Cache
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
	dbdSource := appruntime.NewHTTPDBDSource(filepath.Join(rt.dbdCacheDir(), rt.DBDCommit), []string{
		"https://raw.githubusercontent.com/wowdev/WoWDBDefs/" + rt.DBDCommit + "/definitions/%s.dbd",
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
	}).WithWorkers(rt.Config.Workers)
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
	cascSource := rt.CASC
	localSource := rt.Local
	rt.mu.Unlock()
	if cascSource != nil {
		lf.FilterIDs(rootEntryMap(cascSource.CASCSource))
	} else if localSource != nil {
		lf.FilterIDs(rootEntryMap(localSource.CASCSource))
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
	if rt.DBDCommit == "" {
		revisionSource := appruntime.NewHTTPDBDRevisionSource(rt.dbdCacheDir(), "https://api.github.com/repos/wowdev/WoWDBDefs/commits/master")
		commit, err := revisionSource.Revision()
		if err != nil {
			return nil, err
		}
		rt.DBDCommit = commit
	}
	cacheDir := filepath.Join(rt.dbdCacheDir(), rt.DBDCommit)
	manifestSource := appruntime.NewHTTPDBDManifestSource(cacheDir, []string{
		"https://raw.githubusercontent.com/wowdev/WoWDBDefs/" + rt.DBDCommit + "/manifest.json",
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
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("casc info", "not_ready", "CASC 未就绪，请提供完整目标或检查准备错误")
			}
			info := rt.Diag.GetInfo()
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("casc info", info)

		case "products":
			source, _ := cmd.Flags().GetString("source")
			path, _ := cmd.Flags().GetString("path")
			region, _ := cmd.Flags().GetString("region")

			if source == "" {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("casc products", "target_required", "--source is required")
			}
			if source != "local" && source != "remote" {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("casc products", "invalid_source", "--source must be local or remote")
			}
			if source == "remote" && region == "" {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("casc products", "target_required", "--region is required for remote source")
			}

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
				remote.CacheRoot = rt.CacheRoot
				remote.Workers = rt.Config.Workers
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
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("casc diagnose", "not_ready", "CASC 未就绪，请提供完整目标或检查准备错误")
			}
			d := rt.Diag.Diagnose()
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("casc diagnose", d)

		default:
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("casc", map[string]interface{}{
				"subcommands": []string{"info", "products", "diagnose"},
				"note":        "data commands prepare their required CASC context automatically",
			})
		}
	}
}

func convertProducts(src []casc.Product) []diagnostics.Product {
	out := make([]diagnostics.Product, len(src))
	for i, p := range src {
		out[i] = diagnostics.Product{
			Label: p.Label, BuildIndex: p.BuildIndex, Product: p.Product, Region: p.Region,
			Version: p.Version, BuildID: p.BuildID, BuildConfigKey: p.BuildConfigKey,
			CDNConfigKey: p.CDNConfigKey, Branch: p.Branch, Locales: p.Locales,
		}
	}
	return out
}

var _ = (*cobra.Command)(nil)
