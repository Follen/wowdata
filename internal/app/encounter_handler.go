package app

import (
	"wowdata/internal/wowdata"

	"github.com/spf13/cobra"
)

func NewEncounterHandler(svc *wowdata.EncounterService) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if !svc.RuntimeReady() {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("encounter get", "not_ready", warmupRequiredMessage))
		}
		if !svc.Ready() {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("encounter get", "not_ready", "JournalEncounterSection table is not loaded; run warmup with --tables JournalEncounterSection"))
		}
		encounterID, _ := cmd.Flags().GetUint32("journal-encounter-id")
		result := svc.GetEncounter(encounterID)
		resp := NewSuccessResponse("encounter get", result)
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}
