package app

import (
	"wowdata/internal/wowdata"

	"github.com/spf13/cobra"
)

func NewSpellHandler(svc *wowdata.SpellService) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		parent := cmd.Parent()
		use := ""
		if parent != nil {
			use = cmd.Use
		}

		if use == "info" || cmd.Use == "info" {
			spellID, _ := cmd.Flags().GetUint32("spell-id")
			maxDepth, _ := cmd.Flags().GetInt("max-depth")
			if maxDepth <= 0 {
				maxDepth = 5
			}
			info := svc.GetSpellInfo(spellID, maxDepth)
			resp := NewSuccessResponse("spell info", info)
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "auras" || cmd.Use == "auras" {
			spellID, _ := cmd.Flags().GetUint32("spell-id")
			result := svc.DetectAuras(spellID)
			resp := NewSuccessResponse("spell auras", result)
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "summons" || cmd.Use == "summons" {
			spellID, _ := cmd.Flags().GetUint32("spell-id")
			npcID, _ := cmd.Flags().GetUint32("npc-id")
			summons := svc.DetectSummons(spellID, npcID)
			resp := NewSuccessResponse("spell summons", map[string]interface{}{
				"spellID": spellID,
				"npcID":   npcID,
				"summons": summons,
				"count":   len(summons),
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		resp := NewSuccessResponse("spell", map[string]interface{}{
			"help": "spell subcommands: info, auras, summons",
		})
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}
