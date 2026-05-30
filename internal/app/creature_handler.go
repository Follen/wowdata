package app

import (
	"wowdata/internal/wowdata"

	"github.com/spf13/cobra"
)

func NewCreatureHandler(svc *wowdata.CreatureService) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		parent := cmd.Parent()
		use := ""
		if parent != nil {
			use = cmd.Use
		}

		if use == "display" || cmd.Use == "display" {
			displayID, _ := cmd.Flags().GetUint32("display-id")
			result := svc.GetDisplayByID(displayID)
			if result == nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("creature display", "not_found", "display not found"))
			}
			resp := NewSuccessResponse("creature display", result)
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "model" || cmd.Use == "model" {
			fdid, _ := cmd.Flags().GetUint32("file-data-id")
			displays := svc.GetCreatureDisplaysByFileDataID(fdid)
			resp := NewSuccessResponse("creature model", map[string]interface{}{
				"fileDataID": fdid,
				"displays":   displays,
				"count":      len(displays),
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		resp := NewSuccessResponse("creature", map[string]interface{}{
			"help": "creature subcommands: display, model",
		})
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}
