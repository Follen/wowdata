package app

import (
	"wowdata/internal/wowdata"

	"github.com/spf13/cobra"
)

func NewItemHandler(svc *wowdata.ItemService) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		parent := cmd.Parent()
		use := ""
		if parent != nil {
			use = cmd.Use
		}

		if use == "get" || cmd.Use == "get" {
			itemID, _ := cmd.Flags().GetUint32("item-id")
			item := svc.GetItem(itemID)
			if item == nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("item get", "not_found", "item not found"))
			}
			resp := NewSuccessResponse("item get", item)
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "models" || cmd.Use == "models" {
			itemID, _ := cmd.Flags().GetUint32("item-id")
			raceID, _ := cmd.Flags().GetInt("race-id")
			gender, _ := cmd.Flags().GetInt("gender")
			result := svc.GetItemModels(int(itemID), raceID, gender)
			resp := NewSuccessResponse("item models", result)
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "geosets" || cmd.Use == "geosets" {
			itemID, _ := cmd.Flags().GetUint32("item-id")
			result := svc.GetItemGeosets(itemID)
			resp := NewSuccessResponse("item geosets", map[string]interface{}{
				"itemID": itemID,
				"geosets": result,
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "textures" || cmd.Use == "textures" {
			itemID, _ := cmd.Flags().GetUint32("item-id")
			result := svc.GetItemTextures(itemID)
			resp := NewSuccessResponse("item textures", map[string]interface{}{
				"itemID":   itemID,
				"textures": result,
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		resp := NewSuccessResponse("item", map[string]interface{}{
			"help": "item subcommands: get, models, geosets, textures",
		})
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}
