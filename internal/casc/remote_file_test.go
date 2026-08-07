package casc

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wowdata/internal/resource"
)

func TestNewCASCRemoteUsesCanonicalCNMetadataHost(t *testing.T) {
	remote := NewCASCRemote("cn")
	if remote.PatchHost != "https://cn.version.battlenet.com.cn/" {
		t.Fatalf("PatchHost=%q", remote.PatchHost)
	}
	if remote.Host != remote.PatchHost {
		t.Fatalf("Host=%q PatchHost=%q", remote.Host, remote.PatchHost)
	}
	if remote.RangeChunkSize != 4<<20 {
		t.Fatalf("RangeChunkSize=%d", remote.RangeChunkSize)
	}
}

func TestInitProductWithStageAttributesVersionPayload(t *testing.T) {
	ResetHTTPMetrics()
	resource.ResetStages()
	payload := []byte("Region!STRING:0|BuildConfig!HEX:16|CDNConfig!HEX:16|VersionsName!STRING:0|ProductConfig!HEX:16\nus|00112233445566778899aabbccddeeff|ffeeddccbbaa99887766554433221100|1.2.3.4|0123456789abcdef0123456789abcdef\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	remote := NewCASCRemote("us")
	remote.PatchHost = server.URL + "/"
	remote.CacheRoot = t.TempDir()
	stage := resource.StartStage("remote-init", resource.StageOptions{})
	err := remote.InitProductWithStage("wow", stage)
	resource.FinishStage(stage, err)
	if err != nil {
		t.Fatalf("InitProductWithStage: %v", err)
	}
	metrics := SnapshotHTTPMetrics()
	resource.ReconcileStageNetwork(uint64(metrics.UniquePayloadBytes), uint64(metrics.ResponseBytes))
	stages := resource.SnapshotStages()
	if !stages.Complete || len(stages.Work) != 1 {
		t.Fatalf("stages = %+v", stages)
	}
	if got := stages.Work[0]; got.StageID != stage || got.NetworkUniqueBytes != uint64(len(payload)) || got.NetworkTransferredBytes != uint64(len(payload)) {
		t.Fatalf("stage work = %+v", got)
	}
}

func TestPreloadMetadataWithStageStopsBeforeArchiveRootAndEncoding(t *testing.T) {
	const (
		buildKey = "00112233445566778899aabbccddeeff"
		cdnKey   = "ffeeddccbbaa99887766554433221100"
	)
	var serverURL string
	var mu sync.Mutex
	requests := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests = append(requests, request.URL.Path)
		mu.Unlock()
		switch request.URL.Path {
		case "/wow/cdns":
			fmt.Fprintf(w, "Name!STRING:0|Hosts!STRING:0|Path!STRING:0\nus|%s|\n", serverURL)
		case "/config/" + FormatCDNKey(cdnKey):
			_, _ = w.Write([]byte("# CDN Configuration\narchives = archive-a archive-b\n"))
		case "/config/" + FormatCDNKey(buildKey):
			_, _ = w.Write([]byte("# Build Configuration\nroot = root-content-key\nencoding = encoding-content-key encoding-key\n"))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	serverURL = server.URL
	defer server.Close()

	remote := NewCASCRemote("us")
	remote.PatchHost = server.URL + "/"
	remote.Host = remote.PatchHost
	remote.CacheRoot = t.TempDir()
	remote.Builds = []VersionEntry{{
		Product: "wow", Region: "us", VersionsName: "1.2.3.4",
		BuildConfig: buildKey, CDNConfig: cdnKey,
	}}

	stage := resource.StartStage("metadata-only", resource.StageOptions{})
	err := remote.PreloadMetadataWithStage(0, stage)
	resource.FinishStage(stage, err)
	if err != nil {
		t.Fatalf("PreloadMetadataWithStage: %v", err)
	}
	if remote.Build == nil || remote.Cache == nil || remote.Server == nil {
		t.Fatalf("metadata was not prepared: build=%v cache=%v server=%v", remote.Build, remote.Cache, remote.Server)
	}
	if remote.CDNConfig["archives"] == "" || remote.BuildConfig["root"] == "" {
		t.Fatalf("configs were not loaded: CDN=%v Build=%v", remote.CDNConfig, remote.BuildConfig)
	}
	if len(remote.Archives) != 0 || len(remote.RootEntries) != 0 || len(remote.EncodingEntries) != 0 {
		t.Fatalf("data catalogs loaded during metadata-only preload: archives=%d root=%d encoding=%d", len(remote.Archives), len(remote.RootEntries), len(remote.EncodingEntries))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("requests = %v, want server/CDN/Build config only", requests)
	}
	for _, path := range requests {
		if strings.Contains(path, "/data/") || strings.HasSuffix(path, ".index") {
			t.Fatalf("metadata-only preload requested data catalog %q", path)
		}
	}
}

func TestDecodeCASCData(t *testing.T) {
	raw := buildCascTestBLTE([]byte("hello casc"))
	decoded, err := DecodeCASCData(raw)
	if err != nil {
		t.Fatalf("DecodeCASCData: %v", err)
	}
	if string(decoded) != "hello casc" {
		t.Fatalf("decoded = %q", decoded)
	}
}

func TestResolveFileEncodingKey(t *testing.T) {
	c := NewCASCSource()
	c.RootEntries[10] = []RootEntry{{TypeIndex: 0, ContentKey: "content"}}
	c.RootTypes = append(c.RootTypes, RootType{LocaleFlags: LocaleEnUS})
	c.EncodingEntries["content"] = EncodingEntry{Key: "encoding"}
	c.Locale = LocaleEnUS

	contentKey, encodingKey, err := c.ResolveFileKeys(10)
	if err != nil {
		t.Fatalf("ResolveFileKeys: %v", err)
	}
	if contentKey != "content" || encodingKey != "encoding" {
		t.Fatalf("keys = %s/%s", contentKey, encodingKey)
	}
}

func TestReadFileDataUsesArchiveThenDecodes(t *testing.T) {
	remote := NewCASCRemote("us")
	remote.Cache = NewDataCache(t.TempDir(), "test-build")
	remote.RootEntries[10] = []RootEntry{{TypeIndex: 0, ContentKey: "content"}}
	remote.RootTypes = append(remote.RootTypes, RootType{LocaleFlags: LocaleEnUS})
	remote.EncodingEntries["content"] = EncodingEntry{Key: "encoding"}
	remote.Archives["encoding"] = ArchiveEntry{Key: "archive", Size: int32(len(buildCascTestBLTE([]byte("hello archive")))), Offset: 12}
	remote.Locale = LocaleEnUS

	var gotFile string
	var gotOffset int
	var gotLength int
	remote.fetchPartial = func(cdnFile string, offset, length int) ([]byte, error) {
		gotFile = cdnFile
		gotOffset = offset
		gotLength = length
		return buildCascTestBLTE([]byte("hello archive")), nil
	}

	data, err := remote.ReadFileData(10)
	if err != nil {
		t.Fatalf("ReadFileData: %v", err)
	}
	if string(data) != "hello archive" {
		t.Fatalf("data = %q", data)
	}
	if gotFile != filepath.ToSlash(FormatCDNKey("archive")) || gotOffset != 12 || gotLength != int(remote.Archives["encoding"].Size) {
		t.Fatalf("fetch = %s %d %d", gotFile, gotOffset, gotLength)
	}
	if _, err := os.Stat(remote.Cache.ObjectPath("encoding")); err != nil {
		t.Fatalf("expected cache under cache root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(remote.Cache.Root(), "data", "encoding")); !os.IsNotExist(err) {
		t.Fatalf("unexpected Build-local payload copy: %v", err)
	}
}

func TestReadFileDataPartialWithStageAttributesArchivePayload(t *testing.T) {
	ResetHTTPMetrics()
	resource.ResetStages()
	payload := buildCascTestBLTE([]byte("staged archive"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Range") == "" {
			t.Fatal("expected range request")
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(payload)-1, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	remote := NewCASCRemote("us")
	remote.Host = server.URL + "/"
	remote.Cache = NewDataCache(t.TempDir(), "test-build")
	remote.RootEntries[10] = []RootEntry{{TypeIndex: 0, ContentKey: "content"}}
	remote.RootTypes = append(remote.RootTypes, RootType{LocaleFlags: LocaleEnUS})
	remote.EncodingEntries["content"] = EncodingEntry{Key: "encoding"}
	remote.Archives["encoding"] = ArchiveEntry{Key: "archive", Size: int32(len(payload))}
	remote.Locale = LocaleEnUS

	stage := resource.StartStage("db2-file-read", resource.StageOptions{})
	data, err := remote.ReadFileDataPartialWithStage(10, stage)
	resource.FinishStage(stage, err)
	if err != nil {
		t.Fatalf("ReadFileDataPartialWithStage: %v", err)
	}
	if string(data) != "staged archive" {
		t.Fatalf("data = %q", data)
	}

	metrics := SnapshotHTTPMetrics()
	resource.ReconcileStageNetwork(uint64(metrics.UniquePayloadBytes), uint64(metrics.ResponseBytes))
	stages := resource.SnapshotStages()
	if !stages.Complete || len(stages.Work) != 1 {
		t.Fatalf("stages = %+v", stages)
	}
	if got := stages.Work[0]; got.StageID != stage || got.NetworkUniqueBytes != uint64(len(payload)) || got.NetworkTransferredBytes != uint64(len(payload)) {
		t.Fatalf("stage work = %+v", got)
	}
}

func TestReadFileDataUsesUnarchivedEncodingKey(t *testing.T) {
	remote := NewCASCRemote("us")
	remote.Cache = NewDataCache(t.TempDir(), "test-build")
	remote.RootEntries[10] = []RootEntry{{TypeIndex: 0, ContentKey: "content"}}
	remote.RootTypes = append(remote.RootTypes, RootType{LocaleFlags: LocaleEnUS})
	remote.EncodingEntries["content"] = EncodingEntry{Key: "encoding"}
	remote.Locale = LocaleEnUS

	var gotFile string
	remote.fetchFull = func(cdnFile string) ([]byte, error) {
		gotFile = cdnFile
		return buildCascTestBLTE([]byte("hello full")), nil
	}

	data, err := remote.ReadFileData(10)
	if err != nil {
		t.Fatalf("ReadFileData: %v", err)
	}
	if string(data) != "hello full" {
		t.Fatalf("data = %q", data)
	}
	if gotFile != FormatCDNKey("encoding") {
		t.Fatalf("gotFile = %s", gotFile)
	}
}

func TestHTTPRangeLargeFileUsesConcurrentChunks(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), 512*1024)
	var mu sync.Mutex
	ranges := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		mu.Lock()
		ranges = append(ranges, rangeHeader)
		mu.Unlock()
		if rangeHeader == "" {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusOK)
			return
		}
		var start, end int
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
			t.Fatalf("bad range header %q: %v", rangeHeader, err)
		}
		if end >= len(payload) {
			end = len(payload) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(payload[start : end+1])
	}))
	defer server.Close()

	data, err := httpRange(server.URL, 0, len(payload)-1)
	if err != nil {
		t.Fatalf("httpRange: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatal("downloaded payload differs")
	}
	chunkCount := 0
	mu.Lock()
	for _, header := range ranges {
		if strings.HasPrefix(header, "bytes=") {
			chunkCount++
		}
	}
	mu.Unlock()
	if chunkCount < 2 {
		t.Fatalf("expected multiple range chunks, got headers %#v", ranges)
	}
}

