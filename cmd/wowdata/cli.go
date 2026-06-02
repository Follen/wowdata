package main

import (
	"os"
	"strings"

	"wowdata/internal/app"
	appruntime "wowdata/internal/local/runtime"
	"wowdata/internal/wowdata"

	"github.com/spf13/cobra"
)

func newCLIServiceForRuntime(rt *Runtime) *app.Service {
	fileStore := appruntime.NewCASCFileStore(rt.LF, nil, rt)
	return &app.Service{
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
}

func attachCLIPersistentPreRun(cmd *cobra.Command, rt *Runtime) {
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		syncRuntimeFromPersistentFlags(cmd, rt)
		autoWarmup, _ := commandBoolFlag(cmd, "auto-warmup")
		if !autoWarmup || cmd.CommandPath() == "wowdata warmup" || strings.HasPrefix(cmd.CommandPath(), "wowdata mcp") {
			return nil
		}
		opts := warmupOptionsFromCommand(cmd)
		_, err := rt.initialize(opts)
		return err
	}
}

func syncRuntimeFromPersistentFlags(cmd *cobra.Command, rt *Runtime) {
	cacheRoot, _ := commandStringFlag(cmd, "cache")
	if cacheRoot != "" {
		rt.CacheRoot = resolveCacheRoot(cacheRoot, os.Executable)
		if rt.localRuntime != nil {
			rt.localRuntime.SetCacheRoot(rt.CacheRoot)
		}
	}
}
