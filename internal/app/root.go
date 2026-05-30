package app

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

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
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	registerCommands(cmd, svc)
	return cmd
}

func writeJSON(w io.Writer, resp Response) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(resp)
}

func notImplementedHandler(commandName string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		resp := NewErrorResponse(commandName, "not_implemented", fmt.Sprintf("%s is planned but not implemented in Phase 0", commandName))
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}