func TestDownloadHTTPConcurrentUsesFirstRangeAsProbe(t *testing.T) {
	payload := bytes.Repeat([]byte("probe"), (rangeChunkSize*3)/5+1)
	var mu sync.Mutex
	requests := make([]string, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.Header.Get("Range"))
		mu.Unlock()
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			t.Fatalf("bad range header %q: %v", r.Header.Get("Range"), err)
		}
		if end >= len(payload) {
			end = len(payload) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer server.Close()

	got, err := DownloadHTTPConcurrentWithWorkers(server.URL, 2)
	if err != nil {
		t.Fatalf("DownloadHTTPConcurrentWithWorkers: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("downloaded payload differs")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("requests = %v, want exactly one request per payload chunk", requests)
	}
	for _, request := range requests {
		if strings.HasPrefix(request, http.MethodHead+" ") {
			t.Fatalf("unexpected serialized HEAD probe: %v", requests)
		}
	}
}

func TestDownloadHTTPConcurrentCompletesSmallObjectWithProbe(t *testing.T) {
	payload := bytes.Repeat([]byte("small"), 1024)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(payload)-1, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	got, err := DownloadHTTPConcurrent(server.URL)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("download err=%v equal=%v", err, bytes.Equal(got, payload))
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests=%d, want 1", got)
	}
}

