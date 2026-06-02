package app

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"wowdata/internal/listfile"
	appruntime "wowdata/internal/local/runtime"
	"wowdata/internal/shared/casc"
	"wowdata/internal/shared/export"

	"github.com/spf13/cobra"
)

const warmupRequiredMessage = "CASC 未就绪，请先调用 wow_warmup"

func NewFileHandler(lf *listfile.Listfile, fs *casc.FileService) func(cmd *cobra.Command, args []string) error {
	if lf == nil && fs == nil {
		return NewFileHandlerWithStore(nil)
	}
	return NewFileHandlerWithStore(appruntime.NewCASCFileStore(lf, fs, nil))
}

func NewFileHandlerWithStore(store appruntime.FileStore) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		parent := cmd.Parent()
		use := ""
		if parent != nil {
			use = cmd.Name()
		}

		if use == "lookup" || cmd.Name() == "lookup" {
			if fileStoreNotReady(store) {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file lookup", "not_ready", warmupRequiredMessage))
			}
			id, _ := cmd.Flags().GetUint32("file-data-id")
			name, found := store.Lookup(id)
			if !found {
				name = "unknown"
			}
			resp := NewSuccessResponse("file lookup", map[string]interface{}{
				"fileDataID": id,
				"fileName":   name,
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "search" || cmd.Name() == "search" {
			if fileStoreNotReady(store) {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file search", "not_ready", warmupRequiredMessage))
			}
			query, _ := cmd.Flags().GetString("query")
			limit, _ := cmd.Flags().GetInt("limit")
			results := store.Search(query, limit)
			resp := NewSuccessResponse("file search", map[string]interface{}{
				"search":   query,
				"total":    store.SearchCount(query),
				"returned": len(results),
				"files":    entriesToFiles(results),
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "extension" || cmd.Name() == "extension" {
			if fileStoreNotReady(store) {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file extension", "not_ready", warmupRequiredMessage))
			}
			ext, _ := cmd.Flags().GetString("extension")
			limit, _ := cmd.Flags().GetInt("limit")
			results := store.Extension(ext, limit)
			resp := NewSuccessResponse("file extension", map[string]interface{}{
				"extension": ext,
				"total":     store.ExtensionCount(ext),
				"returned":  len(results),
				"files":     entriesToFormattedFiles(results),
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "exists" || cmd.Name() == "exists" {
			if fileStoreNotReady(store) {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file exists", "not_ready", warmupRequiredMessage))
			}
			id, _ := cmd.Flags().GetUint32("file-data-id")
			filename, _ := cmd.Flags().GetString("filename")
			exists := false
			if id > 0 {
				exists = store.ExistsByID(id)
			} else if filename != "" {
				exists = store.ExistsByName(filename)
			}
			resp := NewSuccessResponse("file exists", map[string]interface{}{
				"fileDataID": id,
				"filename":   filename,
				"exists":     exists,
			})
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "encoding" || cmd.Name() == "encoding" {
			if store == nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file encoding", "not_ready", warmupRequiredMessage))
			}
			id, _ := cmd.Flags().GetUint32("file-data-id")
			info, err := store.EncodingInfo(id)
			if err != nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file encoding", "not_found", err.Error()))
			}
			resp := NewSuccessResponse("file encoding", info)
			return writeJSON(cmd.OutOrStdout(), resp)
		}

		if use == "get" || cmd.Name() == "get" {
			if store == nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file get", "not_ready", warmupRequiredMessage))
			}
			id, _ := cmd.Flags().GetUint32("file-data-id")
			filename, _ := cmd.Flags().GetString("filename")
			outPath, _ := cmd.Flags().GetString("output")
			data, err := readFileStoreData(store, id, filename)
			if err != nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file get", fileReadErrorCode(err), err.Error()))
			}
			if outPath != "" {
				result, err := export.ExportFile(data, outPath)
				if err != nil {
					return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file get", "io_error", err.Error()))
				}
				return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("file get", result))
			}
			return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("file get", map[string]interface{}{
				"fileDataID": id,
				"filename":   filename,
				"size":       len(data),
				"sha256":     fmt.Sprintf("%x", sha256.Sum256(data)),
			}))
		}

		if use == "export" || cmd.Name() == "export" {
			if store == nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file export", "not_ready", warmupRequiredMessage))
			}
			id, _ := cmd.Flags().GetUint32("file-data-id")
			filename, _ := cmd.Flags().GetString("filename")
			outPath, _ := cmd.Flags().GetString("output")
			if outPath == "" {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file export", "missing_argument", "--output is required"))
			}
			data, err := readFileStoreData(store, id, filename)
			if err != nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file export", fileReadErrorCode(err), err.Error()))
			}
			result, err := export.ExportFile(data, outPath)
			if err != nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("file export", "io_error", err.Error()))
			}
			return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("file export", result))
		}

		return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("file", map[string]interface{}{
			"help": "file subcommands: lookup, search, extension, get, exists, encoding, export",
		}))
	}
}

type readyFileStore interface {
	Ready() bool
}

func fileStoreNotReady(store appruntime.FileStore) bool {
	if store == nil {
		return true
	}
	if ready, ok := store.(readyFileStore); ok {
		return !ready.Ready()
	}
	return false
}

func fileReadErrorCode(err error) string {
	if err != nil && strings.Contains(err.Error(), "not initialized") {
		return "not_ready"
	}
	return "not_found"
}

func readFileStoreData(store appruntime.FileStore, id uint32, filename string) ([]byte, error) {
	if id > 0 {
		return store.ReadByID(id)
	}
	return store.ReadByName(filename)
}

func entriesToFiles(entries []appruntime.FileEntry) []map[string]interface{} {
	files := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		files = append(files, map[string]interface{}{
			"fileDataID": entry.FileDataID,
			"fileName":   entry.Filename,
		})
	}
	return files
}

func entriesToFormattedFiles(entries []appruntime.FileEntry) []string {
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		files = append(files, fmt.Sprintf("%s [%d]", entry.Filename, entry.FileDataID))
	}
	return files
}
