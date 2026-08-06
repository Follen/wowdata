package app

import (
	"strings"

	"wowdata/internal/export"
	"wowdata/internal/resource"
	appruntime "wowdata/internal/runtime"
	"wowdata/internal/video"

	"github.com/spf13/cobra"
)

func NewIconHandler() func(cmd *cobra.Command, args []string) error {
	return NewIconHandlerWithStore(nil)
}

func NewIconHandlerWithStore(store appruntime.IconStore) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		fdid, _ := cmd.Flags().GetUint32("file-data-id")
		format, _ := cmd.Flags().GetString("format")
		mipmap, _ := cmd.Flags().GetInt("mipmap")
		mask, _ := cmd.Flags().GetInt("mask")
		outPath, _ := cmd.Flags().GetString("output")

		if format == "" {
			format = "png"
		}
		if outPath == "" {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("icon export", "missing_argument", "--output is required"))
		}
		if store == nil {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("icon export", "not_ready", "icon export is not connected to CASC runtime yet"))
		}
		format = strings.ToLower(strings.TrimSpace(format))
		if format != "png" && format != "webp" {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("icon export", "unsupported_format", "format must be png or webp"))
		}

		data, err := store.ReadByID(fdid)
		if err != nil {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("icon export", "not_found", err.Error()))
		}
		result, err := export.ExportIconWithOptions(data, outPath, format, mipmap, mask)
		if err != nil {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("icon export", "export_error", err.Error()))
		}
		return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("icon export", result))
	}
}

func NewVideoHandler() func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		inputPath, _ := cmd.Flags().GetString("input")
		outputDir, _ := cmd.Flags().GetString("output")

		if inputPath == "" {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("video demux", "missing_argument", "--input is required"))
		}

		data, err := resource.ReadFile(inputPath)
		if err != nil {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("video demux", "io_error", err.Error()))
		}

		demuxer := video.NewVP9AVIDemuxer(data)
		if err := demuxer.ParseHeader(); err != nil {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("video demux", "parse_error", err.Error()))
		}

		frames, err := demuxer.ExtractFrames()
		if err != nil {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("video demux", "extract_error", err.Error()))
		}

		w, h := demuxer.GetDimensions()
		responseData := map[string]interface{}{
			"input":      inputPath,
			"width":      w,
			"height":     h,
			"frameRate":  demuxer.FrameRate(),
			"frames":     frames,
			"frameCount": len(frames),
		}
		if outputDir != "" {
			responseData["outputDir"] = outputDir
		}
		resp := NewSuccessResponse("video demux", responseData)
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}
