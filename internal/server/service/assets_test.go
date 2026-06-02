package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wowdata/internal/server/storage/artifacts"
	"wowdata/internal/server/storage/cascindex"
	"wowdata/internal/server/storage/listfile"
	"wowdata/internal/server/storage/rawcache"
)

func TestServerFileLookupUsesSQLiteListfileIndex(t *testing.T) {
	ctx := context.Background()
	db := openMetadataQueryTestDB(t)
	if err := listfile.ReplaceSource(ctx, db, "source-a", []listfile.Entry{{FileDataID: 100, Path: "Interface/Icons/Test.blp"}}); err != nil {
		t.Fatalf("seed listfile: %v", err)
	}

	got, err := NewAssetServiceForTest(db, nil, nil, nil).FileLookup(ctx, FileLookupRequest{FileDataID: 100})
	if err != nil {
		t.Fatalf("FileLookup: %v", err)
	}
	if got.FileDataID != 100 || got.Filename != "interface/icons/test.blp" {
		t.Fatalf("lookup = %#v", got)
	}
}

func TestServerFileExistsUsesCASCIndexWithoutListfileWhenFileDataIDProvided(t *testing.T) {
	ctx := context.Background()
	db := openMetadataQueryTestDB(t)
	if err := cascindex.ReplaceIndexForSource(ctx, db, cascindex.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"}, "casc-a",
		[]cascindex.RootMapping{{FileDataID: 200, ContentKey: "content"}},
		[]cascindex.EncodingMapping{{ContentKey: "content", EncodingKey: strings.Repeat("a", 64), Size: 4}},
		[]cascindex.ArchiveMapping{{EncodingKey: strings.Repeat("a", 64), ArchiveKey: "archive", Offset: 0, Size: 4}},
	); err != nil {
		t.Fatalf("seed casc index: %v", err)
	}

	exists, err := NewAssetServiceForTest(db, nil, nil, nil).FileExists(ctx, FileExistsRequest{
		Context:    RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"},
		FileDataID: 200,
	})
	if err != nil {
		t.Fatalf("FileExists: %v", err)
	}
	if !exists.Exists {
		t.Fatalf("exists = %#v, want true", exists)
	}
}

func TestServerFileExportReadsRawCacheAndStoresArtifact(t *testing.T) {
	ctx := context.Background()
	db := openMetadataQueryTestDB(t)
	body := []byte("hello file")
	hash := sha256HexForAssetTest(body)
	if err := listfile.ReplaceSource(ctx, db, "source-a", []listfile.Entry{{FileDataID: 300, Path: "Files/Test.bin"}}); err != nil {
		t.Fatalf("seed listfile: %v", err)
	}
	if err := cascindex.ReplaceIndexForSource(ctx, db, cascindex.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"}, "casc-a",
		[]cascindex.RootMapping{{FileDataID: 300, ContentKey: "content"}},
		[]cascindex.EncodingMapping{{ContentKey: "content", EncodingKey: hash, Size: int64(len(body))}},
		[]cascindex.ArchiveMapping{{EncodingKey: hash, ArchiveKey: "archive", Offset: 0, Size: int64(len(body))}},
	); err != nil {
		t.Fatalf("seed casc index: %v", err)
	}
	artifactRoot := t.TempDir()
	svc := NewAssetServiceForTest(db, rawcache.New(t.TempDir()), artifacts.NewStore(artifacts.Config{
		Root:    artifactRoot,
		BaseURL: "http://example.test/files",
	}, db), func(ctx context.Context, encodingKey string) ([]byte, error) {
		return body, nil
	})

	got, err := svc.FileExport(ctx, FileExportRequest{
		Context:    RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"},
		FileDataID: 300,
	})
	if err != nil {
		t.Fatalf("FileExport: %v", err)
	}
	if got.DownloadURL == "" || got.Size != int64(len(body)) || got.SHA256 != hash {
		t.Fatalf("export = %#v", got)
	}
	if _, err := os.Stat(got.Path); err != nil {
		t.Fatalf("artifact not written: %v", err)
	}
}

