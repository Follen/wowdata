package casc

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func buildCDNIndexFixture(t *testing.T, offsetBytes int, entries map[string]ArchiveEntry) []byte {
	t.Helper()
	const pageSize = 4096
	const keyBytes = 16
	const sizeBytes = 4
	const checksumSize = 8
	entrySize := keyBytes + sizeBytes + offsetBytes
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sortStrings(keys)
	entriesPerPage := pageSize / entrySize
	pageCount := (len(keys) + entriesPerPage - 1) / entriesPerPage
	data := make([]byte, pageCount*pageSize)
	lastKeys := make([]byte, 0, pageCount*keyBytes)
	hashes := make([]byte, 0, pageCount*checksumSize)
	for page := 0; page < pageCount; page++ {
		start := page * pageSize
		for slot := 0; slot < entriesPerPage; slot++ {
			index := page*entriesPerPage + slot
			if index >= len(keys) {
				break
			}
			key := keys[index]
			raw, err := hex.DecodeString(key)
			if err != nil {
				t.Fatal(err)
			}
			pos := start + slot*entrySize
			copy(data[pos:], raw)
			entry := entries[key]
			binary.BigEndian.PutUint32(data[pos+keyBytes:], uint32(entry.Size))
			for i := offsetBytes - 1; i >= 0; i-- {
				data[pos+keyBytes+sizeBytes+i] = byte(uint32(entry.Offset) >> (8 * (offsetBytes - 1 - i)))
			}
		}
		last := keys[minInt((page+1)*entriesPerPage, len(keys))-1]
		raw, _ := hex.DecodeString(last)
		lastKeys = append(lastKeys, raw...)
		sum := md5.Sum(data[start : start+pageSize])
		hashes = append(hashes, sum[:checksumSize]...)
	}
	footer := make([]byte, cdnIndexFooterSize)
	footer[8] = 1
	footer[11] = 4
	footer[12] = byte(offsetBytes)
	footer[13] = sizeBytes
	footer[14] = keyBytes
	footer[15] = checksumSize
	binary.LittleEndian.PutUint32(footer[16:], uint32(len(keys)))
	return append(append(append(data, lastKeys...), hashes...), footer...)
}

