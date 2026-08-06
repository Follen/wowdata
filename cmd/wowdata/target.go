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
	AutoSource  bool
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
	explicitSource := flagChanged(cmd, "source")
	explicitPath := flagChanged(cmd, "path")

	if profileName != "" {
		if target.Source != "" || target.Path != "" || target.Region != "" || target.Product != "" || target.Build != "" || target.Locale != "" {
			return targetResolution{}, targetRequiredError{Err: fmt.Errorf("--profile cannot be combined with target fields")}
		}
		profile, err := layout.LoadProfile(profileName)
		if err != nil {
			return targetResolution{}, targetRequiredError{Err: fmt.Errorf("load profile %s: %w", profileName, err)}
		}
		target = profile.Target
		explicitSource = true
		explicitPath = target.Path != ""
	}
	autoSource := false
	if target.Source == "" {
		autoSource = true
		target.Source = "remote"
	}
	env, err := layout.LoadEnv()
	if err != nil {
		return targetResolution{}, targetRequiredError{Err: fmt.Errorf("load .env: %w", err)}
	}
	if target.Source == "local" && target.Path == "" && env.GameDir != "" {
		target.Path = env.GameDir
		explicitPath = true
	}
	if autoSource && env.GameDir != "" && target.Product != "" && target.Build != "" && localBuildAvailable(env.GameDir, target.Product, target.Build) {
		target.Source = "local"
		target.Path = env.GameDir
	}
	if missing := target.MissingFields(); len(missing) > 0 {
		return targetResolution{}, targetRequiredError{Missing: missing, Err: fmt.Errorf("target fields are required: %s", strings.Join(missing, ","))}
	}
	if autoSource && target.Source == "remote" && explicitPath {
		return targetResolution{}, targetRequiredError{Err: fmt.Errorf("--path requires --source local")}
	}
	if explicitSource && target.Source == "local" && target.Path == "" {
		return targetResolution{}, targetRequiredError{Missing: []string{"path"}, Err: fmt.Errorf("target fields are required: path")}
	}
	if err := target.Validate(); err != nil {
		return targetResolution{}, targetRequiredError{Err: err}
	}
	if _, ok := casc.LocaleFlagByNameOK(target.Locale); !ok {
		return targetResolution{}, targetRequiredError{Err: fmt.Errorf("unsupported locale %q", target.Locale)}
	}
	return targetResolution{Target: target, ProfileName: profileName, AutoSource: autoSource}, nil
}

func flagChanged(cmd *cobra.Command, name string) bool {
	if flag := cmd.Flags().Lookup(name); flag != nil && flag.Changed {
		return true
	}
	if flag := cmd.InheritedFlags().Lookup(name); flag != nil && flag.Changed {
		return true
	}
	if flag := cmd.Root().PersistentFlags().Lookup(name); flag != nil && flag.Changed {
		return true
	}
	return false
}

func localBuildAvailable(path, product, build string) bool {
	local := casc.NewCASCLocal(path)
	if err := local.Init(); err != nil {
		return false
	}
	return buildIndexBySelection(local.Builds, product, build) >= 0
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
		opts.AutoSource = resolved.AutoSource
		opts.CacheRoot = resolveCacheRoot(opts.CacheRoot, nil)
		mergeCommandDependencies(cmd, args, &opts)

		fmt.Fprintf(cmd.ErrOrStderr(), "prepare target=%s/%s/%s/%s build=%s\n", opts.Source, opts.Region, opts.Product, opts.Locale, opts.Build)
		lock, err := rt.Layout.AcquireLock(fmt.Sprintf("%s|%s|%s|%s|%s|%s", opts.Source, opts.Path, opts.Region, opts.Product, opts.Build, opts.Locale), 2*time.Minute)
		if err != nil {
			return app.NewResponseWriter(cmd.OutOrStdout()).Error(commandLabel(cmd), "cache_lock_failed", err.Error())
		}
		casc.SetDownloadProgressWriter(cmd.ErrOrStderr())
		if _, err := rt.initialize(opts); err != nil {
			lock.Release()
			return writeWarmupInitError(cmd, err)
		}
		// Initialization publishes immutable Build/CASC/DB2 handles. Do not hold
		// the process lock while executing the read-only command handler.
		lock.Release()
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
	if path == "wowdata casc info" || path == "wowdata casc diagnose" {
		opts.MetadataOnly = true
	}
	if path == "wowdata casc diagnose" {
		opts.WarmTACTKeys = true
	}
	if flag := cmd.Flags().Lookup("file-data-id"); flag != nil && flag.Changed {
		if id, err := cmd.Flags().GetUint32("file-data-id"); err == nil && id != 0 {
			opts.FileDataIDs = append(opts.FileDataIDs, id)
		}
	}
	switch {
	case strings.HasPrefix(path, "wowdata db2 "):
		if len(args) > 0 {
			add(args[0])
		}
	case strings.HasPrefix(path, "wowdata spell "):
		add("SpellName", "Spell", "SpellEffect", "SpellMisc", "SpellCastTimes", "SpellDuration", "SpellRange")
	case path == "wowdata encounter get":
		add("JournalEncounterSection")
	case path == "wowdata encounter export":
		add("JournalInstance", "JournalEncounter", "JournalEncounterSection", "SpellName", "Spell", "SpellEffect", "SpellMisc", "SpellCastTimes", "SpellDuration", "SpellRange")
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
	// The listfile is a large optional name index. FileDataID, DB2, and icon
	// paths resolve through root/encoding directly and must not pay its cost.
	switch path {
	case "wowdata file lookup", "wowdata file search", "wowdata file extension":
		opts.WarmListfile = true
	case "wowdata file get", "wowdata file exists", "wowdata file export":
		filename, _ := cmd.Flags().GetString("filename")
		opts.WarmListfile = strings.TrimSpace(filename) != ""
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