func TestDownloadHTTPConcurrentUsesIgnoredRangeResponseAsFullBody(t *testing.T) {
	payload := bytes.Repeat([]byte("full"), 1024)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	got, err := DownloadHTTPConcurrent(server.URL)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("download err=%v equal=%v", err, bytes.Equal(got, payload))
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests=%d, want 1", got)
	}
}

func TestResumableDownloadReusesCompletedChunks(t *testing.T) {
	payload := bytes.Repeat([]byte("r"), rangeChunkSize*2+17)
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
			return
		}
		requested = append(requested, r.Header.Get("Range"))
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			t.Fatalf("bad range: %v", err)
		}
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer server.Close()
	dir := t.TempDir()
	var progress bytes.Buffer
	oldWriter := downloadProgressWriter
	downloadProgressWriter = &progress
	defer func() { downloadProgressWriter = oldWriter }()
	part, statePath := filepath.Join(dir, "object.part"), filepath.Join(dir, "object.json")
	if err := os.WriteFile(part, payload[:rangeChunkSize], 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(part, int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	state := resumeState{URL: server.URL, Size: int64(len(payload)), ChunkSize: rangeChunkSize, Complete: []bool{true, false, false}, Hashes: []string{hashBytes(payload[:rangeChunkSize]), "", ""}}
	if err := saveResumeState(statePath, state); err != nil {
		t.Fatal(err)
	}
	data, err := DownloadHTTPConcurrentResumable(server.URL, part, statePath, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatal("resumed payload differs")
	}
	if len(requested) != 2 {
		t.Fatalf("expected only missing ranges, got %#v", requested)
	}
	wantRanges := map[string]bool{
		fmt.Sprintf("bytes=%d-%d", rangeChunkSize, rangeChunkSize*2-1): true,
		fmt.Sprintf("bytes=%d-%d", rangeChunkSize*2, len(payload)-1):   true,
	}
	for _, got := range requested {
		if !wantRanges[got] {
			t.Fatalf("unexpected resumed range %q", got)
		}
	}
	log := progress.String()
	if !strings.Contains(log, fmt.Sprintf("reusedBytes=%d", rangeChunkSize)) || !strings.Contains(log, fmt.Sprintf("downloadedBytes=%d", rangeChunkSize+17)) || !strings.Contains(log, "reusedChunks=1") || !strings.Contains(log, "downloadedChunks=2") {
		t.Fatalf("resume progress = %q", log)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal("part file should be removed after publication")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatal("resume state should be removed after publication")
	}
}

func TestResumableDownloadUsesConfiguredChunkSize(t *testing.T) {
	const chunkSize = int64(4 << 20)
	payload := bytes.Repeat([]byte("z"), int(chunkSize*2+17))
	var mu sync.Mutex
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
			return
		}
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			t.Errorf("bad range: %v", err)
			return
		}
		mu.Lock()
		requested = append(requested, r.Header.Get("Range"))
		mu.Unlock()
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer server.Close()

	dir := t.TempDir()
	data, err := DownloadHTTPConcurrentResumableWithOptions(
		server.URL,
		filepath.Join(dir, "object.part"),
		filepath.Join(dir, "object.json"),
		ResumeOptions{Workers: 4, TTL: time.Hour, ChunkSize: chunkSize},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatal("downloaded payload differs")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requested) != 3 {
		t.Fatalf("ranges = %#v, want three 4 MiB chunks", requested)
	}
}