func buildCDNGroupIndexFixture(t *testing.T, key string, size int32, archiveOrdinal uint16, offset uint32, offsetBytes int) []byte {
	t.Helper()
	const pageSize = 4096
	const keyBytes = 16
	const sizeBytes = 4
	const checksumSize = 8
	if offsetBytes < 5 || offsetBytes > 6 {
		t.Fatalf("unsupported group fixture offset bytes: %d", offsetBytes)
	}
	page := make([]byte, pageSize)
	raw, err := hex.DecodeString(key)
	if err != nil {
		t.Fatal(err)
	}
	copy(page, raw)
	binary.BigEndian.PutUint32(page[keyBytes:], uint32(size))
	archivePos := keyBytes + sizeBytes
	if offsetBytes == 5 {
		if archiveOrdinal > 255 {
			t.Fatalf("archive ordinal %d does not fit one byte", archiveOrdinal)
		}
		page[archivePos] = byte(archiveOrdinal)
	} else {
		binary.BigEndian.PutUint16(page[archivePos:], archiveOrdinal)
	}
	binary.BigEndian.PutUint32(page[archivePos+offsetBytes-4:], offset)
	sum := md5.Sum(page)
	footer := make([]byte, cdnIndexFooterSize)
	footer[8] = 1
	footer[11] = 4
	footer[12] = byte(offsetBytes)
	footer[13] = sizeBytes
	footer[14] = keyBytes
	footer[15] = checksumSize
	binary.LittleEndian.PutUint32(footer[16:], 1)
	return append(append(append(page, raw...), sum[:checksumSize]...), footer...)
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestParseCDNIndexSelectedUsesTOCPages(t *testing.T) {
	entries := make(map[string]ArchiveEntry)
	for i := 1; i <= 400; i++ {
		key := make([]byte, 16)
		binary.BigEndian.PutUint32(key[12:], uint32(i))
		entries[hex.EncodeToString(key)] = ArchiveEntry{Size: int32(100 + i), Offset: int32(1000 + i)}
	}
	wantedKey := "0000000000000000000000000000012c"
	data := buildCDNIndexFixture(t, 4, entries)
	found, err := parseCDNIndexSelected(data, map[string]struct{}{wantedKey: {}}, "archive")
	if err != nil {
		t.Fatal(err)
	}
	if got := found[wantedKey]; got.Key != "archive" || got.Size != 400 || got.Offset != 1300 {
		t.Fatalf("entry = %#v", got)
	}
}

func TestParseCDNIndexDetectsCorruptCandidatePage(t *testing.T) {
	key := "11111111111111111111111111111111"
	data := buildCDNIndexFixture(t, 4, map[string]ArchiveEntry{key: {Size: 12, Offset: 34}})
	data[0] ^= 0xff
	_, err := parseCDNIndexSelected(data, map[string]struct{}{key: {}}, "archive")
	if err == nil {
		t.Fatal("expected page checksum error")
	}
}

func TestLooseCDNIndexHasZeroOffset(t *testing.T) {
	key := "22222222222222222222222222222222"
	data := buildCDNIndexFixture(t, 0, map[string]ArchiveEntry{key: {Size: 77}})
	found, err := parseCDNIndexSelected(data, map[string]struct{}{key: {}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := found[key]; got.Size != 77 || got.Offset != 0 {
		t.Fatalf("entry = %#v", got)
	}
}

func TestGroupLocatorResolvesArchiveOrdinalWithoutNetwork(t *testing.T) {
	targetKey := "77777777777777777777777777777777"
	groupKey := "88888888888888888888888888888888"
	archives := []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccc",
	}
	remote := NewCASCRemote("cn")
	remote.CacheRoot = t.TempDir()
	remote.Cache = NewDataCache(remote.CacheRoot, "group-direct")
	remote.fetchPartial = func(string, int, int) ([]byte, error) {
		t.Fatal("cached group lookup attempted a network request")
		return nil, nil
	}
	group := buildCDNGroupIndexFixture(t, targetKey, 1389, 1, 106738989, 6)
	if err := remote.Cache.StoreObject(groupKey+".index", group); err != nil {
		t.Fatal(err)
	}

	found, err := (&cdnGroupLocator{remote: remote, key: groupKey, archiveKeys: archives}).LocateArchives(map[string]struct{}{targetKey: {}})
	if err != nil {
		t.Fatal(err)
	}
	if got := found[targetKey]; got.Key != archives[1] || got.Size != 1389 || got.Offset != 106738989 {
		t.Fatalf("group entry = %#v", got)
	}
}

func TestPatchGroupLocatorSupportsOneByteArchiveOrdinal(t *testing.T) {
	targetKey := "99999999999999999999999999999999"
	groupKey := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	archives := []string{
		"11111111111111111111111111111111",
		"22222222222222222222222222222222",
		"33333333333333333333333333333333",
	}
	remote := NewCASCRemote("cn")
	remote.CacheRoot = t.TempDir()
	remote.Cache = NewDataCache(remote.CacheRoot, "patch-group-direct")
	group := buildCDNGroupIndexFixture(t, targetKey, 2048, 2, 4096, 5)
	if err := remote.Cache.StoreObject(groupKey+".index", group); err != nil {
		t.Fatal(err)
	}
	found, err := (&cdnGroupLocator{remote: remote, key: groupKey, archiveKeys: archives}).LocateArchives(map[string]struct{}{targetKey: {}})
	if err != nil {
		t.Fatal(err)
	}
	if got := found[targetKey]; got.Key != archives[2] || got.Size != 2048 || got.Offset != 4096 {
		t.Fatalf("patch group entry = %#v", got)
	}
}

func TestArchiveLocatorStopsAtProbeBudget(t *testing.T) {
	targetKey := "00000000000000000000000000000001"
	index := buildCDNIndexFixture(t, 4, map[string]ArchiveEntry{
		"11111111111111111111111111111111": {Size: 1, Offset: 2},
	})
	archives := []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccc",
		"dddddddddddddddddddddddddddddddd",
		"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		serveIndexRange(t, w, r, index)
	}))
	defer server.Close()
	remote := NewCASCRemote("cn")
	remote.Host = server.URL + "/"
	remote.CacheRoot = t.TempDir()
	remote.Cache = NewDataCache(remote.CacheRoot, "bounded-archive-probe")
	remote.MetadataWorkers = 2

	_, err := (&cdnArchiveLocator{remote: remote, archiveKeys: archives, tailProbe: 4 << 10, probeLimit: 2}).LocateArchives(map[string]struct{}{targetKey: {}})
	if err == nil || !strings.Contains(err.Error(), "probe budget exhausted after 2 of 5") {
		t.Fatalf("error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 4 {
		t.Fatalf("requests = %d, want two tails and two candidate pages", requests)
	}
}

