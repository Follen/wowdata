package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"wowdata/internal/app"
	"wowdata/internal/casc"
	"wowdata/internal/storage"

	"github.com/spf13/cobra"
)

type targetResolution struct {
	Target      storage.Target
	ProfileName string
}

type targetRequiredError struct {
	Missing []string
	Err     error
}

func (e targetRequiredError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "complete data target is required"
}

func resolveTarget(cmd *cobra.Command, layout storage.Layout) (targetResolution, error) {
	profileName, _ := commandStringFlag(cmd, "profile")
	target := storage.Target{}
	target.Source, _ = commandStringFlag(cmd, "source")
	target.Path, _ = commandStringFlag(cmd, "path")
	target.Region, _ = commandStringFlag(cmd, "region")
	target.Product, _ = commandStringFlag(cmd, "product")
	target.Build, _ = commandStringFlag(cmd, "build")
	target.Locale, _ = commandStringFlag(cmd, "locale")

	if profileName != "" {
		if target.Source != "" || target.Path != "" || target.Region != "" || target.Product != "" || target.Build != "" || target.Locale != "" {
			return targetResolution{}, targetRequiredError{Err: fmt.Errorf("--profile cannot be combined with target fields")}
		}
		profile, err := layout.LoadProfile(profileName)
		if err != nil {
			return targetResolution{}, targetRequiredError{Err: fmt.Errorf("load profile %s: %w", profileName, err)}
		}
		target = profile.Target
	}
	if missing := target.MissingFields(); len(missing) > 0 {
		return targetResolution{}, targetRequiredError{Missing: missing, Err: fmt.Errorf("target fields are required: %s", strings.Join(missing, ","))}
	}
	if err := target.Validate(); err != nil {
		return targetResolution{}, targetRequiredError{Err: err}
	}
	if _, ok := casc.LocaleFlagByNameOK(target.Locale); !ok {
		return targetResolution{}, targetRequiredError{Err: fmt.Errorf("unsupported locale %q", target.Locale)}
	}
	return targetResolution{Target: target, ProfileName: profileName}, nil
}

func prepareThen(rt *Runtime, handler func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if !commandNeedsPreparation(cmd, args) {
			return handler(cmd, args)
		}
		if err := rt.Layout.Ensure(); err != nil {
			return app.NewResponseWriter(cmd.OutOrStdout()).Error(commandLabel(cmd), "home_init_failed", err.Error())
		}
		resolved, err := resolveTarget(cmd, rt.Layout)
		if err != nil {
			missing := []string{}
			if targetErr, ok := err.(targetRequiredError); ok {
				missing = targetErr.Missing
			}
			return app.NewResponseWriter(cmd.OutOrStdout()).Error(commandLabel(cmd), "target_required", targetErrorMessage(err, missing))
		}
		opts := warmupOptionsFromCommand(cmd)
		opts.Source = resolved.Target.Source
		opts.Path = resolved.Target.Path
		opts.Region = resolved.Target.Region
		opts.Product = resolved.Target.Product
		opts.Build = resolved.Target.Build
		opts.Locale = resolved.Target.Locale
		opts.Profile = resolved.ProfileName
		opts.CacheRoot = resolveCacheRoot(opts.CacheRoot, nil)
		mergeCommandDependencies(cmd, args, &opts)

		fmt.Fprintf(cmd.ErrOrStderr(), "prepare target=%s/%s/%s/%s build=%s\n", opts.Source, opts.Region, opts.Product, opts.Locale, opts.Build)
		lock, err := rt.Layout.AcquireLock(fmt.Sprintf("%s|%s|%s|%s|%s|%s", opts.Source, opts.Path, opts.Region, opts.Product, opts.Build, opts.Locale), 2*time.Minute)
		if err != nil {
			return app.NewResponseWriter(cmd.OutOrStdout()).Error(commandLabel(cmd), "cache_lock_failed", err.Error())
		}
		defer lock.Release()
		casc.SetDownloadProgressWriter(cmd.ErrOrStderr())
		if _, err := rt.initialize(opts); err != nil {
			return writeWarmupInitError(cmd, err)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "prepare status=ready")
		return handler(cmd, args)
	}
}

func targetErrorMessage(err error, missing []string) string {
	message := err.Error()
	if len(missing) > 0 {
		message += "; pass all target flags or --profile. Discover remote combinations with: wowdata casc products --source remote --region <region>"
	}
	return message
}

func commandNeedsPreparation(cmd *cobra.Command, args []string) bool {
	if cmd == nil || (cmd.HasSubCommands() && len(args) == 0) {
		return false
	}
	path := cmd.CommandPath()
	switch {
	case path == "wowdata warmup", path == "wowdata casc products":
		return false
	case strings.HasPrefix(path, "wowdata golden"), strings.HasPrefix(path, "wowdata video"):
		return false
	case strings.HasPrefix(path, "wowdata cache"), strings.HasPrefix(path, "wowdata profile"):
		return false
	case path == "wowdata doctor", path == "wowdata update", path == "wowdata uninstall":
		return false
	default:
		return true
	}
}

func commandLabel(cmd *cobra.Command) string {
	return strings.TrimPrefix(cmd.CommandPath(), "wowdata ")
}

func mergeCommandDependencies(cmd *cobra.Command, args []string, opts *warmupOptions) {
	tables := append([]string{}, opts.Tables...)
	add := func(values ...string) { tables = append(tables, values...) }
	path := cmd.CommandPath()
	switch {
	case strings.HasPrefix(path, "wowdata db2 "):
		if len(args) > 0 {
			add(args[0])
		}
	case strings.HasPrefix(path, "wowdata spell "):
		add("SpellName", "Spell", "SpellEffect", "SpellMisc", "SpellCastTimes", "SpellDuration", "SpellRange")
	case path == "wowdata encounter get":
		add("JournalEncounterSection")
	case strings.HasPrefix(path, "wowdata item "):
		add("Item", "ItemSparse", "ModelFileData", "TextureFileData", "ComponentModelFileData", "ItemDisplayInfo", "HelmetGeosetData", "ItemDisplayInfoMaterialRes", "ItemModifiedAppearance", "ItemAppearance")
	case strings.HasPrefix(path, "wowdata creature "):
		add("CreatureDisplayInfo", "CreatureModelData", "CreatureDisplayInfoGeosetData")
	case strings.HasPrefix(path, "wowdata decor "):
		add("HouseDecor")
	}
	if len(tables) > 0 {
		opts.WarmDBDManifest = true
	}
	if strings.HasPrefix(path, "wowdata file ") || strings.HasPrefix(path, "wowdata icon ") || strings.HasPrefix(path, "wowdata item ") || strings.HasPrefix(path, "wowdata creature ") {
		opts.WarmListfile = true
	}
	seen := make(map[string]bool, len(tables))
	unique := tables[:0]
	for _, table := range tables {
		table = strings.TrimSpace(table)
		if table == "" || seen[table] {
			continue
		}
		seen[table] = true
		unique = append(unique, table)
	}
	sort.Strings(unique)
	opts.Tables = unique
}

func buildIndexBySelection(builds []casc.VersionEntry, product, selection string) int {
	for i, build := range builds {
		if build.Product != product {
			continue
		}
		if selection == "latest" || selection == build.Version || selection == build.VersionsName || selection == build.BuildConfig || selection == build.BuildKey {
			return i
		}
		version := build.VersionsName
		if version == "" {
			version = build.Version
		}
		if dot := strings.LastIndex(version, "."); dot >= 0 && version[dot+1:] == selection {
			return i
		}
	}
	return -1
}
