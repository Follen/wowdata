package prune

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"wowdata/internal/server/config"
	"wowdata/internal/server/storage/metadata"
	"wowdata/internal/server/storage/sqlitewrite"
)

type Roots struct {
	CacheRoot    string
	DB2Dir       string
	RawDir       string
	ArtifactRoot string
}

type Options struct {
	DB     *sql.DB
	Roots  Roots
	Apply  bool
	Writer io.Writer
}

type Plan struct {
	Targets      []TargetPlan
	DeleteBuilds int
	DeletePaths  int
	DeleteBytes  int64
	DeleteRows   int64
}

type TargetPlan struct {
	Region  string
	Product string
	Locale  string
	Keep    []BuildPlan
	Delete  []BuildPlan
}

type BuildPlan struct {
	Build        Build
	KeepReason   string
	MetadataRows map[string]int64
	Paths        []PathPlan
}

type Build struct {
	Region    string
	Product   string
	Locale    string
	BuildKey  string
	BuildName string
	State     string
	Active    bool
}

type PathPlan struct {
	Kind       string
	Path       string
	Exists     bool
	Bytes      int64
	Safe       bool
	SkipReason string
}

type Result struct {
	Plan         Plan
	Applied      bool
	DeletedRows  int64
	DeletedPaths int
	DeletedBytes int64
	SkippedPaths int
}

type buildRow struct {
	Build
	discoveredAt string
	updatedAt    string
}

type targetKey struct {
	region  string
	product string
	locale  string
}

type buildTriple struct {
	region   string
	product  string
	buildKey string
}

type buildKey struct {
	region   string
	product  string
	locale   string
	buildKey string
}

var buildScopedTables = []string{
	"server_materialized_tables",
	"server_listfile_indexes",
	"server_listfile_sources",
	"server_casc_indexes",
	"server_casc_sources",
	"server_artifacts",
	"server_refresh_runs",
	"server_casc_archive_entries",
	"server_casc_encoding_entries",
	"server_casc_root_entries",
	"server_builds",
}

func RootsFromConfig(cfg config.Config) Roots {
	cacheRoot := cfg.Cache.Root
	db2Dir := cfg.Cache.DB2Dir
	if db2Dir == "" && cacheRoot != "" {
		db2Dir = filepath.Join(cacheRoot, "db2")
	}
	rawDir := cfg.Cache.RawDir
	if rawDir == "" && cacheRoot != "" {
		rawDir = filepath.Join(cacheRoot, "raw")
	}
	return Roots{
		CacheRoot:    cacheRoot,
		DB2Dir:       db2Dir,
		RawDir:       rawDir,
		ArtifactRoot: cfg.Artifacts.Root,
	}
}

func Run(ctx context.Context, opts Options) (Result, error) {
	if opts.DB == nil {
		return Result{}, errors.New("metadata db is required")
	}
	plan, err := BuildPlanForLatestOnly(ctx, opts.DB, opts.Roots)
	if err != nil {
		return Result{}, err
	}
	result := Result{Plan: plan, Applied: opts.Apply}
	writePlan(opts.Writer, plan, opts.Apply)
	if !opts.Apply {
		return result, nil
	}
	result, err = applyPlan(ctx, opts.DB, plan, opts.Writer)
	result.Plan = plan
	result.Applied = true
	return result, err
}

