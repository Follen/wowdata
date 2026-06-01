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
	db, err := metadata.Open(cfg.Cache.MetadataDB, sqliteMigrationsDir())
	if err != nil {
		return nil, nil, err
	}
	materializer := &httpservice.DB2Materializer{
		CacheRoot:           cfg.Cache.Root,
		MetadataDB:          db,
		Resolver:            runtimeContextResolver{rt: rt, cfg: cfg},
		Loader:              runtimeTableLoader{rt: rt},
		MaterializerVersion: "http-materializer-v1",
	}
	svc := httpservice.NewService(cfg, materializer)
	svc.SetMetadataDB(db)
	return svc, func() { _ = db.Close() }, nil
}

type runtimeContextResolver struct {
	rt  *Runtime
	cfg config.HTTPConfig
}

func (r runtimeContextResolver) ResolveContext(ctx context.Context, rc httpservice.RequestContext) (*appruntime.Context, error) {
	if r.rt == nil {
		r.rt = NewRuntime()
	}
	resolved := httpservice.NewService(r.cfg, nil).ResolveRequestContext(rc)
	if _, err := r.rt.initialize(warmupOptions{
		Source:          "remote",
		Region:          resolved.Region,
		Product:         resolved.Product,
		Locale:          resolved.Locale,
		CacheRoot:       r.cfg.Cache.Root,
		WarmDBDManifest: true,
	}); err != nil {
		return nil, err
	}
	r.rt.mu.Lock()
	defer r.rt.mu.Unlock()
	if r.rt.warmup == nil {
		return nil, sql.ErrNoRows
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

type runtimeTableLoader struct {
	rt *Runtime
}

func (l runtimeTableLoader) LoadDB2Table(ctx context.Context, runtimeCtx *appruntime.Context, table string) (httpservice.LoadedDB2Table, error) {
	if l.rt == nil {
		l.rt = NewRuntime()
	}
	if err := l.rt.warmDB2Tables(runtimeCtx.Product, []string{table}); err != nil {
		return httpservice.LoadedDB2Table{}, err
	}
	l.rt.markWarmupTables([]string{table})
	l.rt.mu.Lock()
	store := l.rt.DB2
	l.rt.mu.Unlock()
	manifest, err := l.rt.loadDBDManifest()
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
	dbdSource := appruntime.NewHTTPDBDSource(l.rt.dbdCacheDir(), []string{
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
		fields = append(fields, cacheparquet.Field{Name: field.Name, Type: field.Type})
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
