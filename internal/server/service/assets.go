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

type ServerAssetService struct {
	db        *sql.DB
	rawCache  *rawcache.Cache
	artifacts *artifacts.Store
	fetch     rawcache.FetchFunc
}

func NewAssetServiceForTest(db *sql.DB, raw *rawcache.Cache, store *artifacts.Store, fetch rawcache.FetchFunc) *ServerAssetService {
	return &ServerAssetService{db: db, rawCache: raw, artifacts: store, fetch: fetch}
}

func NewAssetService(db *sql.DB, raw *rawcache.Cache, store *artifacts.Store, fetch rawcache.FetchFunc) *ServerAssetService {
	return &ServerAssetService{db: db, rawCache: raw, artifacts: store, fetch: fetch}
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
	_, err := cascindex.ResolveFileDataID(ctx, s.db, fileDataID)
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
	span, err := cascindex.ResolveFileDataID(ctx, s.db, fileDataID)
	if err != nil {
		return nil, err
	}
	expectedSHA256 := strings.ToLower(span.EncodingKey)
	return s.rawCache.Get(ctx, reqCtx.Region, reqCtx.Product, reqCtx.BuildKey, span.EncodingKey, expectedSHA256, s.fetch)
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
