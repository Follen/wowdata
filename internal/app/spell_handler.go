package app

import (
	"wowdata/internal/shared/wowdata"

	"github.com/spf13/cobra"
)

func NewSpellHandler(svc *wowdata.SpellService) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		parent := cmd.Parent()
		use := ""
		if parent != nil {
			use = cmd.Name()
		}

		if use == "info" || cmd.Name() == "info" {
			if !svc.RuntimeReady() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("spell info", "not_ready", warmupRequiredMessage))
			}
			if !svc.Ready() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("spell info", "not_ready", "SpellEffect table is not loaded; run warmup with --tables SpellEffect"))
			}
			spellID, _ := cmd.Flags().GetUint32("spell-id")
			maxDepth, _ := cmd.Flags().GetInt("max-depth")
			if maxDepth <= 0 {
				maxDepth = 5
			}
			info := svc.GetSpellInfo(spellID, maxDepth)
			resp := NewSuccessResponse("spell info", info)
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "auras" || cmd.Name() == "auras" {
			if !svc.RuntimeReady() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("spell auras", "not_ready", warmupRequiredMessage))
			}
			if !svc.Ready() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("spell auras", "not_ready", "SpellEffect table is not loaded; run warmup with --tables SpellEffect"))
			}
			spellID, _ := cmd.Flags().GetUint32("spell-id")
			result := svc.DetectAuras(spellID)
			data := map[string]interface{}{
				"hasAura": []uint32{},
				"noAura":  []uint32{},
			}
			if result.HasAura {
				data["hasAura"] = []uint32{spellID}
			} else {
				data["noAura"] = []uint32{spellID}
			}
			resp := NewSuccessResponse("spell auras", data)
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "summons" || cmd.Name() == "summons" {
			if !svc.RuntimeReady() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("spell summons", "not_ready", warmupRequiredMessage))
			}
			if !svc.Ready() {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("spell summons", "not_ready", "SpellEffect table is not loaded; run warmup with --tables SpellEffect"))
			}
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
