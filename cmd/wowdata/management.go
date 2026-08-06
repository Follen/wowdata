package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"wowdata/internal/app"
	"wowdata/internal/resource"
	"wowdata/internal/storage"

	"github.com/spf13/cobra"
)

func profileHandler(rt *Runtime) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := rt.Layout.Ensure(); err != nil {
			return app.NewResponseWriter(cmd.OutOrStdout()).Error("profile "+cmd.Name(), "home_init_failed", err.Error())
		}
		switch cmd.Name() {
		case "list":
			profiles, err := rt.Layout.ListProfiles()
			if err != nil {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("profile list", "read_failed", err.Error())
			}
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("profile list", map[string]any{"profiles": profiles})
		case "show":
			if len(args) != 1 {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("profile show", "missing_argument", "profile name is required")
			}
			profile, err := rt.Layout.LoadProfile(args[0])
			if err != nil {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("profile show", "read_failed", err.Error())
			}
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("profile show", profile)
		case "set":
			if len(args) != 1 {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("profile set", "missing_argument", "profile name is required")
			}
			resolved, err := resolveTarget(cmd, rt.Layout)
			if err != nil {
				missing := []string{}
				if targetErr, ok := err.(targetRequiredError); ok {
					missing = targetErr.Missing
				}
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("profile set", "target_required", targetErrorMessage(err, missing))
			}
			if resolved.ProfileName != "" {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("profile set", "invalid_argument", "--profile cannot be used while defining a profile")
			}
			profile := storage.Profile{Name: args[0], Target: resolved.Target}
			if err := rt.Layout.SaveProfile(profile); err != nil {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("profile set", "write_failed", err.Error())
			}
			profile, _ = rt.Layout.LoadProfile(args[0])
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("profile set", profile)
		case "remove":
			if len(args) != 1 {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("profile remove", "missing_argument", "profile name is required")
			}
			path, err := rt.Layout.ProfilePath(args[0])
			if err != nil {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("profile remove", "invalid_argument", err.Error())
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("profile remove", "remove_failed", err.Error())
			}
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("profile remove", map[string]any{"name": args[0], "removed": true})
		default:
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("profile", map[string]any{"subcommands": []string{"list", "show", "set", "remove"}})
		}
	}
}

func cacheHandler(rt *Runtime) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := rt.Layout.Ensure(); err != nil {
			return app.NewResponseWriter(cmd.OutOrStdout()).Error("cache "+cmd.Name(), "home_init_failed", err.Error())
		}
		config, err := rt.Layout.LoadConfig()
		if err != nil {
			return app.NewResponseWriter(cmd.OutOrStdout()).Error("cache "+cmd.Name(), "config_failed", err.Error())
		}
		switch cmd.Name() {
		case "status":
			usage, err := storage.MeasureCacheUsage(rt.Layout.Cache, config.CacheMaxBytes)
			if err != nil {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("cache status", "read_failed", err.Error())
			}
			format, err := rt.Layout.LoadCacheFormat()
			if err != nil {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("cache status", "read_failed", err.Error())
			}
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("cache status", map[string]any{
				"path": filepath.ToSlash(rt.Layout.Cache), "sizeBytes": usage.TotalBytes,
				"maxBytes": config.CacheMaxBytes, "downloadWorkers": config.Workers,
				"usage": usage, "format": format,
			})
		case "verify":
			result := rt.Layout.VerifyCache()
			if !result.OK {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("cache verify", "cache_invalid", fmt.Sprintf("missing=%d corrupt=%d errors=%d", len(result.Missing), len(result.Corrupt), len(result.Errors)))
			}
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("cache verify", result)
		case "prune":
			result, err := rt.Layout.PruneCache(config.CacheMaxBytes)
			if err != nil {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("cache prune", "prune_failed", err.Error())
			}
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("cache prune", result)
		case "clear":
			before, _ := storage.DirSize(rt.Layout.Cache)
			if err := rt.Layout.ClearCache(); err != nil {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("cache clear", "clear_failed", err.Error())
			}
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("cache clear", map[string]any{"removedBytes": before, "path": filepath.ToSlash(rt.Layout.Cache)})
		case "config":
			maxGB, _ := cmd.Flags().GetInt("max-gb")
			workers, _ := cmd.Flags().GetInt("workers")
			autoWorkers, _ := cmd.Flags().GetBool("auto-workers")
			if workers > 0 && autoWorkers {
				return app.NewResponseWriter(cmd.OutOrStdout()).Error("cache config", "invalid_argument", "--workers and --auto-workers cannot be combined")
			}
			if maxGB > 0 {
				config.CacheMaxBytes = int64(maxGB) * 1024 * 1024 * 1024
			}
			if workers > 0 {
				config.Workers = workers
				config.WorkerMode = storage.WorkerModeFixed
			}
			if autoWorkers {
				config.Workers = 0
				config.WorkerMode = storage.WorkerModeAuto
			}
			if maxGB > 0 || workers > 0 || autoWorkers {
				if err := rt.Layout.SaveConfig(config); err != nil {
					return app.NewResponseWriter(cmd.OutOrStdout()).Error("cache config", "write_failed", err.Error())
				}
				rt.Config = config
			}
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("cache config", config)
		default:
			return app.NewResponseWriter(cmd.OutOrStdout()).Success("cache", map[string]any{"subcommands": []string{"status", "verify", "prune", "clear", "config"}})
		}
	}
}