func BuildPlanForLatestOnly(ctx context.Context, db *sql.DB, roots Roots) (Plan, error) {
	builds, err := listBuilds(ctx, db)
	if err != nil {
		return Plan{}, err
	}
	grouped := map[targetKey][]buildRow{}
	var order []targetKey
	for _, build := range builds {
		key := targetKey{region: build.Region, product: build.Product, locale: build.Locale}
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], build)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].region != order[j].region {
			return order[i].region < order[j].region
		}
		if order[i].product != order[j].product {
			return order[i].product < order[j].product
		}
		return order[i].locale < order[j].locale
	})

	protectedRaw := map[buildTriple]bool{}
	latestByTarget := map[targetKey]string{}
	for key, targetBuilds := range grouped {
		sortBuildsLatestFirst(targetBuilds)
		latestByTarget[key] = targetBuilds[0].BuildKey
		for _, build := range targetBuilds {
			if keepReason(build.Build, targetBuilds[0].BuildKey) != "" {
				protectedRaw[buildTriple{region: build.Region, product: build.Product, buildKey: build.BuildKey}] = true
			}
		}
	}

	var plan Plan
	for _, key := range order {
		targetBuilds := grouped[key]
		sortBuildsLatestFirst(targetBuilds)
		target := TargetPlan{Region: key.region, Product: key.product, Locale: key.locale}
		for _, row := range targetBuilds {
			reason := keepReason(row.Build, latestByTarget[key])
			if reason != "" {
				target.Keep = append(target.Keep, BuildPlan{Build: row.Build, KeepReason: reason})
				continue
			}
			buildPlan, err := planDeletedBuild(ctx, db, roots, row.Build, protectedRaw)
			if err != nil {
				return Plan{}, err
			}
			target.Delete = append(target.Delete, buildPlan)
			plan.DeleteBuilds++
			for _, path := range buildPlan.Paths {
				if path.Exists && path.Safe {
					plan.DeletePaths++
					plan.DeleteBytes += path.Bytes
				}
			}
			for _, rows := range buildPlan.MetadataRows {
				plan.DeleteRows += rows
			}
		}
		plan.Targets = append(plan.Targets, target)
	}
	return plan, nil
}

