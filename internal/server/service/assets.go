package service

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	"wowdata/internal/server/storage/artifacts"
	"wowdata/internal/server/storage/cascindex"
	"wowdata/internal/server/storage/listfile"
	"wowdata/internal/server/storage/metadata"
	"wowdata/internal/server/storage/rawcache"
	"wowdata/internal/shared/export"
)

type AssetRecord struct {
	FileDataID  uint32 `json:"fileDataID,omitempty"`
	Filename    string `json:"filename,omitempty"`
	Path        string `json:"path,omitempty"`
	URI         string `json:"uri,omitempty"`
	DownloadURL string `json:"downloadUrl,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
	Size        int64  `json:"size,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
}

type FileLookupRequest struct {
	FileDataID uint32
	Filename   string
}

type FileExistsRequest struct {
	Context    RequestContext
	FileDataID uint32
	Filename   string
}

type FileExportRequest struct {
	Context    RequestContext
	FileDataID uint32
	Filename   string
	MIMEType   string
}

type IconExportRequest struct {
	Context    RequestContext
	FileDataID uint32
	Format     string
	Mipmap     int
	Mask       int
}

type FileExistsResult struct {
	FileDataID uint32 `json:"fileDataID,omitempty"`
	Filename   string `json:"filename,omitempty"`
	Exists     bool   `json:"exists"`
}

type AssetService interface {
	FileLookup(context.Context, FileLookupRequest) (AssetRecord, error)
	FileExists(context.Context, FileExistsRequest) (FileExistsResult, error)
	FileExport(context.Context, FileExportRequest) (AssetRecord, error)
	IconExport(context.Context, IconExportRequest) (AssetRecord, error)
}

type ContextFetchFunc func(ctx context.Context, reqCtx RequestContext, encodingKey string) ([]byte, error)

type ServerAssetService struct {
	db           *sql.DB
	rawCache     *rawcache.Cache
	artifacts    *artifacts.Store
	fetch        rawcache.FetchFunc
	contextFetch ContextFetchFunc
}

func NewAssetServiceForTest(db *sql.DB, raw *rawcache.Cache, store *artifacts.Store, fetch rawcache.FetchFunc) *ServerAssetService {
	return &ServerAssetService{db: db, rawCache: raw, artifacts: store, fetch: fetch}
}

func NewAssetService(db *sql.DB, raw *rawcache.Cache, store *artifacts.Store, fetch rawcache.FetchFunc) *ServerAssetService {
	return &ServerAssetService{db: db, rawCache: raw, artifacts: store, fetch: fetch}
}

func NewAssetServiceWithContextFetch(db *sql.DB, raw *rawcache.Cache, store *artifacts.Store, fetch ContextFetchFunc) *ServerAssetService {
	return &ServerAssetService{db: db, rawCache: raw, artifacts: store, contextFetch: fetch}
}

func (s *ServerAssetService) FileLookup(ctx context.Context, req FileLookupRequest) (AssetRecord, error) {
	entry, err := s.lookupEntry(ctx, req.FileDataID, req.Filename)
	if err != nil {
		return AssetRecord{}, err
	}
	return AssetRecord{FileDataID: entry.FileDataID, Filename: entry.Path}, nil
}

func (s *ServerAssetService) FileExists(ctx context.Context, req FileExistsRequest) (FileExistsResult, error) {
	fileDataID := req.FileDataID
	filename := req.Filename
	if fileDataID == 0 && filename != "" {
		entry, err := listfile.LookupByFilename(ctx, s.db, filename)
		if err != nil {
			return FileExistsResult{Filename: filename, Exists: false}, nil
		}
		fileDataID = entry.FileDataID
		filename = entry.Path
	}
	if fileDataID == 0 {
		return FileExistsResult{Filename: filename, Exists: false}, nil
	}
	resolvedCtx, err := s.resolveContext(ctx, req.Context)
	if err != nil {
		return FileExistsResult{FileDataID: fileDataID, Filename: filename, Exists: false}, err
	}
	_, err = cascindex.ResolveFileDataIDForSource(ctx, s.db, cascSourceKey(resolvedCtx), fileDataID)
	return FileExistsResult{FileDataID: fileDataID, Filename: filename, Exists: err == nil}, nil
}

func (s *ServerAssetService) FileExport(ctx context.Context, req FileExportRequest) (AssetRecord, error) {
	fileDataID := req.FileDataID
	filename := req.Filename
	if fileDataID == 0 || filename == "" {
		entry, err := s.lookupEntry(ctx, fileDataID, filename)
		if err != nil {
			return AssetRecord{}, err
		}
		fileDataID = entry.FileDataID
		filename = entry.Path
	}
	body, err := s.readRawFile(ctx, req.Context, fileDataID)
	if err != nil {
		return AssetRecord{}, err
	}
	mimeType := req.MIMEType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	name := filepath.Base(filepath.ToSlash(filename))
	if name == "." || name == "" {
		name = fmt.Sprintf("%d.bin", fileDataID)
	}
	record, err := s.artifacts.StoreBytes(ctx, artifacts.RequestContext{
		Region: req.Context.Region, Product: req.Context.Product, Locale: req.Context.Locale, BuildKey: req.Context.BuildKey,
	}, "files", name, mimeType, body)
	if err != nil {
		return AssetRecord{}, err
	}
	return assetRecordFromArtifact(fileDataID, filename, record), nil
}

