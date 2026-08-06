package app

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"wowdata/internal/golden"
	"wowdata/internal/resource"

	"github.com/spf13/cobra"
)

func NewGoldenHandler() func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		parent := cmd.Parent()
		use := ""
		if parent != nil {
			use = cmd.Name()
		}

		if use == "capture" || cmd.Name() == "capture" {
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden capture", "missing_argument", "--name is required"))
			}
			commandLine, _ := cmd.Flags().GetString("command")
			if commandLine == "" {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden capture", "missing_argument", "--command is required"))
			}
			outputPath, _ := cmd.Flags().GetString("output")
			if outputPath == "" {
				outputPath = filepath.Join("fixtures", "golden", name+".json")
			}
			manifestPath, _ := cmd.Flags().GetString("manifest")
			capture, err := runGoldenCapture(name, commandLine)
			if err != nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden capture", "capture_failed", err.Error()))
			}
			if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden capture", "io_error", err.Error()))
			}
			data, err := resource.MarshalIndentJSON(capture, "", "  ")
			if err != nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden capture", "json_error", err.Error()))
			}
			if err := resource.WriteFile(outputPath, data, 0644); err != nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden capture", "io_error", err.Error()))
			}
			if manifestPath != "" {
				if err := upsertGoldenManifest(manifestPath, name, outputPath, outputPath); err != nil {
					return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden capture", "manifest_error", err.Error()))
				}
			}
			return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("golden capture", map[string]interface{}{
				"name":   name,
				"path":   outputPath,
				"status": capture.ExitCode,
			}))
		}

		if use == "compare" || cmd.Name() == "compare" {
			fixture, _ := cmd.Flags().GetString("fixture")
			all, _ := cmd.Flags().GetBool("all")

			if all {
				manifestPath := fixture
				if manifestPath == "" {
					manifestPath = filepath.Join("fixtures", "golden", "manifest.json")
				}
				data, err := resource.ReadFile(manifestPath)
				if err != nil {
					return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden compare", "io_error", err.Error()))
				}
				manifest, err := golden.ParseManifest(data)
				if err != nil {
					return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden compare", "invalid_manifest", err.Error()))
				}
				if missing := manifest.MissingRequiredGroups(); len(missing) > 0 {
					return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden compare", "missing_required_groups", "manifest is missing fixtures for required groups: "+strings.Join(missing, ",")))
				}
				if missing := manifest.MissingRequiredSuccessGroups(); len(missing) > 0 {
					return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden compare", "missing_required_success_groups", "manifest is missing success fixtures for required groups: "+strings.Join(missing, ",")))
				}
				manifestDir := filepath.Dir(manifestPath)
				results := make([]golden.CompareResult, 0, len(manifest.Fixtures))
				failed := 0
				firstFailure := ""
				for _, entry := range manifest.Fixtures {
					result, err := compareGoldenFilesWithMode(entry.Fixture, entry.Actual, entry.Comparison)
					if err != nil {
						result = golden.CompareResult{Fixture: entry.Fixture, Equal: false, Reason: err.Error()}
					}
					if !result.Equal {
						failed++
						if firstFailure == "" {
							firstFailure = result.Reason
						}
					}
					results = append(results, result)
					for _, artifactResult := range golden.CompareArtifacts(manifestDir, entry) {
						if !artifactResult.Equal {
							failed++
							if firstFailure == "" {
								firstFailure = artifactResult.Reason
							}
						}
						results = append(results, artifactResult)
					}
				}
				resp := map[string]interface{}{
					"manifest": manifestPath,
					"total":    len(results),
					"failed":   failed,
					"results":  results,
				}
				if failed > 0 {
					message := fmt.Sprintf("%d fixtures failed", failed)
					if firstFailure != "" {
						message += ": " + firstFailure
					}
					return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden compare", "compare_failed", message))
				}
				return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("golden compare", resp))
			}

			if fixture == "" {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden compare", "missing_argument", "--fixture is required"))
			}
			actual, _ := cmd.Flags().GetString("actual")
			if actual == "" {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden compare", "missing_argument", "--actual is required"))
			}
			result, err := compareGoldenFiles(fixture, actual)
			if err != nil {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden compare", "compare_error", err.Error()))
			}
			if !result.Equal {
				return writeJSON(cmd.OutOrStdout(), NewErrorResponse("golden compare", "compare_failed", result.Reason))
			}
			return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("golden compare", result))
		}

		resp := NewSuccessResponse("golden", map[string]interface{}{
			"help": "golden subcommands: capture, compare",
		})
		_ = fmt.Sprint
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}

func compareGoldenFiles(fixture, actual string) (golden.CompareResult, error) {
	return compareGoldenFilesWithMode(fixture, actual, "")
}

func compareGoldenFilesWithMode(fixture, actual, mode string) (golden.CompareResult, error) {
	expectedData, err := resource.ReadFile(fixture)
	if err != nil {
		return golden.CompareResult{Fixture: fixture}, err
	}
	actualData, err := resource.ReadFile(actual)
	if err != nil {
		return golden.CompareResult{Fixture: fixture}, err
	}
	result, err := golden.CompareJSONWithMode(expectedData, actualData, mode)
	result.Fixture = fixture
	return result, err
}

func upsertGoldenManifest(path string, name string, fixture string, actual string) error {
	manifest := &golden.Manifest{Version: 1}
	if data, err := resource.ReadFile(path); err == nil && len(data) > 0 {
		parsed, err := golden.ParseManifest(data)
		if err != nil {
			return err
		}
		manifest = parsed
	}
	entry := golden.ManifestEntry{Name: name, Group: manifestGroupFromName(name), Fixture: fixture, Actual: actual}
	replaced := false
	for i := range manifest.Fixtures {
		if manifest.Fixtures[i].Name == name {
			manifest.Fixtures[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		manifest.Fixtures = append(manifest.Fixtures, entry)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := resource.MarshalIndentJSON(manifest, "", "  ")
	if err != nil {
		return err
	}
	return resource.WriteFile(path, data, 0644)
}

func manifestGroupFromName(name string) string {
	for i, r := range name {
		if r == '/' || r == '\\' {
			return name[:i]
		}
	}
	return name
}

type goldenCapture struct {
	Name     string   `json:"name"`
	Command  string   `json:"command"`
	Args     []string `json:"args"`
	ExitCode int      `json:"exitCode"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
}

func runGoldenCapture(name string, commandLine string) (*goldenCapture, error) {
	parts, err := splitCommandLine(commandLine)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	commandName := parts[0]
	if commandName == "wowdata" {
		commandName = os.Args[0]
	}
	c := exec.Command(commandName, parts[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err = c.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}
	return &goldenCapture{
		Name:     name,
		Command:  parts[0],
		Args:     parts[1:],
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

func splitCommandLine(commandLine string) ([]string, error) {
	var parts []string
	var current strings.Builder
	var quote rune
	inToken := false
	runes := []rune(commandLine)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			if r == '\\' && i+1 < len(runes) && (runes[i+1] == quote || runes[i+1] == '\\') {
				i++
				current.WriteRune(runes[i])
				inToken = true
				continue
			}
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			inToken = true
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			inToken = true
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if inToken {
				parts = append(parts, current.String())
				current.Reset()
				inToken = false
			}
			continue
		}
		current.WriteRune(r)
		inToken = true
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in command")
	}
	if inToken {
		parts = append(parts, current.String())
	}
	return parts, nil
}