func TestFileLocatorFetchesOnlyTOCAndCandidatePage(t *testing.T) {
	entries := make(map[string]ArchiveEntry)
	for i := 1; i <= 400; i++ {
		key := make([]byte, 16)
		binary.BigEndian.PutUint32(key[12:], uint32(i))
		entries[hex.EncodeToString(key)] = ArchiveEntry{Size: int32(100 + i)}
	}
	wantedKey := "0000000000000000000000000000012c"
	index := buildCDNIndexFixture(t, 0, entries)
	indexKey := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	var mu sync.Mutex
	var requests int
	var transferred int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/data/"+FormatCDNKey(indexKey)+".index" {
			http.NotFound(w, r)
			return
		}
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			t.Errorf("bad range %q: %v", r.Header.Get("Range"), err)
			return
		}
		if start < 0 || end < start || end >= len(index) {
			t.Errorf("range %d-%d is outside index size %d", start, end, len(index))
			return
		}
		mu.Lock()
		requests++
		transferred += end - start + 1
		mu.Unlock()
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(index)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(index[start : end+1])
	}))
	defer server.Close()

	ResetHTTPMetrics()
	remote := NewCASCRemote("cn")
	remote.Host = server.URL + "/"
	remote.CacheRoot = t.TempDir()
	remote.Cache = NewDataCache(remote.CacheRoot, "test-build")
	locator := &cdnFileLocator{remote: remote, key: indexKey, size: len(index)}
	wanted := map[string]struct{}{wantedKey: {}}
	found, err := locator.LocateFiles(wanted)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := found[wantedKey]; !ok {
		t.Fatalf("encoding key %s was not found", wantedKey)
	}
	mu.Lock()
	firstRequests, firstTransferred := requests, transferred
	mu.Unlock()
	if firstRequests != 2 {
		t.Fatalf("requests = %d, want footer/TOC tail plus overlapping page prefix", firstRequests)
	}
	if firstTransferred >= len(index) || firstTransferred != 4172 {
		t.Fatalf("transferred = %d, full index = %d", firstTransferred, len(index))
	}
	if metrics := SnapshotHTTPMetrics(); metrics.DuplicateBytes != 0 {
		t.Fatalf("duplicate payload bytes = %d", metrics.DuplicateBytes)
	}

	found, err = locator.LocateFiles(wanted)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := found[wantedKey]; !ok {
		t.Fatalf("cached encoding key %s was not found", wantedKey)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != firstRequests || transferred != firstTransferred {
		t.Fatalf("repeat-warm network changed: requests %d->%d bytes %d->%d", firstRequests, requests, firstTransferred, transferred)
	}
}

func TestFileLocatorLoadsCandidatePagesWithinMetadataBudget(t *testing.T) {
	entries := make(map[string]ArchiveEntry)
	for i := 1; i <= 800; i++ {
		key := make([]byte, 16)
		binary.BigEndian.PutUint32(key[12:], uint32(i))
		entries[hex.EncodeToString(key)] = ArchiveEntry{Size: int32(i)}
	}
	index := buildCDNIndexFixture(t, 0, entries)
	indexKey := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	wanted := make(map[string]struct{})
	for _, value := range []uint32{10, 300, 500} {
		key := make([]byte, 16)
		binary.BigEndian.PutUint32(key[12:], value)
		wanted[hex.EncodeToString(key)] = struct{}{}
	}

	var mu sync.Mutex
	active, peak := 0, 0
	remote := NewCASCRemote("cn")
	remote.CacheRoot = t.TempDir()
	remote.Cache = NewDataCache(remote.CacheRoot, "test-build")
	remote.MetadataWorkers = 3
	remote.fetchPartial = func(_ string, offset, length int) ([]byte, error) {
		if offset < len(index)-(4<<10) {
			mu.Lock()
			active++
			if active > peak {
				peak = active
			}
			mu.Unlock()
			time.Sleep(30 * time.Millisecond)
			mu.Lock()
			active--
			mu.Unlock()
		}
		return append([]byte(nil), index[offset:offset+length]...), nil
	}

	found, err := (&cdnFileLocator{remote: remote, key: indexKey, size: len(index)}).LocateFiles(wanted)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != len(wanted) {
		t.Fatalf("found %d keys, want %d", len(found), len(wanted))
	}
	mu.Lock()
	defer mu.Unlock()
	if peak < 2 || peak > remote.MetadataWorkers {
		t.Fatalf("peak candidate page requests = %d, budget = %d", peak, remote.MetadataWorkers)
	}
}