func TestResumableDownloadKeepPartAvoidsReadback(t *testing.T) {
	payload := bytes.Repeat([]byte("keep-part"), (2<<20)/9+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			return
		}
		var start, end int
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		if end >= len(payload) {
			end = len(payload) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer server.Close()

	dir := t.TempDir()
	part := filepath.Join(dir, "payload.part")
	statePath := filepath.Join(dir, "payload.state.json")
	data, err := DownloadHTTPConcurrentResumableWithOptions(server.URL, part, statePath, ResumeOptions{
		Workers: 2, TTL: time.Hour, ChunkSize: 1 << 20, KeepPart: true,
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if data != nil {
		t.Fatalf("KeepPart returned %d in-memory bytes, want nil", len(data))
	}
	got, err := os.ReadFile(part)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("part err=%v equal=%v", err, bytes.Equal(got, payload))
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state should be removed after completed part: %v", err)
	}
}

func TestResumableDownloadHonorsContextDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(2<<20))
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	dir := t.TempDir()
	started := time.Now()
	_, err := DownloadHTTPConcurrentResumableWithOptions(server.URL, filepath.Join(dir, "payload.part"), filepath.Join(dir, "payload.state.json"), ResumeOptions{
		Workers: 2, TTL: time.Hour, ChunkSize: 1 << 20, Context: ctx,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
}

func TestResumableDownloadRejectsQuotaBeforeGET(t *testing.T) {
	payload := bytes.Repeat([]byte("q"), rangeChunkSize)
	var gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		if r.Method == http.MethodGet {
			gets.Add(1)
		}
	}))
	defer server.Close()

	cacheRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(cacheRoot, "existing"), []byte("occupied"), 0644); err != nil {
		t.Fatal(err)
	}
	part, statePath := ResumeObjectPaths(filepath.Join(cacheRoot, "casc", "resume"), server.URL)
	_, err := DownloadHTTPConcurrentResumableWithOptions(server.URL, part, statePath, ResumeOptions{
		Workers: 2, CacheRoot: cacheRoot, MaxBytes: int64(len(payload)), TTL: time.Hour,
	})
	if !IsCacheQuotaError(err) {
		t.Fatalf("error = %v, want cache quota error", err)
	}
	if got := gets.Load(); got != 0 {
		t.Fatalf("GET count = %d, want 0", got)
	}
	if _, statErr := os.Stat(part); !os.IsNotExist(statErr) {
		t.Fatalf("quota failure published reservation: %v", statErr)
	}
}