type doctorCheck struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Detail      string `json:"detail,omitempty"`
	Next        string `json:"next,omitempty"`
	ProblemCode string `json:"problemCode,omitempty"`
}

func doctorHandler(rt *Runtime) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		checks := []doctorCheck{}
		add := func(name, status, detail, code, next string) {
			checks = append(checks, doctorCheck{Name: name, Status: status, Detail: detail, ProblemCode: code, Next: next})
		}
		if exe, err := os.Executable(); err == nil {
			add("cli", "ok", filepath.ToSlash(exe)+" "+app.Version, "", "")
		} else {
			add("cli", "error", err.Error(), "cli_path", "reinstall @follenfang/wowdata")
		}
		if _, err := exec.LookPath("npm"); err == nil {
			add("npm", "ok", "npm is available", "", "")
		} else {
			add("npm", "error", err.Error(), "npm_missing", "install Node.js/npm")
		}
		for _, entry := range []struct{ name, path string }{
			{"root", rt.Layout.Root}, {"config", rt.Layout.Config}, {"profiles", rt.Layout.Profiles},
			{"builds", rt.Layout.Builds}, {"cache", rt.Layout.Cache}, {"tmp", rt.Layout.Temp}, {"locks", rt.Layout.Locks},
		} {
			name, path := entry.name, entry.path
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				if info.Mode().Perm()&0o200 == 0 {
					add("directory_"+name, "error", filepath.ToSlash(path)+" writable=false", "directory_not_writable", "fix directory permissions")
				} else {
					add("directory_"+name, "ok", filepath.ToSlash(path)+" writable=true", "", "")
				}
			} else {
				add("directory_"+name, "error", filepath.ToSlash(path), "directory_missing", "run wowdata cache status")
			}
		}
		checks = append(checks, doctorResidueChecks(rt.Layout)...)
		if snapshot, err := latestBuildSnapshot(rt.Layout); err != nil {
			add("active_target", "error", err.Error(), "build_state_invalid", "inspect ~/.wowdata/builds")
		} else if snapshot == nil {
			add("active_target", "unknown", "no resolved build snapshot", "", "run a data command with a complete target or profile")
		} else {
			add("active_target", "ok", fmt.Sprintf("%s/%s/%s/%s build=%s", snapshot.Source, snapshot.Region, snapshot.Product, snapshot.Locale, snapshot.Version), "", "")
		}
		config, err := rt.Layout.LoadConfig()
		if err != nil {
			add("config", "error", err.Error(), "config_invalid", "inspect ~/.wowdata/config/config.json")
		} else {
			add("config", "ok", fmt.Sprintf("max=%d workers=%d", config.CacheMaxBytes, config.Workers), "", "")
		}
		profiles, err := rt.Layout.ListProfiles()
		if err != nil {
			add("profiles", "error", err.Error(), "profiles_invalid", "run wowdata profile list")
		} else if len(profiles) == 0 {
			add("profiles", "unknown", "no profiles", "", "create a profile or pass a complete target")
		} else {
			add("profiles", "ok", fmt.Sprintf("%d profiles", len(profiles)), "", "")
		}
		verification := rt.Layout.VerifyCache()
		if verification.OK {
			add("cache_integrity", "ok", fmt.Sprintf("%d files", verification.Files), "", "")
		} else {
			add("cache_integrity", "error", fmt.Sprintf("missing=%d corrupt=%d errors=%d", len(verification.Missing), len(verification.Corrupt), len(verification.Errors)), "cache_invalid", "run wowdata cache verify")
		}
		checkDoctorNetwork(cmd.Context(), profiles, &checks)
		ok := true
		for _, check := range checks {
			if check.Status == "error" {
				ok = false
				break
			}
		}
		return app.NewResponseWriter(cmd.OutOrStdout()).Success("doctor", map[string]any{"ok": ok, "go": runtime.Version(), "checks": checks})
	}
}

func checkDoctorNetwork(parent context.Context, profiles []storage.Profile, checks *[]doctorCheck) {
	ctx, cancel := context.WithTimeout(parent, 6*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 5 * time.Second}
	urls := map[string]string{
		"wowdbdefs": "https://raw.githubusercontent.com/wowdev/WoWDBDefs/master/manifest.json",
		"listfile":  "https://github.com/wowdev/wow-listfile/releases/latest/download/community-listfile.csv",
		"tact_keys": "https://raw.githubusercontent.com/wowdev/TACTKeys/master/WoW.txt",
	}
	if len(profiles) > 0 && profiles[0].Target.Source == "remote" {
		region := profiles[0].Target.Region
		host := fmt.Sprintf("http://%s.patch.battle.net:1119/", region)
		if region == "cn" {
			host = "https://cn.version.battlenet.com.cn/"
		}
		urls["cdn_versions"] = host + profiles[0].Target.Product + "/versions"
	} else {
		*checks = append(*checks, doctorCheck{Name: "cdn_versions", Status: "unknown", Detail: "no remote profile available", Next: "create a remote profile or run casc products with an explicit region"})
	}
	type result struct{ name, status, detail, code, next string }
	results := make(chan result, len(urls))
	for name, url := range urls {
		go func(name, url string) {
			req, _ := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
			resp, err := client.Do(req)
			if err != nil {
				results <- result{name, "error", err.Error(), "network_unreachable", "check network or use verified offline cache"}
				return
			}
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				results <- result{name, "error", resp.Status, "upstream_http", "retry later"}
				return
			}
			results <- result{name, "ok", resp.Status, "", ""}
		}(name, url)
	}
	for range urls {
		r := <-results
		*checks = append(*checks, doctorCheck{Name: r.name, Status: r.status, Detail: r.detail, ProblemCode: r.code, Next: r.next})
	}
}

