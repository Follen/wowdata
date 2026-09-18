package app

import "github.com/spf13/cobra"

var Version = "0.0.0-development"

func NewRootCommand() *cobra.Command {
	return NewRootCommandWithService(nil)
}

func NewRootCommandWithService(svc *Service) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wowdata",
		Short: "Query and export World of Warcraft data from local clients or remote CDN builds.",
		Long: "wowdata is a Go CLI for querying World of Warcraft CASC, DB2, listfile, texture, item, creature, decor, spell, and encounter data.\n\n" +
			"Data commands prepare their required cache automatically. The optional warmup command prepares data ahead of time.",
		Example: "  wowdata casc products --source remote --region cn\n" +
			"  wowdata warmup --source remote --region cn --product wow --build latest --locale zhCN\n" +
			"  wowdata sql \"SELECT ID, Name_lang FROM SpellName WHERE ID = 123\" --source local --region us --product wow --build latest --locale enUS\n" +
			"  wowdata db2 rows SpellName --id 123 --source remote --region cn --product wow --build latest --locale zhCN\n" +
			"  wowdata file lookup --file-data-id 456 --profile retail-cn\n" +
			"  wowdata icon export --file-data-id 789 --format png --profile retail-cn",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().Bool("auto-warmup", false, "Deprecated compatibility flag; data commands prepare required cache automatically")
	cmd.PersistentFlags().String("source", "", "Data source: local or remote (required unless --profile is used)")
	cmd.PersistentFlags().String("path", "", "Local WoW client path when source=local")
	cmd.PersistentFlags().String("region", "", "WoW region (required unless --profile is used)")
	cmd.PersistentFlags().String("product", "", "WoW product (required unless --profile is used)")
	cmd.PersistentFlags().String("build", "", "WoW build: latest, version, build ID, or config key (required unless --profile is used)")
	cmd.PersistentFlags().String("locale", "", "WoW locale, such as zhCN or enUS (required unless --profile is used)")
	cmd.PersistentFlags().String("profile", "", "Named target profile from ~/.wowdata/profiles")
	cmd.PersistentFlags().String("cache", "", "Cache directory override; defaults to ~/.wowdata/cache")
	cmd.PersistentFlags().String("tables", "", "Additional comma-separated DB2 tables to prepare")
	cmd.PersistentFlags().Bool("listfile", false, "Also prepare the listfile cache")
	cmd.PersistentFlags().String("listfile-format", "binary", "Listfile source format: binary or text")
	cmd.PersistentFlags().Bool("dbd-manifest", false, "Also prepare the DBD manifest cache")

	registerCommands(cmd, svc)
	return cmd
}

func unavailableHandler(commandName string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		resp := NewErrorResponse(commandName, "service_unavailable", "no runtime service was attached for this command")
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}