func TestResumableDownloadRejectsObjectLimitBeforeGET(t *testing.T) {
	var gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", "1048577")
		if r.Method == http.MethodGet {
			gets.Add(1)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	_, err := DownloadHTTPConcurrentResumableWithOptions(server.URL,
		filepath.Join(dir, "payload.part"), filepath.Join(dir, "payload.state.json"), ResumeOptions{
			Workers: 2, ObjectMaxBytes: 1 << 20, TTL: time.Hour,
		})
	if err == nil || !strings.Contains(err.Error(), "object byte limit") {
		t.Fatalf("error = %v, want object byte limit", err)
	}
	if got := gets.Load(); got != 0 {
		t.Fatalf("GET requests = %d, want zero before object limit rejection", got)
	}
}

func TestResumableDownloadDiscardsExpiredState(t *testing.T) {
	payload := bytes.Repeat([]byte("t"), rangeChunkSize*2)
	var requested atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
			return
		}
		requested.Add(1)
		var start, end int
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer server.Close()

	dir := t.TempDir()
	part, statePath := filepath.Join(dir, "object.part"), filepath.Join(dir, "object.json")
	if err := os.WriteFile(part, payload, 0644); err != nil {
		t.Fatal(err)
	}
	state := resumeState{URL: server.URL, Size: int64(len(payload)), ChunkSize: rangeChunkSize, Complete: []bool{true, false}, Hashes: []string{hashBytes(payload[:rangeChunkSize]), ""}}
	if err := saveResumeState(statePath, state); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(statePath, old, old); err != nil {
		t.Fatal(err)
	}
	data, err := DownloadHTTPConcurrentResumableWithOptions(server.URL, part, statePath, ResumeOptions{Workers: 2, TTL: time.Hour})
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("download err=%v equal=%v", err, bytes.Equal(data, payload))
	}
	if got := requested.Load(); got != 2 {
		t.Fatalf("requested chunks = %d, want 2 after TTL expiry", got)
	}
}