func TestServerFileExportResolvesCASCIndexForRequestBuild(t *testing.T) {
	ctx := context.Background()
	db := openMetadataQueryTestDB(t)
	bodyUS := []byte("us file")
	bodyCN := []byte("cn file")
	hashUS := sha256HexForAssetTest(bodyUS)
	hashCN := sha256HexForAssetTest(bodyCN)
	us := cascindex.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-us"}
	cn := cascindex.SourceKey{Region: "cn", Product: "wow", Locale: "zhCN", BuildKey: "build-cn"}
	if err := listfile.ReplaceSource(ctx, db, "source-a", []listfile.Entry{{FileDataID: 302, Path: "Files/Scoped.bin"}}); err != nil {
		t.Fatalf("seed listfile: %v", err)
	}
	if err := cascindex.ReplaceIndexForSource(ctx, db, us, "source-us",
		[]cascindex.RootMapping{{FileDataID: 302, ContentKey: "content-us"}},
		[]cascindex.EncodingMapping{{ContentKey: "content-us", EncodingKey: hashUS, Size: int64(len(bodyUS))}},
		[]cascindex.ArchiveMapping{{EncodingKey: hashUS, ArchiveKey: "archive-us", Offset: 0, Size: int64(len(bodyUS))}},
	); err != nil {
		t.Fatalf("seed us casc index: %v", err)
	}
	if err := cascindex.ReplaceIndexForSource(ctx, db, cn, "source-cn",
		[]cascindex.RootMapping{{FileDataID: 302, ContentKey: "content-cn"}},
		[]cascindex.EncodingMapping{{ContentKey: "content-cn", EncodingKey: hashCN, Size: int64(len(bodyCN))}},
		[]cascindex.ArchiveMapping{{EncodingKey: hashCN, ArchiveKey: "archive-cn", Offset: 0, Size: int64(len(bodyCN))}},
	); err != nil {
		t.Fatalf("seed cn casc index: %v", err)
	}
	fetches := map[string][]byte{hashUS: bodyUS, hashCN: bodyCN}
	svc := NewAssetServiceForTest(db, rawcache.New(t.TempDir()), artifacts.NewStore(artifacts.Config{
		Root:    t.TempDir(),
		BaseURL: "http://example.test/files",
	}, db), func(ctx context.Context, encodingKey string) ([]byte, error) {
		body, ok := fetches[encodingKey]
		if !ok {
			return nil, fmt.Errorf("unexpected encoding key %s", encodingKey)
		}
		return body, nil
	})

	got, err := svc.FileExport(ctx, FileExportRequest{
		Context:    RequestContext{Region: "cn", Product: "wow", Locale: "zhCN", BuildKey: "build-cn"},
		FileDataID: 302,
	})
	if err != nil {
		t.Fatalf("FileExport: %v", err)
	}
	if got.SHA256 != hashCN || got.Size != int64(len(bodyCN)) {
		t.Fatalf("export = %#v, want CN blob", got)
	}
}

