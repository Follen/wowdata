package app

import (
	"wowdata/internal/wowdata"

	"github.com/spf13/cobra"
)

func NewDecorHandler(svc *wowdata.DecorService) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		parent := cmd.Parent()
		use := ""
		if parent != nil {
			use = cmd.Name()
		}

		if use == "list" || cmd.Name() == "list" {
			if !svc.RuntimeReady() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("decor list", "not_ready", warmupRequiredMessage))
			}
			if !svc.Ready() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("decor list", "not_ready", "HouseDecor table is not loaded; run warmup with --tables HouseDecor"))
			}
			limit, _ := cmd.Flags().GetInt("limit")
			all := svc.ListAll()
			if limit > 0 && limit < len(all) {
				all = all[:limit]
			}
			resp := NewSuccessResponse("decor list", map[string]interface{}{
				"items": all,
				"count": len(all),
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "get" || cmd.Name() == "get" {
			if !svc.RuntimeReady() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("decor get", "not_ready", warmupRequiredMessage))
			}
			if !svc.Ready() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("decor get", "not_ready", "HouseDecor table is not loaded; run warmup with --tables HouseDecor"))
			}
			id, _ := cmd.Flags().GetUint32("id")
			modelFDID, _ := cmd.Flags().GetUint32("model-file-data-id")

			var item *wowdata.DecorItem
			if id > 0 {
				item = svc.GetByID(id)
			} else if modelFDID > 0 {
				item = svc.GetByModelFileDataID(modelFDID)
			}

			if item == nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("decor get", "not_found", "decor not found"))
			}
			resp := NewSuccessResponse("decor get", item)
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		resp := NewSuccessResponse("decor", map[string]interface{}{
			"help": "decor subcommands: list, get",
		})
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}
