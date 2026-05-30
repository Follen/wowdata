package main

import (
	"fmt"
	"os"
	"sync"

	"wowdata/internal/app"
	"wowdata/internal/casc"
	"wowdata/internal/diagnostics"
	"wowdata/internal/listfile"
	"wowdata/internal/wowdata"

	"github.com/spf13/cobra"
)

// Runtime holds the live state that handlers share.
type Runtime struct {
	mu     sync.Mutex
	CASC   *casc.CASCRemote
	LF     *listfile.Listfile
	FS     *casc.FileService
	Spell  *wowdata.SpellService
	Enc    *wowdata.EncounterService
	Item   *wowdata.ItemService
	Creat  *wowdata.CreatureService
	Decor  *wowdata.DecorService
	Diag   *diagnostics.DiagnosticsService
}

func NewRuntime() *Runtime {
	return &Runtime{
		LF:    listfile.New(),
		FS:    casc.NewFileService(),
		Spell: wowdata.NewSpellService(),
		Enc:   wowdata.NewEncounterService(),
		Item:  wowdata.NewItemService(),
		Creat: wowdata.NewCreatureService(),
		Decor: wowdata.NewDecorService(),
		Diag:  diagnostics.NewDiagnosticsService(),
	}
}

func main() {
	rt := NewRuntime()

	svc := &app.Service{
		Warmup:    warmupHandler(rt),
		Casc:      cascHandler(rt),
		DB2:       app.NewDB2Handler(),
		Spell:     app.NewSpellHandler(rt.Spell),
		Encounter: app.NewEncounterHandler(rt.Enc),
		File:      app.NewFileHandler(rt.LF, rt.FS),
		Icon:      app.NewIconHandler(),
		Item:      app.NewItemHandler(rt.Item),
		Creature:  app.NewCreatureHandler(rt.Creat),
		Decor:     app.NewDecorHandler(rt.Decor),
		Video:     app.NewVideoHandler(),
		Golden:    app.NewGoldenHandler(),
	}

	cmd := app.NewRootCommandWithService(svc)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func warmupHandler(rt *Runtime) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		source, _ := cmd.Flags().GetString("source")
		region, _ := cmd.Flags().GetString("region")
		product, _ := cmd.Flags().GetString("product")

		rt.mu.Lock()
		defer rt.mu.Unlock()

		result := map[string]interface{}{
			"source":  source,
			"region":  region,
			"product": product,
		}

		if source == "remote" {
			remote := casc.NewCASCRemote(region)
			if err := remote.Init(); err != nil {
				result["status"] = "init_failed"
				result["error"] = err.Error()
				w := cmd.OutOrStdout()
				return app.NewResponseWriter(w).Error("warmup", "init_failed", err.Error())
			}

			// Find matching build
			var buildIdx int = -1
			for i, b := range remote.Builds {
				if b.Product == product {
					buildIdx = i
					break
				}
			}
			if buildIdx < 0 {
				result["status"] = "no_build"
				result["builds"] = convertProducts(remote.GetProductList())
				return app.NewResponseWriter(cmd.OutOrStdout()).Success("warmup", result)
			}

			if err := remote.Preload(buildIdx); err != nil {
				result["status"] = "preload_failed"
				result["error"] = err.Error()
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("warmup", "preload_failed", err.Error())
			}

			rt.CASC = remote

			// Populate file service from root entries
			for _, fdid := range remote.GetValidRootEntries() {
				contentKeys := remote.RootEntries[fdid]
				for _, ck := range contentKeys {
					rt.FS.AddRootEntry(fdid, ck)
					encKey, err := remote.GetEncodingKeyForContentKey(ck)
					if err == nil {
						sz := remote.EncodingSizes[ck]
						rt.FS.AddEncodingEntry(ck, encKey, sz)
					}
				}
			}

			// Populate diagnostics
			rt.Diag.SetInfo(diagnostics.CASCInfo{
				Source:    source,
				Region:    region,
				Product:   product,
				BuildName: remote.GetBuildName(),
				BuildKey:  remote.GetBuildKey(),
				CachePath: remote.Cache.Root(),
			})
			rt.Diag.SetProducts(diagnostics.CASCProducts{
				Source:   source,
				Products: convertProducts(remote.GetProductList()),
			})

			result["status"] = "ok"
			result["buildName"] = remote.GetBuildName()
			result["buildKey"] = remote.GetBuildKey()
			result["rootEntryCount"] = len(remote.RootEntries)
			result["encodingEntryCount"] = len(remote.EncodingKeys)
			result["archiveCount"] = len(remote.Archives)
		} else {
			result["status"] = "local_source_not_implemented"
		}

		return app.NewResponseWriter(cmd.OutOrStdout()).Success("warmup", result)
	}
}

func cascHandler(rt *Runtime) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		use := cmd.Name()
		switch use {
		case "info":
			info := rt.Diag.GetInfo()
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("casc info", info)

		case "products":
			source, _ := cmd.Flags().GetString("source")
			region, _ := cmd.Flags().GetString("region")

			var products []diagnostics.Product
			if rt.CASC != nil {
				products = convertProducts(rt.CASC.GetProductList())
			} else {
				remote := casc.NewCASCRemote(region)
				if err := remote.Init(); err == nil {
					products = convertProducts(remote.GetProductList())
				}
			}

			return app.NewResponseWriter(cmd.OutOrStdout()).Success("casc products", diagnostics.CASCProducts{
				Source:   source,
				Products: products,
			})

		case "diagnose":
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