func doctorResidueChecks(layout storage.Layout) []doctorCheck {
	return []doctorCheck{
		doctorResidueCheck("locks", layout.Locks, 30*time.Minute, "stale_locks", "wait for active commands, then remove stale lock files"),
		doctorResidueCheck("temporary_files", layout.Temp, 30*time.Minute, "stale_temporary_files", "run wowdata cache verify, then remove stale temporary files"),
	}
}

func doctorResidueCheck(name, path string, staleAfter time.Duration, code, next string) doctorCheck {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return doctorCheck{Name: name, Status: "unknown", Detail: "directory does not exist", Next: "run wowdata cache status"}
	}
	if err != nil {
		return doctorCheck{Name: name, Status: "error", Detail: err.Error(), ProblemCode: code, Next: next}
	}
	stale := 0
	for _, entry := range entries {
		if info, infoErr := entry.Info(); infoErr == nil && time.Since(info.ModTime()) > staleAfter {
			stale++
		}
	}
	if stale > 0 {
		return doctorCheck{Name: name, Status: "error", Detail: fmt.Sprintf("entries=%d stale=%d", len(entries), stale), ProblemCode: code, Next: next}
	}
	if len(entries) > 0 {
		return doctorCheck{Name: name, Status: "unknown", Detail: fmt.Sprintf("entries=%d active_or_recent=true", len(entries)), Next: "wait for active commands and run doctor again"}
	}
	return doctorCheck{Name: name, Status: "ok", Detail: "entries=0"}
}

func latestBuildSnapshot(layout storage.Layout) (*storage.BuildSnapshot, error) {
	entries, err := os.ReadDir(layout.Builds)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var latest *storage.BuildSnapshot
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		body, err := resource.ReadFile(filepath.Join(layout.Builds, entry.Name()))
		if err != nil {
			return nil, err
		}
		var snapshot storage.BuildSnapshot
		if err := json.Unmarshal(body, &snapshot); err != nil {
			return nil, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		if latest == nil || snapshot.LastUsedAt > latest.LastUsedAt {
			copy := snapshot
			latest = &copy
		}
	}
	return latest, nil
}

func updateHandler(rt *Runtime) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		version, _ := cmd.Flags().GetString("version")
		if version == "" {
			version = "latest"
		}
		if !validNPMVersion(version) {
			return app.NewResponseWriter(cmd.OutOrStdout()).Error("update", "invalid_version", "version must be latest or an exact semantic version")
		}
		output, err := runNPM(cmd, "install", "-g", "@follenfang/wowdata@"+version)
		if err != nil {
			return app.NewResponseWriter(cmd.OutOrStdout()).Error("update", "npm_update_failed", output)
		}
		return app.NewResponseWriter(cmd.OutOrStdout()).Success("update", map[string]any{"package": "@follenfang/wowdata", "version": version, "output": output})
	}
}

func uninstallHandler(rt *Runtime) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		keepData, _ := cmd.Flags().GetBool("keep-data")
		if keepData {
			_ = os.Setenv("WOWDATA_KEEP_DATA", "1")
			defer os.Unsetenv("WOWDATA_KEEP_DATA")
		}
		output, err := runNPM(cmd, "uninstall", "-g", "@follenfang/wowdata")
		if err != nil {
			return app.NewResponseWriter(cmd.OutOrStdout()).Error("uninstall", "npm_uninstall_failed", output)
		}
		return app.NewResponseWriter(cmd.OutOrStdout()).Success("uninstall", map[string]any{"package": "@follenfang/wowdata", "keepData": keepData, "output": output})
	}
}

func runNPM(cmd *cobra.Command, args ...string) (string, error) {
	executable := "npm"
	commandArgs := args
	if runtime.GOOS == "windows" {
		executable = os.Getenv("ComSpec")
		if executable == "" {
			executable = "cmd.exe"
		}
		commandArgs = append([]string{"/d", "/s", "/c", "npm.cmd"}, args...)
	}
	process := exec.CommandContext(cmd.Context(), executable, commandArgs...)
	process.Env = os.Environ()
	output, err := process.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

var exactNPMVersion = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

func validNPMVersion(version string) bool {
	return version == "latest" || exactNPMVersion.MatchString(version)
}
