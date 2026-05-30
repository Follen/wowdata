package app

import (
	"os"

	"wowdata/internal/casc"
	"wowdata/internal/listfile"

	"github.com/spf13/cobra"
)

func NewFileHandler(lf *listfile.Listfile, fs *casc.FileService) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		parent := cmd.Parent()
		use := ""
		if parent != nil {
			use = cmd.Name()
		}

		if use == "lookup" || cmd.Name() == "lookup" {
			id, _ := cmd.Flags().GetUint32("file-data-id")
			name := lf.GetByID(id)
			resp := NewSuccessResponse("file lookup", map[string]interface{}{
				"fileDataID": id,
				"filename":   name,
				"found":      name != "",
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "search" || cmd.Name() == "search" {
			query, _ := cmd.Flags().GetString("query")
			results := lf.GetFilteredEntries(query)
			resp := NewSuccessResponse("file search", map[string]interface{}{
				"query":   query,
				"results": results,
				"count":   len(results),
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "extension" || cmd.Name() == "extension" {
			ext, _ := cmd.Flags().GetString("extension")
			results := lf.GetFilenamesByExtension(ext)
			resp := NewSuccessResponse("file extension", map[string]interface{}{
				"extension": ext,
				"results":   results,
				"count":     len(results),
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "exists" || cmd.Name() == "exists" {
			id, _ := cmd.Flags().GetUint32("file-data-id")
			filename, _ := cmd.Flags().GetString("filename")
			exists := false
			if id > 0 {
				exists = fs.Exists(id) || lf.ExistsByID(id)
			} else if filename != "" {
				exists = lf.GetByFilename(filename) > 0
			}
			resp := NewSuccessResponse("file exists", map[string]interface{}{
				"fileDataID": id,
				"filename":   filename,
				"exists":     exists,
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "encoding" || cmd.Name() == "encoding" {
			id, _ := cmd.Flags().GetUint32("file-data-id")
			info, err := fs.GetEncodingInfo(id)
			if err != nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file encoding", "not_found", err.Error()))
			}
			resp := NewSuccessResponse("file encoding", info)
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "get" || cmd.Name() == "get" {
			id, _ := cmd.Flags().GetUint32("file-data-id")
			outPath, _ := cmd.Flags().GetString("output")
			if outPath != "" {
				info, err := fs.GetEncodingInfo(id)
				if err != nil {
					return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file get", "not_found", err.Error()))
				}
				resp := NewSuccessResponse("file get", map[string]interface{}{
					"fileDataID": id,
					"output":     outPath,
					"info":       info,
					"note":       "raw file retrieval requires full CASC context",
				})
				return writeJSON(cmd.OutOrStdout(), resp)
			}
		}

		if use == "export" || cmd.Name() == "export" {
			id, _ := cmd.Flags().GetUint32("file-data-id")
			outPath, _ := cmd.Flags().GetString("output")
			if outPath == "" {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file export", "missing_argument", "--output is required"))
			}
			info, _ := fs.GetEncodingInfo(id)
			resp := NewSuccessResponse("file export", map[string]interface{}{
				"fileDataID": id,
				"output":     outPath,
				"encoding":   info,
				"note":       "file export requires full CASC context",
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("file", map[string]interface{}{
			"help": "file subcommands: lookup, search, extension, get, exists, encoding, export",
		}))
	}
}

var _ = os.ReadFile