func TestServerFileExportUsesCachedRawBlobWithoutFetcher(t *testing.T) {
	ctx := context.Background()
	db := openMetadataQueryTestDB(t)
	body := []byte("cached file")
	hash := sha256HexForAssetTest(body)
	if err := listfile.ReplaceSource(ctx, db, "source-a", []listfile.Entry{{FileDataID: 301, Path: "Files/Cached.bin"}}); err != nil {
		t.Fatalf("seed listfile: %v", err)
	}
	if err := cascindex.ReplaceIndexForSource(ctx, db, cascindex.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"}, "casc-a",
		[]cascindex.RootMapping{{FileDataID: 301, ContentKey: "content"}},
		[]cascindex.EncodingMapping{{ContentKey: "content", EncodingKey: hash, Size: int64(len(body))}},
		[]cascindex.ArchiveMapping{{EncodingKey: hash, ArchiveKey: "archive", Offset: 0, Size: int64(len(body))}},
	); err != nil {
		t.Fatalf("seed casc index: %v", err)
	}
	rawRoot := t.TempDir()
	cachePath, err := rawcache.Path(rawRoot, "us", "wow", "active-build", hash)
	if err != nil {
		t.Fatalf("raw cache path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatalf("mkdir raw cache: %v", err)
	}
	if err := os.WriteFile(cachePath, body, 0644); err != nil {
		t.Fatalf("write raw cache: %v", err)
	}
	svc := NewAssetServiceForTest(db, rawcache.New(rawRoot), artifacts.NewStore(artifacts.Config{
		Root:    t.TempDir(),
		BaseURL: "http://example.test/files",
	}, db), nil)

	got, err := svc.FileExport(ctx, FileExportRequest{
		Context:    RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"},
		FileDataID: 301,
	})
	if err != nil {
		t.Fatalf("FileExport: %v", err)
	}
	if got.Size != int64(len(body)) || got.DownloadURL == "" {
		t.Fatalf("export = %#v", got)
	}
}

func TestServerIconExportConvertsBLPAndStoresDownloadableArtifact(t *testing.T) {
	ctx := context.Background()
	db := openMetadataQueryTestDB(t)
	body := buildAssetTestBLP(4, 4)
	hash := sha256HexForAssetTest(body)
	if err := cascindex.ReplaceIndexForSource(ctx, db, cascindex.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"}, "casc-a",
		[]cascindex.RootMapping{{FileDataID: 400, ContentKey: "content"}},
		[]cascindex.EncodingMapping{{ContentKey: "content", EncodingKey: hash, Size: int64(len(body))}},
		[]cascindex.ArchiveMapping{{EncodingKey: hash, ArchiveKey: "archive", Offset: 0, Size: int64(len(body))}},
	); err != nil {
		t.Fatalf("seed casc index: %v", err)
	}
	svc := NewAssetServiceForTest(db, rawcache.New(t.TempDir()), artifacts.NewStore(artifacts.Config{
		Root:    t.TempDir(),
		BaseURL: "http://example.test/files",
	}, db), func(ctx context.Context, encodingKey string) ([]byte, error) {
		return body, nil
	})

	got, err := svc.IconExport(ctx, IconExportRequest{
		Context:    RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"},
		FileDataID: 400,
		Format:     "png",
	})
	if err != nil {
		t.Fatalf("IconExport: %v", err)
	}
	if got.DownloadURL == "" || got.MIMEType != "image/png" || got.Size <= 0 || got.SHA256 == "" {
		t.Fatalf("icon export = %#v", got)
	}
	if _, err := os.Stat(got.Path); err != nil {
		t.Fatalf("icon artifact not written: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM server_artifacts WHERE artifact_path = ?`, filepath.ToSlash(got.Path)).Scan(&count); err != nil {
		t.Fatalf("query icon artifact metadata: %v", err)
	}
	if count != 1 {
		t.Fatalf("icon artifact metadata rows = %d, want 1", count)
	}
}

func TestServerAssetServiceDoesNotImportLocalRuntime(t *testing.T) {
	source, err := os.ReadFile("assets.go")
	if err != nil {
		t.Fatalf("read assets.go: %v", err)
	}
	if strings.Contains(string(source), "internal/local") || strings.Contains(string(source), "internal/service/http") {
		t.Fatalf("server asset service imports local runtime:\n%s", source)
	}
}

func sha256HexForAssetTest(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum[:])
}

func buildAssetTestBLP(width, height int) []byte {
	dataOfs := 148 + 256*4
	data := make([]byte, dataOfs+width*height*4)
	binary.LittleEndian.PutUint32(data, 0x32504C42)
	data[4] = 3
	data[7] = 1
	binary.LittleEndian.PutUint32(data[8:], 1)
	binary.LittleEndian.PutUint32(data[12:], uint32(width))
	binary.LittleEndian.PutUint32(data[16:], uint32(height))
	binary.LittleEndian.PutUint32(data[20:], uint32(dataOfs))
	binary.LittleEndian.PutUint32(data[84:], uint32(width*height*4))
	for ofs := dataOfs; ofs < len(data); ofs += 4 {
		data[ofs] = 0xBB
		data[ofs+1] = 0xCC
		data[ofs+2] = 0xAA
		data[ofs+3] = 0xFF
	}
	return data
}

func TestAssetRecordPathStaysUnderRoot(t *testing.T) {
	root := t.TempDir()
	record := AssetRecord{Path: filepath.Join(root, "files", "a.bin")}
	if !strings.HasPrefix(record.Path, root) {
		t.Fatalf("record path = %q, want under %q", record.Path, root)
	}
}