func listBuilds(ctx context.Context, db *sql.DB) ([]buildRow, error) {
	rows, err := db.QueryContext(ctx, `
SELECT region, product, locale, build_key, build_name, state, active, discovered_at, updated_at
FROM server_builds`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var builds []buildRow
	for rows.Next() {
		var row buildRow
		var active int
		if err := rows.Scan(&row.Region, &row.Product, &row.Locale, &row.BuildKey, &row.BuildName, &row.State, &active, &row.discoveredAt, &row.updatedAt); err != nil {
			return nil, err
		}
		row.Active = active != 0
		builds = append(builds, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return builds, nil
}

func sortBuildsLatestFirst(builds []buildRow) {
	sort.Slice(builds, func(i, j int) bool {
		if builds[i].discoveredAt != builds[j].discoveredAt {
			return builds[i].discoveredAt > builds[j].discoveredAt
		}
		if builds[i].updatedAt != builds[j].updatedAt {
			return builds[i].updatedAt > builds[j].updatedAt
		}
		if builds[i].Active != builds[j].Active {
			return builds[i].Active
		}
		return builds[i].BuildKey > builds[j].BuildKey
	})
}

func keepReason(build Build, latestBuildKey string) string {
	switch {
	case build.Active:
		return "active"
	case build.State == metadata.StatePreparing:
		return "preparing"
	case build.BuildKey == latestBuildKey:
		return "latest"
	default:
		return ""
	}
}

func planDeletedBuild(ctx context.Context, db *sql.DB, roots Roots, build Build, protectedRaw map[buildTriple]bool) (BuildPlan, error) {
	plan := BuildPlan{Build: build, MetadataRows: map[string]int64{}}
	for _, table := range buildScopedTables {
		rows, err := countRows(ctx, db, table, build)
		if err != nil {
			return BuildPlan{}, err
		}
		if rows > 0 {
			plan.MetadataRows[table] = rows
		}
	}

	seen := map[string]bool{}
	addPath := func(kind, path, root string) {
		pathPlan := newPathPlan(kind, path, root)
		if pathPlan.Path == "" || seen[pathPlan.Path] {
			return
		}
		seen[pathPlan.Path] = true
		plan.Paths = append(plan.Paths, pathPlan)
	}

	parquetPaths, err := distinctColumnPaths(ctx, db, "server_materialized_tables", "parquet_path", build)
	if err != nil {
		return BuildPlan{}, err
	}
	for _, path := range parquetPaths {
		addPath("parquet", resolvePath(path, roots.CacheRoot), roots.CacheRoot)
	}

	artifactPaths, err := distinctColumnPaths(ctx, db, "server_artifacts", "artifact_path", build)
	if err != nil {
		return BuildPlan{}, err
	}
	for _, path := range artifactPaths {
		addPath("artifact", resolvePathWithRoot(path, roots.ArtifactRoot), roots.ArtifactRoot)
	}

	if roots.DB2Dir != "" {
		addPath("db2-build-locale-dir", filepath.Join(roots.DB2Dir, build.Region, build.Product, build.BuildKey, build.Locale), roots.DB2Dir)
	}
	if roots.RawDir != "" && !protectedRaw[buildTriple{region: build.Region, product: build.Product, buildKey: build.BuildKey}] {
		addPath("raw-casc-build-dir", filepath.Join(roots.RawDir, "casc", build.Region, build.Product, build.BuildKey), roots.RawDir)
	}
	return plan, nil
}

func countRows(ctx context.Context, db *sql.DB, table string, build Build) (int64, error) {
	exists, err := tableExists(ctx, db, table)
	if err != nil || !exists {
		return 0, err
	}
	var count int64
	err = db.QueryRowContext(ctx, fmt.Sprintf(`
SELECT COUNT(*) FROM %s
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`, table),
		build.Region, build.Product, build.Locale, build.BuildKey,
	).Scan(&count)
	return count, err
}

func distinctColumnPaths(ctx context.Context, db *sql.DB, table, column string, build Build) ([]string, error) {
	exists, err := tableExists(ctx, db, table)
	if err != nil || !exists {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
SELECT DISTINCT %s FROM %s
WHERE region = ? AND product = ? AND locale = ? AND build_key = ? AND %s <> ''`, column, table, column),
		build.Region, build.Product, build.Locale, build.BuildKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}

func tableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func resolvePath(path, cacheRoot string) string {
	return resolvePathWithRoot(path, cacheRoot)
}

func resolvePathWithRoot(path, root string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if root == "" {
		return ""
	}
	return filepath.Join(root, filepath.FromSlash(path))
}

func newPathPlan(kind, path, root string) PathPlan {
	plan := PathPlan{Kind: kind, Path: path}
	if path == "" {
		plan.SkipReason = "empty path"
		return plan
	}
	plan.Path = filepath.Clean(path)
	if root == "" {
		plan.SkipReason = "empty root"
		return plan
	}
	safe, err := insideRoot(root, path)
	if err != nil {
		plan.SkipReason = err.Error()
		return plan
	}
	plan.Safe = safe
	if !safe {
		plan.SkipReason = "path escapes configured root"
		return plan
	}
	exists, bytes, err := pathStats(path)
	if err != nil {
		plan.SkipReason = err.Error()
		return plan
	}
	plan.Exists = exists
	plan.Bytes = bytes
	return plan
}

func pathStats(path string) (bool, int64, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if !info.IsDir() {
		return true, info.Size(), nil
	}
	var bytes int64
	err = filepath.WalkDir(path, func(entryPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() {
			bytes += info.Size()
		}
		return nil
	})
	return true, bytes, err
}

func insideRoot(root, path string) (bool, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(filepath.Clean(rootAbs), filepath.Clean(pathAbs))
	if err != nil {
		return false, err
	}
	return rel == "." || (rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))), nil
}

func applyPlan(ctx context.Context, db *sql.DB, plan Plan, writer io.Writer) (Result, error) {
	result := Result{Plan: plan, Applied: true}
	for _, target := range plan.Targets {
		for _, buildPlan := range target.Delete {
			deletable, err := deletableNow(ctx, db, buildPlan.Build)
			if err != nil {
				return result, err
			}
			if !deletable {
				fmt.Fprintf(writerOrDiscard(writer), "wowdata-server cache gc skip build now protected: %s/%s/%s build=%s\n", buildPlan.Build.Region, buildPlan.Build.Product, buildPlan.Build.Locale, buildPlan.Build.BuildKey)
				continue
			}
			rows, err := deleteBuildMetadata(ctx, db, buildPlan.Build)
			if err != nil {
				return result, err
			}
			if rows == 0 {
				continue
			}
			result.DeletedRows += rows
			for _, path := range sortedPathsForDelete(buildPlan.Paths) {
				if !path.Safe || !path.Exists {
					if !path.Safe {
						result.SkippedPaths++
					}
					continue
				}
				if err := os.RemoveAll(path.Path); err != nil {
					return result, fmt.Errorf("remove %s %s: %w", path.Kind, path.Path, err)
				}
				result.DeletedPaths++
				result.DeletedBytes += path.Bytes
			}
			if err := cleanupEmptyParents(buildPlan.Build, planRootHints(buildPlan.Paths)); err != nil {
				return result, err
			}
		}
	}
	fmt.Fprintf(writerOrDiscard(writer), "wowdata-server cache gc applied: builds=%d rows=%d paths=%d bytes=%d skipped_paths=%d\n", plan.DeleteBuilds, result.DeletedRows, result.DeletedPaths, result.DeletedBytes, result.SkippedPaths)
	return result, nil
}

func sortedPathsForDelete(paths []PathPlan) []PathPlan {
	copied := append([]PathPlan(nil), paths...)
	sort.SliceStable(copied, func(i, j int) bool {
		return strings.Count(copied[i].Path, string(filepath.Separator)) > strings.Count(copied[j].Path, string(filepath.Separator))
	})
	return copied
}

func deletableNow(ctx context.Context, db *sql.DB, build Build) (bool, error) {
	builds, err := listBuilds(ctx, db)
	if err != nil {
		return false, err
	}
	var targetBuilds []buildRow
	for _, row := range builds {
		if row.Region == build.Region && row.Product == build.Product && row.Locale == build.Locale {
			targetBuilds = append(targetBuilds, row)
		}
	}
	if len(targetBuilds) == 0 {
		return false, nil
	}
	sortBuildsLatestFirst(targetBuilds)
	for _, row := range targetBuilds {
		if row.BuildKey != build.BuildKey {
			continue
		}
		return keepReason(row.Build, targetBuilds[0].BuildKey) == "", nil
	}
	return false, nil
}

func deleteBuildMetadata(ctx context.Context, db *sql.DB, build Build) (int64, error) {
	var deleted int64
	err := sqlitewrite.Do(ctx, func() error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() {
			if err != nil {
				_ = tx.Rollback()
			}
		}()

		deletable, checkErr := deletableNowTx(ctx, tx, build)
		if checkErr != nil {
			err = checkErr
			return err
		}
		if !deletable {
			err = fmt.Errorf("build became protected before metadata delete: %s/%s/%s build=%s", build.Region, build.Product, build.Locale, build.BuildKey)
			return err
		}
		for _, table := range buildScopedTables {
			exists, existsErr := tableExistsTx(ctx, tx, table)
			if existsErr != nil {
				err = existsErr
				return err
			}
			if !exists {
				continue
			}
			res, execErr := tx.ExecContext(ctx, fmt.Sprintf(`
DELETE FROM %s
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`, table),
				build.Region, build.Product, build.Locale, build.BuildKey,
			)
			if execErr != nil {
				err = execErr
				return err
			}
			n, _ := res.RowsAffected()
			deleted += n
		}
		err = tx.Commit()
		return err
	})
	return deleted, err
}

func deletableNowTx(ctx context.Context, tx *sql.Tx, build Build) (bool, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT region, product, locale, build_key, build_name, state, active, discovered_at, updated_at
FROM server_builds
WHERE region = ? AND product = ? AND locale = ?`,
		build.Region, build.Product, build.Locale,
	)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	var builds []buildRow
	for rows.Next() {
		var row buildRow
		var active int
		if err := rows.Scan(&row.Region, &row.Product, &row.Locale, &row.BuildKey, &row.BuildName, &row.State, &active, &row.discoveredAt, &row.updatedAt); err != nil {
			return false, err
		}
		row.Active = active != 0
		builds = append(builds, row)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(builds) == 0 {
		return false, nil
	}
	sortBuildsLatestFirst(builds)
	for _, row := range builds {
		if row.BuildKey == build.BuildKey {
			return keepReason(row.Build, builds[0].BuildKey) == "", nil
		}
	}
	return false, nil
}

func tableExistsTx(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func planRootHints(paths []PathPlan) []string {
	var roots []string
	for _, path := range paths {
		if path.Kind == "db2-build-locale-dir" || path.Kind == "raw-casc-build-dir" {
			roots = append(roots, filepath.Dir(path.Path))
		}
	}
	return roots
}

func cleanupEmptyParents(build Build, parents []string) error {
	for _, parent := range parents {
		if parent == "" {
			continue
		}
		if err := os.Remove(parent); err != nil && !errors.Is(err, os.ErrNotExist) && !isDirectoryNotEmpty(err) {
			return fmt.Errorf("remove empty parent %s for %s/%s/%s build=%s: %w", parent, build.Region, build.Product, build.Locale, build.BuildKey, err)
		}
	}
	return nil
}

func isDirectoryNotEmpty(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "directory not empty") ||
		strings.Contains(strings.ToLower(err.Error()), "the directory is not empty")
}

func writePlan(writer io.Writer, plan Plan, apply bool) {
	w := writerOrDiscard(writer)
	mode := "dry-run"
	if apply {
		mode = "apply"
	}
	fmt.Fprintf(w, "wowdata-server cache gc plan: mode=%s targets=%d delete_builds=%d delete_rows=%d delete_paths=%d delete_bytes=%d\n", mode, len(plan.Targets), plan.DeleteBuilds, plan.DeleteRows, plan.DeletePaths, plan.DeleteBytes)
	for _, target := range plan.Targets {
		fmt.Fprintf(w, "target %s/%s/%s keep=%d delete=%d\n", target.Region, target.Product, target.Locale, len(target.Keep), len(target.Delete))
		for _, keep := range target.Keep {
			fmt.Fprintf(w, "  keep build=%s state=%s active=%t reason=%s\n", keep.Build.BuildKey, keep.Build.State, keep.Build.Active, keep.KeepReason)
		}
		for _, del := range target.Delete {
			var rows int64
			for _, n := range del.MetadataRows {
				rows += n
			}
			fmt.Fprintf(w, "  delete build=%s state=%s rows=%d paths=%d\n", del.Build.BuildKey, del.Build.State, rows, len(del.Paths))
			for _, path := range del.Paths {
				if path.Safe {
					fmt.Fprintf(w, "    path kind=%s exists=%t bytes=%d %s\n", path.Kind, path.Exists, path.Bytes, path.Path)
				} else {
					fmt.Fprintf(w, "    skip path kind=%s reason=%s %s\n", path.Kind, path.SkipReason, path.Path)
				}
			}
		}
	}
	if !apply {
		fmt.Fprintln(w, "wowdata-server cache gc dry-run: no metadata or files deleted; rerun with --apply to delete")
	}
}

func writerOrDiscard(writer io.Writer) io.Writer {
	if writer == nil {
		return io.Discard
	}
	return writer
}

func RunFromConfig(ctx context.Context, cfg config.Config, db *sql.DB, apply bool, writer io.Writer) (Result, error) {
	return Run(ctx, Options{
		DB:     db,
		Roots:  RootsFromConfig(cfg),
		Apply:  apply,
		Writer: writer,
	})
}
