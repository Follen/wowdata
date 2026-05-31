package casc

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

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
	if _, err := os.Stat(filepath.Join(remote.Cache.Root(), "data", "encoding")); err != nil {
		t.Fatalf("expected cache under cache root: %v", err)
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

func TestHTTPRangeConcurrentUsesSixteenWorkersByDefault(t *testing.T) {
	if rangeWorkerCount != 16 {
		t.Fatalf("rangeWorkerCount = %d, want 16", rangeWorkerCount)
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
