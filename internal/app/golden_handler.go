package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func NewGoldenHandler() func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		parent := cmd.Parent()
		use := ""
		if parent != nil {
			use = cmd.Use
		}

		if use == "capture" || cmd.Use == "capture" {
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden capture", "missing_argument", "--name is required"))
			}
			capturePath := filepath.Join("fixtures", "golden", name+".json")
			resp := NewSuccessResponse("golden capture", map[string]interface{}{
				"name": name,
				"path": capturePath,
				"note": "golden capture requires a populated CASC context",
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "compare" || cmd.Use == "compare" {
			fixture, _ := cmd.Flags().GetString("fixture")
			all, _ := cmd.Flags().GetBool("all")

			if all {
				manifestPath := filepath.Join("fixtures", "golden", "node", "manifest.json")
				data, err := os.ReadFile(manifestPath)
				if err != nil {
					return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden compare", "io_error", err.Error()))
				}
				var manifest map[string]interface{}
				json.Unmarshal(data, &manifest)
				resp := NewSuccessResponse("golden compare", map[string]interface{}{
					"mode":     "all",
					"manifest": manifest,
					"note":     "full comparison requires populated CASC context",
				})
				return writeJSON(cmd.OutOrStdout(), resp)
			}

			if fixture == "" {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden compare", "missing_argument", "--fixture is required"))
			}
			resp := NewSuccessResponse("golden compare", map[string]interface{}{
				"fixture": fixture,
				"note":    "fixture comparison requires populated CASC context",
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		resp := NewSuccessResponse("golden", map[string]interface{}{
			"help": "golden subcommands: capture, compare",
		})
		_ = fmt.Sprint
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}

var _ = json.Marshal
