package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"wowdata/internal/cache/metadata"
	cacheparquet "wowdata/internal/cache/parquet"
	"wowdata/internal/config"
	appruntime "wowdata/internal/runtime"
	httpservice "wowdata/internal/service/http"
)

func NewHTTPServiceRuntime(cfg config.HTTPConfig, rt *Runtime) (*httpservice.Service, func(), error) {
	if rt == nil {
		rt = NewRuntime()
	}
	rt.CacheRoot = cfg.Cache.Root
	db, err := metadata.Open(cfg.Cache.MetadataDB, sqliteMigrationsDir())
	if err != nil {
		return nil, nil, err
	}
	runtimeState := &httpRuntimeState{rt: rt, cfg: cfg}
	materializer := &httpservice.DB2Materializer{
		CacheRoot:           cfg.Cache.Root,
		MetadataDB:          db,
		Resolver:            runtimeState,
		Loader:              runtimeState,
		MaterializerVersion: "http-materializer-v1",
	}
	svc := httpservice.NewService(cfg, materializer)
	svc.SetMetadataDB(db)
	return svc, func() { _ = db.Close() }, nil
}

type httpRuntimeState struct {
	rt  *Runtime
	cfg config.HTTPConfig
}

func (r *httpRuntimeState) ResolveContext(ctx context.Context, rc httpservice.RequestContext) (*appruntime.Context, error) {
	if r.rt == nil {
		r.rt = NewRuntime()
	}
	r.rt.httpRuntimeMu.Lock()
	defer r.rt.httpRuntimeMu.Unlock()
	return r.resolveContextLocked(ctx, rc)
}

func (r *httpRuntimeState) resolveContextLocked(ctx context.Context, rc httpservice.RequestContext) (*appruntime.Context, error) {
	resolved := httpservice.NewService(r.cfg, nil).ResolveRequestContext(rc)
	result, err := r.rt.initialize(warmupOptions{
		Source:          "remote",
		Region:          resolved.Region,
		Product:         resolved.Product,
		Locale:          resolved.Locale,
		CacheRoot:       r.cfg.Cache.Root,
		WarmDBDManifest: true,
	})
	if err != nil {
		return nil, err
	}
	if status, _ := result["status"].(string); status == "no_build" {
		return nil, fmt.Errorf("no build found for %s/%s", resolved.Region, resolved.Product)
	}
	r.rt.mu.Lock()
	defer r.rt.mu.Unlock()
	if r.rt.warmup == nil {
		return nil, sql.ErrNoRows
	}
	if r.rt.warmup.Region != resolved.Region || r.rt.warmup.Product != resolved.Product || r.rt.warmup.Locale != resolved.Locale {
		return nil, fmt.Errorf("resolved runtime context mismatch: got %s/%s/%s, want %s/%s/%s", r.rt.warmup.Region, r.rt.warmup.Product, r.rt.warmup.Locale, resolved.Region, resolved.Product, resolved.Locale)
	}
	return &appruntime.Context{
		Source:      r.rt.warmup.Source,
		Region:      r.rt.warmup.Region,
		Product:     r.rt.warmup.Product,
		BuildName:   r.rt.warmup.BuildName,
		BuildKey:    r.rt.warmup.BuildKey,
		BuildIndex:  r.rt.warmup.BuildIndex,
		Locale:      r.rt.warmup.Locale,
		CacheRoot:   r.rt.warmup.CacheRoot,
		CASCReady:   r.rt.CASC != nil || r.rt.Local != nil,
		DBDReady:    r.rt.warmup.DBDManifest,
		Listfile:    r.rt.warmup.Listfile,
		TablesReady: cloneBoolMap(r.rt.warmup.Tables),
	}, nil
}

func (r *httpRuntimeState) LoadDB2Table(ctx context.Context, runtimeCtx *appruntime.Context, table string) (httpservice.LoadedDB2Table, error) {
	if r.rt == nil {
		r.rt = NewRuntime()
	}
	r.rt.httpRuntimeMu.Lock()
	defer r.rt.httpRuntimeMu.Unlock()
	if runtimeCtx != nil {
		if _, err := r.resolveContextLocked(ctx, httpservice.RequestContext{
			Region:  runtimeCtx.Region,
			Product: runtimeCtx.Product,
			Locale:  runtimeCtx.Locale,
		}); err != nil {
			return httpservice.LoadedDB2Table{}, err
		}
	}
	if err := r.rt.warmDB2Tables(runtimeCtx.Product, []string{table}); err != nil {
		return httpservice.LoadedDB2Table{}, err
	}
	r.rt.markWarmupTables([]string{table})
	r.rt.mu.Lock()
	store := r.rt.DB2
	r.rt.mu.Unlock()
	manifest, err := r.rt.loadDBDManifest()
	if err != nil {
		return httpservice.LoadedDB2Table{}, err
	}
	fileDataID, ok := manifest.GetByTableName(table)
	if !ok {
		return httpservice.LoadedDB2Table{}, fmt.Errorf("table not found in DBD manifest: %s", table)
	}
	schema, rowCount, err := store.Schema(table)
	if err != nil {
		return httpservice.LoadedDB2Table{}, err
	}
	rows, err := store.Rows(table, nil, nil, "", 0)
	if err != nil {
		return httpservice.LoadedDB2Table{}, err
	}
	dbdSource := appruntime.NewHTTPDBDSource(r.rt.dbdCacheDir(), []string{
		"https://raw.githubusercontent.com/wowdev/WoWDBDefs/refs/heads/master/definitions/%s.dbd",
		"https://www.kruithne.net/wow.export/data/dbd/?def=%s",
	})
	rawDBD, err := dbdSource.Definition(table)
	if err != nil {
		return httpservice.LoadedDB2Table{}, err
	}
	sum := sha256.Sum256([]byte(rawDBD))
	return httpservice.LoadedDB2Table{
		DB2FileDataID:     int(fileDataID),
		DBDDefinitionHash: hex.EncodeToString(sum[:]),
		DecoderVersion:    "runtime-db2-loader-v1",
		RowCount:          rowCount,
		Schema:            parquetFields(schema),
		Rows:              rows,
	}, nil
}

func sqliteMigrationsDir() string {
	for _, dir := range []string{
		"migrations/sqlite",
		filepath.Join("..", "..", "migrations", "sqlite"),
	} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return "migrations/sqlite"
}

func parquetFields(schema []appruntime.SchemaField) []cacheparquet.Field {
	fields := make([]cacheparquet.Field, 0, len(schema))
	for _, field := range schema {
		fields = append(fields, cacheparquet.Field{Name: field.Name, Type: field.Type, ArrayLen: field.ArrayLen})
	}
	return fields
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