func TestResumableDownloadRepairsCorruptCompletedChunk(t *testing.T) {
	payload := bytes.Repeat([]byte("c"), rangeChunkSize+17)
	requested := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
			return
		}
		requested[r.Header.Get("Range")]++
		var start, end int
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer server.Close()

	dir := t.TempDir()
	part, statePath := filepath.Join(dir, "object.part"), filepath.Join(dir, "object.json")
	corrupt := append([]byte(nil), payload...)
	corrupt[0] = 'x'
	if err := os.WriteFile(part, corrupt, 0644); err != nil {
		t.Fatal(err)
	}
	state := resumeState{
		URL: server.URL, Size: int64(len(payload)), ChunkSize: rangeChunkSize,
		Complete: []bool{true, true},
		Hashes:   []string{hashBytes(payload[:rangeChunkSize]), hashBytes(payload[rangeChunkSize:])},
	}
	if err := saveResumeState(statePath, state); err != nil {
		t.Fatal(err)
	}
	data, err := DownloadHTTPConcurrentResumable(server.URL, part, statePath, 2)
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("repaired data: err=%v equal=%v", err, bytes.Equal(data, payload))
	}
	firstRange := fmt.Sprintf("bytes=0-%d", rangeChunkSize-1)
	if requested[firstRange] != 1 || len(requested) != 1 {
		t.Fatalf("requested ranges = %#v; want only corrupt chunk", requested)
	}
}

func TestResumableDownloadFallsBackWithoutRangeSupport(t *testing.T) {
	payload := []byte("complete response")
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		if r.Method == http.MethodGet {
			gets++
			_, _ = w.Write(payload)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	part, statePath := filepath.Join(dir, "object.part"), filepath.Join(dir, "object.json")
	if err := os.WriteFile(part, []byte("untrusted"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(`{"complete":[true]}`), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := DownloadHTTPConcurrentResumable(server.URL, part, statePath, 2)
	if err != nil || !bytes.Equal(data, payload) || gets != 1 {
		t.Fatalf("fallback data=%q gets=%d err=%v", data, gets, err)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatalf("untrusted part was not removed: %v", err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("untrusted state was not removed: %v", err)
	}
}

func TestResumableDownloadFallsBackWhenServerIgnoresAdvertisedRange(t *testing.T) {
	payload := bytes.Repeat([]byte("ignored-range"), rangeChunkSize)
	var rangedGets atomic.Int32
	var fullGets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			return
		}
		if r.Header.Get("Range") != "" {
			rangedGets.Add(1)
		} else {
			fullGets.Add(1)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	dir := t.TempDir()
	part, statePath := filepath.Join(dir, "object.part"), filepath.Join(dir, "object.json")
	data, err := DownloadHTTPConcurrentResumable(server.URL, part, statePath, 2)
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("fallback err=%v equal=%v", err, bytes.Equal(data, payload))
	}
	if got := fullGets.Load(); got != 1 {
		t.Fatalf("full GET count = %d, want 1", got)
	}
	if got := rangedGets.Load(); got < 1 || got > 2 {
		t.Fatalf("ranged GET count = %d, want between 1 and worker limit 2", got)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatalf("part file was not removed: %v", err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("resume state was not removed: %v", err)
	}
}

func TestArchiveIndexSinglePublisherAcrossBuilds(t *testing.T) {
	payload := []byte("shared archive index")
	var gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		if r.Method == http.MethodGet {
			gets.Add(1)
			_, _ = w.Write(payload)
		}
	}))
	defer server.Close()

	cacheRoot := t.TempDir()
	key := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	remotes := []*CASCRemote{NewCASCRemote("cn"), NewCASCRemote("us")}
	for index, remote := range remotes {
		remote.Host = server.URL + "/"
		remote.Cache = NewDataCache(cacheRoot, fmt.Sprintf("build-%d", index))
		remote.Workers = 2
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(remotes))
	for _, remote := range remotes {
		wg.Add(1)
		go func(remote *CASCRemote) {
			defer wg.Done()
			data, err := remote.downloadArchiveIndex(key)
			if err == nil && !bytes.Equal(data, payload) {
				err = fmt.Errorf("payload differs")
			}
			errs <- err
		}(remote)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := gets.Load(); got != 1 {
		t.Fatalf("GET count = %d, want 1", got)
	}
	locks, err := os.ReadDir(filepath.Join(cacheRoot, "casc", "object_locks"))
	if err != nil || len(locks) != 0 {
		t.Fatalf("object locks = %d err=%v", len(locks), err)
	}
}

func TestResumableDownloadContinuesAtRecordedFractions(t *testing.T) {
	tests := []struct {
		name      string
		chunks    int
		completed int
	}{
		{name: "25-percent", chunks: 4, completed: 1},
		{name: "50-percent", chunks: 4, completed: 2},
		{name: "90-percent", chunks: 10, completed: 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := bytes.Repeat([]byte{byte(tt.completed + 1)}, rangeChunkSize*tt.chunks)
			var requested atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					w.Header().Set("Accept-Ranges", "bytes")
					w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
					return
				}
				requested.Add(1)
				var start, end int
				_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(payload[start : end+1])
			}))
			defer server.Close()
			dir := t.TempDir()
			part, statePath := filepath.Join(dir, "object.part"), filepath.Join(dir, "object.json")
			if err := os.WriteFile(part, payload, 0644); err != nil {
				t.Fatal(err)
			}
			state := resumeState{URL: server.URL, Size: int64(len(payload)), ChunkSize: rangeChunkSize, Complete: make([]bool, tt.chunks), Hashes: make([]string, tt.chunks)}
			for i := 0; i < tt.completed; i++ {
				state.Complete[i] = true
				state.Hashes[i] = hashBytes(payload[i*rangeChunkSize : (i+1)*rangeChunkSize])
			}
			if err := saveResumeState(statePath, state); err != nil {
				t.Fatal(err)
			}
			data, err := DownloadHTTPConcurrentResumable(server.URL, part, statePath, 4)
			if err != nil || !bytes.Equal(data, payload) {
				t.Fatalf("resume err=%v equal=%v", err, bytes.Equal(data, payload))
			}
			if got := int(requested.Load()); got != tt.chunks-tt.completed {
				t.Fatalf("requested %d chunks, want %d", got, tt.chunks-tt.completed)
			}
		})
	}
}

