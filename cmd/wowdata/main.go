package main

import (
	"fmt"
	"os"

	"wowdata/internal/app"
	"wowdata/internal/casc"
	"wowdata/internal/diagnostics"
	"wowdata/internal/listfile"
	"wowdata/internal/wowdata"

	"github.com/spf13/cobra"
)

func main() {
	lf := listfile.New()
	fs := casc.NewFileService()

	spellSvc := wowdata.NewSpellService()
	encounterSvc := wowdata.NewEncounterService()
	itemSvc := wowdata.NewItemService()
	creatureSvc := wowdata.NewCreatureService()
	decorSvc := wowdata.NewDecorService()

	svc := &app.Service{
		Warmup:    app.PlaceholderHandler("warmup"),
		Casc:      app.PlaceholderHandler("casc"),
		DB2:       app.NewDB2Handler(),
		Spell:     app.NewSpellHandler(spellSvc),
		Encounter: app.NewEncounterHandler(encounterSvc),
		File:      app.NewFileHandler(lf, fs),
		Icon:      app.NewIconHandler(),
		Item:      app.NewItemHandler(itemSvc),
		Creature:  app.NewCreatureHandler(creatureSvc),
		Decor:     app.NewDecorHandler(decorSvc),
		Video:     app.NewVideoHandler(),
		Golden:    app.NewGoldenHandler(),
	}

	_ = diagnostics.NewDiagnosticsService()

	cmd := app.NewRootCommandWithService(svc)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Ensure cobra import used
var _ = (*cobra.Command)(nil)