func TestFileLocatorReusesFullResponseWhenRangeIsIgnored(t *testing.T) {
	key := "33333333333333333333333333333333"
	indexKey := "cccccccccccccccccccccccccccccccc"
	index := buildCDNIndexFixture(t, 0, map[string]ArchiveEntry{key: {Size: 77}})
	remote := NewCASCRemote("cn")
	remote.CacheRoot = t.TempDir()
	remote.Cache = NewDataCache(remote.CacheRoot, "test-build")
	requests := 0
	remote.fetchPartial = func(_ string, _, _ int) ([]byte, error) {
		requests++
		return append([]byte(nil), index...), nil
	}

	found, err := (&cdnFileLocator{remote: remote, key: indexKey, size: len(index)}).LocateFiles(map[string]struct{}{key: {}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := found[key]; !ok {
		t.Fatalf("encoding key %s was not found", key)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one full response reused as fallback", requests)
	}
	full, err := remote.Cache.GetObject(indexKey + ".index")
	if err != nil || len(full) != len(index) {
		t.Fatalf("full response cache bytes = %d, err = %v", len(full), err)
	}
}

func TestArchivedEncodingKeyMissesLooseIndexAndFallsBackToArchive(t *testing.T) {
	targetKey := "c8db8a0388577f987eb006f5cfcda49f"
	looseIndexKey := "54d9f67d89379374187d7e6d35bac408"
	targetArchive := "e353ca95b78f9ead4290b49c65a19d63"
	missArchive := "0017a402f556fbece46c38dc431a2c9b"
	looseIndex := buildCDNIndexFixture(t, 0, map[string]ArchiveEntry{
		"c7ffffffffffffffffffffffffffffff": {Size: 1},
		"c9000000000000000000000000000000": {Size: 2},
	})
	missIndex := buildCDNIndexFixture(t, 4, map[string]ArchiveEntry{
		"c8000000000000000000000000000000": {Size: 3, Offset: 4},
	})
	targetIndex := buildCDNIndexFixture(t, 4, map[string]ArchiveEntry{
		targetKey: {Size: 1389, Offset: 106738989},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data []byte
		switch r.URL.Path {
		case "/data/" + FormatCDNKey(looseIndexKey) + ".index":
			data = looseIndex
		case "/data/" + FormatCDNKey(missArchive) + ".index":
			data = missIndex
		case "/data/" + FormatCDNKey(targetArchive) + ".index":
			data = targetIndex
		default:
			http.NotFound(w, r)
			return
		}
		serveIndexRange(t, w, r, data)
	}))
	defer server.Close()

	remote := NewCASCRemote("us")
	remote.Host = server.URL + "/"
	remote.CacheRoot = t.TempDir()
	remote.Cache = NewDataCache(remote.CacheRoot, "classic-era-test-build")
	remote.MetadataWorkers = 1
	remote.CDNConfig = map[string]string{
		"archives":      missArchive + " " + targetArchive,
		"fileIndex":     looseIndexKey,
		"fileIndexSize": fmt.Sprintf("%d", len(looseIndex)),
	}

	if err := remote.loadArchivesSelected(map[string]struct{}{targetKey: {}}); err != nil {
		t.Fatal(err)
	}
	if got := remote.Archives[targetKey]; got.Key != targetArchive || got.Size != 1389 || got.Offset != 106738989 {
		t.Fatalf("archive entry = %#v", got)
	}
	if loose, err := (&cdnFileLocator{remote: remote, key: looseIndexKey, size: len(looseIndex)}).LocateFiles(map[string]struct{}{targetKey: {}}); err != nil {
		t.Fatal(err)
	} else if _, ok := loose[targetKey]; ok {
		t.Fatalf("archived encoding key %s was incorrectly classified as loose", targetKey)
	}
}

func TestSelectedNormalEncodingLookupSkipsPatchLocators(t *testing.T) {
	targetKey := "c8db8a0388577f987eb006f5cfcda49f"
	targetArchive := "e353ca95b78f9ead4290b49c65a19d63"
	patchArchive := "0017a402f556fbece46c38dc431a2c9b"
	patchIndex := "11111111111111111111111111111111"
	patchGroup := "22222222222222222222222222222222"
	targetIndex := buildCDNIndexFixture(t, 4, map[string]ArchiveEntry{
		targetKey: {Size: 1389, Offset: 106738989},
	})
	patchRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/data/" + FormatCDNKey(targetArchive) + ".index":
			serveIndexRange(t, w, r, targetIndex)
		case "/data/" + FormatCDNKey(patchArchive) + ".index",
			"/data/" + FormatCDNKey(patchIndex) + ".index",
			"/data/" + FormatCDNKey(patchGroup) + ".index":
			patchRequests++
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	remote := NewCASCRemote("us")
	remote.Host = server.URL + "/"
	remote.CacheRoot = t.TempDir()
	remote.Cache = NewDataCache(remote.CacheRoot, "normal-before-patch")
	remote.MetadataWorkers = 1
	remote.CDNConfig = map[string]string{
		"archives":          targetArchive,
		"patchArchives":     patchArchive,
		"patchFileIndex":    patchIndex,
		"patchArchiveGroup": patchGroup,
	}

	if err := remote.loadArchivesSelected(map[string]struct{}{targetKey: {}}); err != nil {
		t.Fatal(err)
	}
	if patchRequests != 0 {
		t.Fatalf("patch locator requests = %d, want 0 for normal encoding keys", patchRequests)
	}
	if got := remote.Archives[targetKey]; got.Key != targetArchive {
		t.Fatalf("archive entry = %#v", got)
	}
}

func TestArchiveLocatorDoesNotDuplicateTailFromCachedFullIndex(t *testing.T) {
	targetKey := "44444444444444444444444444444444"
	archiveKey := "dddddddddddddddddddddddddddddddd"
	index := buildCDNIndexFixture(t, 4, map[string]ArchiveEntry{
		targetKey: {Size: 321, Offset: 654},
	})
	remote := NewCASCRemote("us")
	remote.CacheRoot = t.TempDir()
	remote.Cache = NewDataCache(remote.CacheRoot, "cached-full-index")
	if err := remote.Cache.StoreObject(archiveKey+".index", index); err != nil {
		t.Fatal(err)
	}

	found, err := (&cdnArchiveLocator{
		remote: remote, archiveKeys: []string{archiveKey}, tailProbe: 4 << 10,
	}).LocateArchives(map[string]struct{}{targetKey: {}})
	if err != nil {
		t.Fatal(err)
	}
	if got := found[targetKey]; got.Key != archiveKey || got.Size != 321 || got.Offset != 654 {
		t.Fatalf("archive entry = %#v", got)
	}
	if _, err := remote.Cache.GetObject(archiveKey + ".index.tail"); err == nil {
		t.Fatal("cached full index produced a duplicate persisted tail")
	}
}

func TestPublishingFullArchiveIndexRemovesCachedFragments(t *testing.T) {
	archiveKey := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	fileName := archiveKey + ".index"
	remote := NewCASCRemote("us")
	remote.CacheRoot = t.TempDir()
	remote.Cache = NewDataCache(remote.CacheRoot, "publish-full-index")
	for _, fragment := range []string{
		fileName + ".tail",
		fileName + ".page-3",
		fileName + ".page-7.prefix-512",
	} {
		if err := remote.Cache.StoreObject(fragment, []byte(fragment)); err != nil {
			t.Fatal(err)
		}
	}
	full := buildCDNIndexFixture(t, 4, map[string]ArchiveEntry{
		"55555555555555555555555555555555": {Size: 1, Offset: 2},
	})
	if err := remote.storeCompleteArchiveIndex(fileName, full); err != nil {
		t.Fatal(err)
	}
	reopened := NewDataCache(remote.CacheRoot, "another-build-view")
	if got, err := reopened.GetObject(fileName); err != nil || !bytes.Equal(got, full) {
		t.Fatalf("complete index bytes = %d, err = %v", len(got), err)
	}
	for _, fragment := range []string{
		fileName + ".tail",
		fileName + ".page-3",
		fileName + ".page-7.prefix-512",
	} {
		if _, err := remote.Cache.GetObject(fragment); err == nil {
			t.Fatalf("fragment %q remained after complete index publish", fragment)
		}
	}
}

func serveIndexRange(t *testing.T, w http.ResponseWriter, r *http.Request, data []byte) {
	t.Helper()
	rangeHeader := r.Header.Get("Range")
	if strings.HasPrefix(rangeHeader, "bytes=-") {
		var length int
		if _, err := fmt.Sscanf(rangeHeader, "bytes=-%d", &length); err != nil {
			t.Errorf("bad suffix range %q: %v", rangeHeader, err)
			return
		}
		if length > len(data) {
			length = len(data)
		}
		start := len(data) - length
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start:])
		return
	}
	var start, end int
	if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
		t.Errorf("bad range %q: %v", rangeHeader, err)
		return
	}
	if start < 0 || end < start || end >= len(data) {
		t.Errorf("range %d-%d is outside index size %d", start, end, len(data))
		return
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(data[start : end+1])
}

