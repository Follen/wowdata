package app

import "github.com/spf13/cobra"

const Version = "0.0.1"

func NewRootCommand() *cobra.Command {
	return NewRootCommandWithService(nil)
}

func NewRootCommandWithService(svc *Service) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wowdata",
		Short: "Query and export World of Warcraft data from local clients or remote CDN builds.",
		Long: "wowdata is a Go CLI for querying World of Warcraft CASC, DB2, listfile, texture, item, creature, decor, spell, and encounter data.\n\n" +
			"Run warmup before commands that require an active build context. First use may take time while manifests, indexes, listfiles, and DB definitions are cached.",
		Example: "  wowdata warmup --source remote --region cn --product wow\n" +
			"  wowdata db2 rows SpellName --id 123\n" +
			"  wowdata file lookup --file-data-id 456\n" +
			"  wowdata icon export --file-data-id 789 --format png",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().Bool("auto-warmup", false, "Initialize the requested data context before running commands that need warmup")
	cmd.PersistentFlags().Bool("mcp", false, "Serve MCP tools over stdio")
	cmd.PersistentFlags().String("source", "remote", "Data source for --auto-warmup: local or remote")
	cmd.PersistentFlags().String("path", "", "Local WoW client path for --auto-warmup when source=local")
	cmd.PersistentFlags().String("region", "cn", "WoW region for --auto-warmup")
	cmd.PersistentFlags().String("product", "wow", "WoW product for --auto-warmup")
	cmd.PersistentFlags().String("locale", "zhCN", "WoW locale for --auto-warmup, such as zhCN or enUS")
	cmd.PersistentFlags().String("cache", "", "Cache directory for --auto-warmup; defaults to cache next to the executable")
	cmd.PersistentFlags().String("tables", "", "Comma-separated DB2 tables to load during --auto-warmup")
	cmd.PersistentFlags().Bool("listfile", false, "Load listfile during --auto-warmup")
	cmd.PersistentFlags().String("listfile-format", "binary", "Listfile source format for --auto-warmup: binary or text")
	cmd.PersistentFlags().Bool("dbd-manifest", false, "Load DBD manifest during --auto-warmup")

	registerCommands(cmd, svc)
	return cmd
}

func unavailableHandler(commandName string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		resp := NewErrorResponse(commandName, "service_unavailable", "no runtime service was attached for this command")
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}