func TestHTTPRangeConcurrentUsesFourWorkersByDefault(t *testing.T) {
	if rangeWorkerCount != 4 {
		t.Fatalf("rangeWorkerCount = %d, want 4", rangeWorkerCount)
	}
}

func TestHTTPRangeConcurrentUsesConfiguredWorkers(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), rangeChunkSize*4)
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			t.Fatalf("bad range header %q: %v", r.Header.Get("Range"), err)
		}
		started <- struct{}{}
		<-release
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer server.Close()

	done := make(chan error, 1)
	go func() {
		_, err := httpRangeConcurrentWithWorkers(server.URL, 0, len(payload)-1, 2)
		done <- err
	}()
	<-started
	<-started
	select {
	case <-started:
		t.Fatal("more than two range workers started before a slot was released")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("httpRangeConcurrentWithWorkers: %v", err)
	}
}

func TestHTTPRangeConcurrentReportsElapsedToStderr(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), rangeChunkSize+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			t.Fatalf("bad range header %q: %v", r.Header.Get("Range"), err)
		}
		if end >= len(payload) {
			end = len(payload) - 1
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write(payload[start : end+1])
	}))
	defer server.Close()

	var stderr bytes.Buffer
	oldWriter := downloadProgressWriter
	downloadProgressWriter = &stderr
	defer func() { downloadProgressWriter = oldWriter }()

	if _, err := httpRangeConcurrent(server.URL, 0, len(payload)-1); err != nil {
		t.Fatalf("httpRangeConcurrent: %v", err)
	}
	log := stderr.String()
	if !strings.Contains(log, "download") || !strings.Contains(log, "duration=") || !strings.Contains(log, "workers=2") {
		t.Fatalf("download progress log = %q", log)
	}
}

func TestHTTPRangeSingleRetriesTransientErrors(t *testing.T) {
	attempts := 0
	payload := []byte("retry-ok")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write(payload)
	}))
	defer server.Close()

	data, err := httpRangeSingle(server.URL, 0, len(payload)-1)
	if err != nil {
		t.Fatalf("httpRangeSingle: %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("data = %q", data)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func buildCascTestBLTE(payload []byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(0x45544c42))
	binary.Write(buf, binary.BigEndian, int32(0))
	buf.WriteByte(0x4E)
	buf.Write(payload)
	return buf.Bytes()
}
