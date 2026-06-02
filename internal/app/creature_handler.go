package app

import (
	"wowdata/internal/shared/wowdata"

	"github.com/spf13/cobra"
)

func NewCreatureHandler(svc *wowdata.CreatureService) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		parent := cmd.Parent()
		use := ""
		if parent != nil {
			use = cmd.Name()
		}

		if use == "display" || cmd.Name() == "display" {
			if !svc.RuntimeReady() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("creature display", "not_ready", warmupRequiredMessage))
			}
			if !svc.Ready() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("creature display", "not_ready", "CreatureDisplayInfo and CreatureModelData tables are not loaded; run warmup with creature tables"))
			}
			displayID, _ := cmd.Flags().GetUint32("display-id")
			fdid, _ := cmd.Flags().GetUint32("file-data-id")
			var result *wowdata.CreatureDisplayInfo
			if displayID > 0 {
				result = svc.GetDisplayByID(displayID)
			} else if fdid > 0 {
				result = svc.GetDisplayByFileDataID(fdid)
			}
			if result == nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("creature display", "not_found", "display not found"))
			}
			resp := NewSuccessResponse("creature display", result)
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "model" || cmd.Name() == "model" {
			if !svc.RuntimeReady() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("creature model", "not_ready", warmupRequiredMessage))
			}
			if !svc.Ready() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("creature model", "not_ready", "CreatureDisplayInfo and CreatureModelData tables are not loaded; run warmup with creature tables"))
			}
			fdid, _ := cmd.Flags().GetUint32("file-data-id")
			displays := svc.GetCreatureDisplaysByFileDataID(fdid)
			resp := NewSuccessResponse("creature model", map[string]interface{}{
				"fileDataID": fdid,
				"displays":   creatureModelDisplaysResponse(displays),
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		resp := NewSuccessResponse("creature", map[string]interface{}{
			"help": "creature subcommands: display, model",
		})
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}

func creatureModelDisplaysResponse(displays []wowdata.CreatureDisplayInfo) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(displays))
	for _, display := range displays {
		textures := make([]uint32, 0, len(display.Textures))
		textures = append(textures, display.Textures...)
		out = append(out, map[string]interface{}{
			"ID":       display.DisplayID,
			"modelID":  display.ModelID,
			"textures": textures,
		})
	}
	return out
}