func TestArchiveIndexTailProbeUsesLargestConfiguredIndex(t *testing.T) {
	probe := archiveIndexTailProbe(map[string]string{"archivesIndexSize": "8268 721028 45348"})
	if probe < 8192 || probe%4096 != 0 {
		t.Fatalf("probe = %d", probe)
	}
}

func TestRemoteUsesMeasuredArchiveTailProbeDefault(t *testing.T) {
	if got := NewCASCRemote("cn").ArchiveTailProbe; got != 64<<10 {
		t.Fatalf("archive tail probe = %d, want %d", got, 64<<10)
	}
}

func TestHTTPRangeHelpersDoNotRetryPermanent404(t *testing.T) {
	tests := []struct {
		name string
		call func(string) error
	}{
		{
			name: "range",
			call: func(url string) error {
				_, err := httpRangeSingleContext(context.Background(), url, 0, 3)
				return err
			},
		},
		{
			name: "suffix",
			call: func(url string) error {
				_, err := httpRangeSuffixInfoContext(context.Background(), url, 4)
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				http.NotFound(w, r)
			}))
			defer server.Close()

			err := tt.call(server.URL)
			if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
				t.Fatalf("error = %v, want HTTP 404", err)
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
		})
	}
}

func TestHTTPRangeHelpersRetryTransientStatuses(t *testing.T) {
	tests := []struct {
		name string
		call func(string) ([]byte, error)
	}{
		{
			name: "range",
			call: func(url string) ([]byte, error) {
				return httpRangeSingleContext(context.Background(), url, 0, 3)
			},
		},
		{
			name: "suffix",
			call: func(url string) ([]byte, error) {
				result, err := httpRangeSuffixInfoContext(context.Background(), url, 4)
				return result.data, err
			},
		},
	}
	statuses := []int{http.StatusTooManyRequests, http.StatusInternalServerError}
	for _, status := range statuses {
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%s/%d", tt.name, status), func(t *testing.T) {
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					requests++
					if requests == 1 {
						w.WriteHeader(status)
						return
					}
					w.Header().Set("Content-Range", "bytes 0-3/4")
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write([]byte("data"))
				}))
				defer server.Close()

				data, err := tt.call(server.URL)
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != "data" {
					t.Fatalf("data = %q, want data", data)
				}
				if requests != 2 {
					t.Fatalf("requests = %d, want 2", requests)
				}
			})
		}
	}
}

