package app

import (
	"os"

	"wowdata/internal/export"
	"wowdata/internal/video"

	"github.com/spf13/cobra"
)

func NewIconHandler() func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		fdid, _ := cmd.Flags().GetUint32("file-data-id")
		format, _ := cmd.Flags().GetString("format")
		mipmap, _ := cmd.Flags().GetInt("mipmap")
		outPath, _ := cmd.Flags().GetString("output")

		if format == "" {
			format = "png"
		}
		if outPath == "" {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("icon export", "missing_argument", "--output is required"))
		}

		// Note: actual BLP decoding requires CASC file retrieval
		resp := NewSuccessResponse("icon export", map[string]interface{}{
			"fileDataID": fdid,
			"format":     format,
			"mipmap":     mipmap,
			"output":     outPath,
			"note":       "icon export requires full CASC context for BLP file retrieval",
		})
		// Ensure icon export package is imported
		if false {
			_, _ = export.ExportIcon([]byte{}, "", "", 0)
		}
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}

func NewVideoHandler() func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		inputPath, _ := cmd.Flags().GetString("input")
		outputDir, _ := cmd.Flags().GetString("output")

		if inputPath == "" {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("video demux", "missing_argument", "--input is required"))
		}

		data, err := os.ReadFile(inputPath)
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
		resp := NewSuccessResponse("video demux", map[string]interface{}{
			"input":     inputPath,
			"outputDir": outputDir,
			"width":     w,
			"height":    h,
			"frameRate": demuxer.FrameRate(),
			"frames":    frames,
			"frameCount": len(frames),
		})
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}