func (s *ServerAssetService) IconExport(ctx context.Context, req IconExportRequest) (AssetRecord, error) {
	body, err := s.readRawFile(ctx, req.Context, req.FileDataID)
	if err != nil {
		return AssetRecord{}, err
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "png"
	}
	mimeType := "image/" + format
	path, record, err := s.artifacts.Reserve("icons", fmt.Sprintf("%d.%s", req.FileDataID, format), mimeType)
	if err != nil {
		return AssetRecord{}, err
	}
	if _, err := export.ExportIconWithOptions(body, path, format, req.Mipmap, req.Mask); err != nil {
		return AssetRecord{}, err
	}
	record, err = s.artifacts.RecordExisting(ctx, artifacts.RequestContext{
		Region: req.Context.Region, Product: req.Context.Product, Locale: req.Context.Locale, BuildKey: req.Context.BuildKey,
	}, path, mimeType)
	if err != nil {
		return AssetRecord{}, err
	}
	return assetRecordFromArtifact(req.FileDataID, "", record), nil
}

func (s *ServerAssetService) lookupEntry(ctx context.Context, fileDataID uint32, filename string) (listfile.Entry, error) {
	if s == nil || s.db == nil {
		return listfile.Entry{}, fmt.Errorf("asset metadata db is unavailable")
	}
	if fileDataID != 0 {
		return listfile.LookupByFileDataID(ctx, s.db, fileDataID)
	}
	if filename != "" {
		return listfile.LookupByFilename(ctx, s.db, filename)
	}
	return listfile.Entry{}, fmt.Errorf("fileDataID or filename is required")
}

func (s *ServerAssetService) readRawFile(ctx context.Context, reqCtx RequestContext, fileDataID uint32) ([]byte, error) {
	if s == nil || s.db == nil || s.rawCache == nil {
		return nil, fmt.Errorf("server asset storage is unavailable")
	}
	reqCtx, err := s.resolveContext(ctx, reqCtx)
	if err != nil {
		return nil, err
	}
	span, err := cascindex.ResolveFileDataIDForSource(ctx, s.db, cascSourceKey(reqCtx), fileDataID)
	if err != nil {
		return nil, err
	}
	expectedSHA256 := expectedRawSHA256(span.EncodingKey)
	fetch := s.fetch
	if s.contextFetch != nil {
		fetch = func(ctx context.Context, encodingKey string) ([]byte, error) {
			return s.contextFetch(ctx, reqCtx, encodingKey)
		}
	}
	return s.rawCache.Get(ctx, reqCtx.Region, reqCtx.Product, reqCtx.BuildKey, span.EncodingKey, expectedSHA256, fetch)
}

func (s *ServerAssetService) resolveContext(ctx context.Context, reqCtx RequestContext) (RequestContext, error) {
	if reqCtx.BuildKey != "" {
		return reqCtx, nil
	}
	active, err := metadata.ActiveBuild(ctx, s.db, reqCtx.Region, reqCtx.Product, reqCtx.Locale)
	if err != nil {
		return reqCtx, fmt.Errorf("resolve active build for asset request: %w", err)
	}
	reqCtx.BuildKey = active.Key.BuildKey
	return reqCtx, nil
}

func expectedRawSHA256(encodingKey string) string {
	key := strings.ToLower(strings.TrimSpace(encodingKey))
	if len(key) == 64 {
		return key
	}
	return ""
}

func cascSourceKey(reqCtx RequestContext) cascindex.SourceKey {
	return cascindex.SourceKey{
		Region:   reqCtx.Region,
		Product:  reqCtx.Product,
		Locale:   reqCtx.Locale,
		BuildKey: reqCtx.BuildKey,
	}
}

func assetRecordFromArtifact(fileDataID uint32, filename string, record artifacts.Record) AssetRecord {
	return AssetRecord{
		FileDataID:  fileDataID,
		Filename:    filename,
		Path:        record.Path,
		URI:         recordURI(record),
		DownloadURL: record.DownloadURL,
		MIMEType:    record.MIMEType,
		Size:        record.Size,
		SHA256:      record.SHA256,
	}
}

func recordURI(record artifacts.Record) string {
	if record.DownloadURL != "" {
		return record.DownloadURL
	}
	return record.URI
}