func TestArchiveLocatorPipelinesPagesAndCancelsOutstandingTOCs(t *testing.T) {
	ResetHTTPMetrics()
	targetKey := "11111111111111111111111111111111"
	fastArchive := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	slowArchive := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fastIndex := buildCDNIndexFixture(t, 4, map[string]ArchiveEntry{
		targetKey: {Size: 123, Offset: 456},
	})
	slowIndex := buildCDNIndexFixture(t, 4, map[string]ArchiveEntry{
		"22222222222222222222222222222222": {Size: 1, Offset: 2},
	})
	slowStarted := make(chan struct{})
	slowCanceled := make(chan struct{})
	pageRequested := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveRange := func(data []byte) {
			rangeHeader := r.Header.Get("Range")
			if strings.HasPrefix(rangeHeader, "bytes=-") {
				length := 4096
				start := len(data) - length
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(data[start:])
				return
			}
			select {
			case <-pageRequested:
			default:
				close(pageRequested)
			}
			var start, end int
			if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
				t.Errorf("bad range %q: %v", rangeHeader, err)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[start : end+1])
		}

		switch r.URL.Path {
		case "/data/" + FormatCDNKey(fastArchive) + ".index":
			serveRange(fastIndex)
		case "/data/" + FormatCDNKey(slowArchive) + ".index":
			select {
			case <-slowStarted:
			default:
				close(slowStarted)
			}
			select {
			case <-r.Context().Done():
				close(slowCanceled)
			case <-time.After(2 * time.Second):
				serveRange(slowIndex)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	remote := NewCASCRemote("cn")
	remote.Host = server.URL + "/"
	remote.CacheRoot = t.TempDir()
	remote.Cache = NewDataCache(remote.CacheRoot, "test-build")
	remote.Workers = 2
	locator := &cdnArchiveLocator{
		remote: remote, archiveKeys: []string{fastArchive, slowArchive}, tailProbe: 4096,
	}
	startedAt := time.Now()
	found, err := locator.LocateArchives(map[string]struct{}{targetKey: {}})
	if err != nil {
		t.Fatal(err)
	}
	if got := found[targetKey]; got.Key != fastArchive || got.Size != 123 || got.Offset != 456 {
		t.Fatalf("entry = %#v", got)
	}
	if elapsed := time.Since(startedAt); elapsed >= time.Second {
		t.Fatalf("locator waited for slow TOC: %s", elapsed)
	}
	select {
	case <-slowStarted:
	default:
		t.Fatal("slow TOC request did not start concurrently")
	}
	select {
	case <-pageRequested:
	default:
		t.Fatal("candidate page was not requested")
	}
	select {
	case <-slowCanceled:
	case <-time.After(time.Second):
		t.Fatal("slow TOC request was not canceled after the target resolved")
	}
	metrics := SnapshotHTTPMetrics()
	if metrics.DuplicateBytes != 0 {
		t.Fatalf("locator transferred duplicate payload bytes: %#v", metrics)
	}
}

func TestArchiveLocatorFetchesSmallTailThenMissingTOCPrefix(t *testing.T) {
	entries := make(map[string]ArchiveEntry)
	for i := 1; i <= 30000; i++ {
		key := make([]byte, 16)
		binary.BigEndian.PutUint32(key[12:], uint32(i))
		entries[hex.EncodeToString(key)] = ArchiveEntry{Size: int32(i), Offset: int32(i * 2)}
	}
	targetKey := "00000000000000000000000000007530"
	archiveKey := "e353ca95b78f9ead4290b49c65a19d63"
	index := buildCDNIndexFixture(t, 4, entries)

	var mu sync.Mutex
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		serveIndexRange(t, w, r, index)
	}))
	defer server.Close()

	ResetHTTPMetrics()
	remote := NewCASCRemote("us")
	remote.Host = server.URL + "/"
	remote.CacheRoot = t.TempDir()
	remote.Cache = NewDataCache(remote.CacheRoot, "classic-era-large-index")
	remote.MetadataWorkers = 1
	found, err := (&cdnArchiveLocator{remote: remote, archiveKeys: []string{archiveKey}, tailProbe: 4 << 10}).LocateArchives(map[string]struct{}{targetKey: {}})
	if err != nil {
		t.Fatal(err)
	}
	if got := found[targetKey]; got.Key != archiveKey || got.Size != 30000 || got.Offset != 60000 {
		t.Fatalf("archive entry = %#v", got)
	}

	footer, err := parseCDNIndexFooter(index)
	if err != nil {
		t.Fatal(err)
	}
	wantBytes := (4 << 10) + maxInt(footer.TailSize-(4<<10), 0) + footer.PageSize
	metrics := SnapshotHTTPMetrics()
	if metrics.UniquePayloadBytes != int64(wantBytes) || metrics.DuplicateBytes != 0 {
		t.Fatalf("payload metrics = %#v, want %d unique and zero duplicate", metrics, wantBytes)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ranges) != 3 || ranges[0] != "bytes=-4096" {
		t.Fatalf("ranges = %q, want 4 KiB suffix, missing TOC prefix, and candidate page", ranges)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestRealCDNIndexProbe(t *testing.T) {
	path := os.Getenv("WOWDATA_REAL_INDEX")
	key := os.Getenv("WOWDATA_REAL_EKEY")
	if path == "" || key == "" {
		t.Skip("WOWDATA_REAL_INDEX and WOWDATA_REAL_EKEY are not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	found, err := parseCDNIndexSelected(data, map[string]struct{}{key: {}}, "real-archive")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := found[key]; !ok {
		t.Fatalf("encoding key %s was not found", key)
	}
}
