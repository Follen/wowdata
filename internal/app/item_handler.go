package app

import (
	"wowdata/internal/shared/wowdata"

	"github.com/spf13/cobra"
)

func NewItemHandler(svc *wowdata.ItemService) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		parent := cmd.Parent()
		use := ""
		if parent != nil {
			use = cmd.Name()
		}

		if use == "get" || cmd.Name() == "get" {
			if !svc.RuntimeReady() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("item get", "not_ready", warmupRequiredMessage))
			}
			if !svc.Ready() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("item get", "not_ready", "Item and ItemSparse tables are not loaded; run warmup with item tables"))
			}
			itemID, _ := cmd.Flags().GetUint32("item-id")
			item := svc.GetItem(itemID)
			if item == nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("item get", "not_found", "item not found"))
			}
			resp := NewSuccessResponse("item get", item)
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "models" || cmd.Name() == "models" {
			if !svc.RuntimeReady() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("item models", "not_ready", warmupRequiredMessage))
			}
			if !svc.Ready() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("item models", "not_ready", "Item tables are not loaded; run warmup with item model tables"))
			}
			itemID, _ := cmd.Flags().GetUint32("item-id")
			raceID, _ := cmd.Flags().GetInt("race-id")
			gender, _ := cmd.Flags().GetInt("gender")
			result := svc.GetItemModels(int(itemID), raceID, gender)
			resp := NewSuccessResponse("item models", itemModelsResponse(result))
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "geosets" || cmd.Name() == "geosets" {
			if !svc.RuntimeReady() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("item geosets", "not_ready", warmupRequiredMessage))
			}
			if !svc.Ready() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("item geosets", "not_ready", "Item tables are not loaded; run warmup with item geoset tables"))
			}
			itemID, _ := cmd.Flags().GetUint32("item-id")
			result := svc.GetItemGeosets(itemID)
			helmetHide := []int{}
			if result != nil {
				helmetHide = append(helmetHide, result.HelmetHide...)
			}
			resp := NewSuccessResponse("item geosets", map[string]interface{}{
				"itemID":     itemID,
				"geosets":    itemGeosetsResponse(result),
				"helmetHide": helmetHide,
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "textures" || cmd.Name() == "textures" {
			if !svc.RuntimeReady() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("item textures", "not_ready", warmupRequiredMessage))
			}
			if !svc.Ready() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("item textures", "not_ready", "Item tables are not loaded; run warmup with item texture tables"))
			}
			itemID, _ := cmd.Flags().GetUint32("item-id")
			result := svc.GetItemTextures(itemID)
			resp := NewSuccessResponse("item textures", map[string]interface{}{
				"itemID":   itemID,
				"textures": itemTexturesResponse(result),
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		resp := NewSuccessResponse("item", map[string]interface{}{
			"help": "item subcommands: get, models, geosets, textures",
		})
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}

func itemTexturesResponse(result *wowdata.ItemTextureResult) []map[string]interface{} {
	if result == nil {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(result.Sections))
	for _, section := range result.Sections {
		out = append(out, map[string]interface{}{
			"section":    section.Section,
			"fileDataID": section.FileDataID,
		})
	}
	return out
}

func itemModelsResponse(result *wowdata.ItemModelResult) map[string]interface{} {
	if result == nil {
		return map[string]interface{}{}
	}
	display := map[string]interface{}{
		"ID":                    result.DisplayID,
		"models":                result.Models,
		"textures":              result.Textures,
		"geosetGroup":           result.GeosetGroup,
		"attachmentGeosetGroup": []int{0, 0, 0, 0, 0, 0},
	}
	return map[string]interface{}{
		"itemID":  result.ItemID,
		"raceID":  result.RaceID,
		"gender":  result.Gender,
		"display": display,
	}
}

func itemGeosetsResponse(result *wowdata.ItemGeosetResult) map[string]interface{} {
	if result == nil {
		return map[string]interface{}{
			"geosetGroup":     []int{},
			"helmetGeosetVis": []int{},
		}
	}
	return map[string]interface{}{
		"geosetGroup":     result.GeosetGroup,
		"helmetGeosetVis": result.HelmetGeosetVis,
	}
}
