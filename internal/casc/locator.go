package casc

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"wowdata/internal/resource"
)

const cdnIndexFooterSize = 28

// FileLocator identifies encoding keys that the CDN exposes as loose files.
// Its input and output are process-local; only the original index payload is
// persisted by the implementation.
type FileLocator interface {
	LocateFiles(encodingKeys map[string]struct{}) (map[string]struct{}, error)
}

// ArchiveLocator resolves encoding keys to archive byte ranges.
type ArchiveLocator interface {
	LocateArchives(encodingKeys map[string]struct{}) (map[string]ArchiveEntry, error)
}

type cdnIndexFooter struct {
	PageSize     int
	OffsetBytes  int
	SizeBytes    int
	KeyBytes     int
	ChecksumSize int
	EntryCount   int
	PageCount    int
	EntrySize    int
	TailSize     int
}

type cdnIndexTOC struct {
	footer   cdnIndexFooter
	lastKeys [][]byte
	hashes   [][]byte
}

type cdnFileLocator struct {
	remote *CASCRemote
	key    string
	size   int
	stage  resource.StageID
	wave   string
}

type cdnArchiveLocator struct {
	remote      *CASCRemote
	archiveKeys []string
	tailProbe   int
	probeLimit  int
	adaptive    bool
	stage       resource.StageID
	wave        string
}

type cdnGroupLocator struct {
	remote      *CASCRemote
	key         string
	archiveKeys []string
	stage       resource.StageID
	wave        string
}

type archiveLocateResult struct {
	archive string
	entries map[string]ArchiveEntry
	err     error
}

func parseCDNIndexFooter(data []byte) (footer cdnIndexFooter, err error) {
	defer func() {
		if err == nil {
			resource.RecordCASCMetadata(len(data))
		}
	}()
	if len(data) < cdnIndexFooterSize {
		return cdnIndexFooter{}, fmt.Errorf("CDN index is shorter than its footer")
	}
	footerData := data[len(data)-cdnIndexFooterSize:]
	if footerData[8] != 1 || footerData[9] != 0 || footerData[10] != 0 {
		return cdnIndexFooter{}, fmt.Errorf("unsupported CDN index footer version=%d reserved=%02x%02x", footerData[8], footerData[9], footerData[10])
	}
	pageSize := int(footerData[11]) << 10
	offsetBytes := int(footerData[12])
	sizeBytes := int(footerData[13])
	keyBytes := int(footerData[14])
	checksumSize := int(footerData[15])
	entryCount := int(binary.LittleEndian.Uint32(footerData[16:20]))
	entrySize := keyBytes + offsetBytes + sizeBytes
	if pageSize <= 0 || keyBytes <= 0 || keyBytes > 32 || sizeBytes <= 0 || sizeBytes > 8 || offsetBytes < 0 || offsetBytes > 8 || checksumSize <= 0 || checksumSize > 16 || entrySize > pageSize {
		return cdnIndexFooter{}, fmt.Errorf("invalid CDN index footer dimensions page=%d key=%d offset=%d size=%d checksum=%d", pageSize, keyBytes, offsetBytes, sizeBytes, checksumSize)
	}
	entriesPerPage := pageSize / entrySize
	if entriesPerPage == 0 {
		return cdnIndexFooter{}, fmt.Errorf("CDN index entry does not fit a page")
	}
	pageCount := 0
	if entryCount > 0 {
		pageCount = (entryCount + entriesPerPage - 1) / entriesPerPage
	}
	tailSize := pageCount*(keyBytes+checksumSize) + cdnIndexFooterSize
	return cdnIndexFooter{
		PageSize: pageSize, OffsetBytes: offsetBytes, SizeBytes: sizeBytes,
		KeyBytes: keyBytes, ChecksumSize: checksumSize, EntryCount: entryCount,
		PageCount: pageCount, EntrySize: entrySize, TailSize: tailSize,
	}, nil
}

func parseCDNIndexTOC(tail []byte) (toc cdnIndexTOC, err error) {
	defer func() {
		if err == nil {
			resource.RecordCASCMetadata(len(tail))
		}
	}()
	footer, err := parseCDNIndexFooter(tail)
	if err != nil {
		return cdnIndexTOC{}, err
	}
	if len(tail) < footer.TailSize {
		return cdnIndexTOC{}, fmt.Errorf("CDN index tail is truncated: got %d want at least %d", len(tail), footer.TailSize)
	}
	data := tail[len(tail)-footer.TailSize:]
	lastKeys := make([][]byte, footer.PageCount)
	pos := 0
	for i := range lastKeys {
		lastKeys[i] = append([]byte(nil), data[pos:pos+footer.KeyBytes]...)
		pos += footer.KeyBytes
		if i > 0 && bytes.Compare(lastKeys[i-1], lastKeys[i]) > 0 {
			return cdnIndexTOC{}, fmt.Errorf("CDN index TOC keys are not sorted at page %d", i)
		}
	}
	hashes := make([][]byte, footer.PageCount)
	for i := range hashes {
		hashes[i] = append([]byte(nil), data[pos:pos+footer.ChecksumSize]...)
		pos += footer.ChecksumSize
	}
	return cdnIndexTOC{footer: footer, lastKeys: lastKeys, hashes: hashes}, nil
}

func (toc cdnIndexTOC) candidatePage(key []byte) (int, bool) {
	if len(key) < toc.footer.KeyBytes || len(toc.lastKeys) == 0 {
		return 0, false
	}
	key = key[:toc.footer.KeyBytes]
	page := sort.Search(len(toc.lastKeys), func(i int) bool {
		return bytes.Compare(toc.lastKeys[i], key) >= 0
	})
	return page, page < len(toc.lastKeys)
}

func (toc cdnIndexTOC) verifyPage(page int, data []byte) error {
	if page < 0 || page >= toc.footer.PageCount {
		return fmt.Errorf("CDN index page %d is out of range", page)
	}
	if len(data) != toc.footer.PageSize {
		return fmt.Errorf("CDN index page %d has %d bytes, want %d", page, len(data), toc.footer.PageSize)
	}
	sum := md5.Sum(data)
	if !bytes.Equal(sum[:toc.footer.ChecksumSize], toc.hashes[page]) {
		return fmt.Errorf("CDN index page %d checksum mismatch", page)
	}
	return nil
}

func parseCDNIndexPage(toc cdnIndexTOC, page int, data []byte, wanted map[string]struct{}, archiveKey string) (entries map[string]ArchiveEntry, err error) {
	defer func() {
		if err == nil {
			resource.RecordCASCMetadata(len(data))
		}
	}()
	if err := toc.verifyPage(page, data); err != nil {
		return nil, err
	}
	entries = make(map[string]ArchiveEntry)
	remaining := toc.footer.EntryCount - page*(toc.footer.PageSize/toc.footer.EntrySize)
	if remaining <= 0 {
		return entries, nil
	}
	pageEntries := toc.footer.PageSize / toc.footer.EntrySize
	if remaining < pageEntries {
		pageEntries = remaining
	}
	for i, pos := 0, 0; i < pageEntries && pos+toc.footer.EntrySize <= len(data); i, pos = i+1, pos+toc.footer.EntrySize {
		keyBytes := data[pos : pos+toc.footer.KeyBytes]
		if isZeroKey(keyBytes) {
			break
		}
		key := hex.EncodeToString(keyBytes)
		if _, ok := wanted[key]; !ok {
			continue
		}
		sizePos := pos + toc.footer.KeyBytes
		offsetPos := sizePos + toc.footer.SizeBytes
		size, err := readBigEndianUint(data[sizePos:offsetPos])
		if err != nil || size > uint64(^uint32(0)>>1) {
			return nil, fmt.Errorf("CDN index entry %s has invalid size", key)
		}
		offset, err := readBigEndianUint(data[offsetPos : offsetPos+toc.footer.OffsetBytes])
		if err != nil || offset > uint64(^uint32(0)>>1) {
			return nil, fmt.Errorf("CDN index entry %s has invalid offset", key)
		}
		entries[key] = ArchiveEntry{Key: archiveKey, Size: int32(size), Offset: int32(offset)}
	}
	return entries, nil
}

func parseCDNGroupIndexPage(toc cdnIndexTOC, page int, data []byte, wanted map[string]struct{}, archiveKeys []string) (map[string]ArchiveEntry, error) {
	ordinalBytes := toc.footer.OffsetBytes - 4
	if ordinalBytes < 1 || ordinalBytes > 2 {
		return nil, fmt.Errorf("CDN group index offset width is %d, want 5 or 6", toc.footer.OffsetBytes)
	}
	if err := toc.verifyPage(page, data); err != nil {
		return nil, err
	}
	entries := make(map[string]ArchiveEntry)
	remaining := toc.footer.EntryCount - page*(toc.footer.PageSize/toc.footer.EntrySize)
	if remaining <= 0 {
		return entries, nil
	}
	pageEntries := toc.footer.PageSize / toc.footer.EntrySize
	if remaining < pageEntries {
		pageEntries = remaining
	}
	for i, pos := 0, 0; i < pageEntries && pos+toc.footer.EntrySize <= len(data); i, pos = i+1, pos+toc.footer.EntrySize {
		keyBytes := data[pos : pos+toc.footer.KeyBytes]
		if isZeroKey(keyBytes) {
			break
		}
		key := hex.EncodeToString(keyBytes)
		if _, ok := wanted[key]; !ok {
			continue
		}
		sizePos := pos + toc.footer.KeyBytes
		archivePos := sizePos + toc.footer.SizeBytes
		offsetPos := archivePos + ordinalBytes
		size, sizeErr := readBigEndianUint(data[sizePos:archivePos])
		archiveOrdinal, archiveErr := readBigEndianUint(data[archivePos:offsetPos])
		offset, offsetErr := readBigEndianUint(data[offsetPos : offsetPos+4])
		if sizeErr != nil || archiveErr != nil || offsetErr != nil || size > uint64(^uint32(0)>>1) || offset > uint64(^uint32(0)>>1) {
			return nil, fmt.Errorf("CDN group index entry %s has invalid dimensions", key)
		}
		if archiveOrdinal >= uint64(len(archiveKeys)) {
			return nil, fmt.Errorf("CDN group index entry %s references archive ordinal %d of %d", key, archiveOrdinal, len(archiveKeys))
		}
		entries[key] = ArchiveEntry{Key: archiveKeys[archiveOrdinal], Size: int32(size), Offset: int32(offset)}
	}
	resource.RecordCASCMetadata(len(data))
	return entries, nil
}

func (l *cdnGroupLocator) LocateArchives(encodingKeys map[string]struct{}) (map[string]ArchiveEntry, error) {
	resolved := make(map[string]ArchiveEntry)
	if l == nil || l.remote == nil || l.key == "" || len(l.archiveKeys) == 0 || len(encodingKeys) == 0 {
		return resolved, nil
	}
	ctx := context.Background()
	reader := &cdnArchiveLocator{remote: l.remote, tailProbe: archiveIndexTailProbe(l.remote.CDNConfig)}
	toc, full, tail, err := reader.loadArchiveTOC(ctx, l.key)
	if err != nil {
		// archive-group files are commonly client-local merged indexes. CDN
		// hosts legitimately return 404/403; treat that as an unsupported
		// direct locator and let the archive-index fallback continue.
		if strings.Contains(err.Error(), "HTTP 404") || strings.Contains(err.Error(), "HTTP 403") {
			return resolved, nil
		}
		return nil, fmt.Errorf("archive group %s: %w", l.key, err)
	}
	if toc.footer.OffsetBytes < 5 || toc.footer.OffsetBytes > 6 {
		return nil, fmt.Errorf("archive group %s has offset width %d, want 5 or 6", l.key, toc.footer.OffsetBytes)
	}
	pages := candidatePages(toc, encodingKeys)
	for page := range pages {
		var pageData []byte
		if len(full) > 0 {
			start := page * toc.footer.PageSize
			end := start + toc.footer.PageSize
			if start < 0 || end > len(full) {
				return nil, fmt.Errorf("archive group %s page %d exceeds cached index", l.key, page)
			}
			pageData = full[start:end]
		} else {
			pageData, err = reader.loadArchivePage(ctx, l.key, toc, page, tail)
			if err != nil {
				return nil, err
			}
		}
		entries, parseErr := parseCDNGroupIndexPage(toc, page, pageData, encodingKeys, l.archiveKeys)
		if parseErr != nil {
			return nil, parseErr
		}
		for key, entry := range entries {
			resolved[key] = entry
		}
	}
	return resolved, nil
}

func readBigEndianUint(data []byte) (uint64, error) {
	if len(data) > 8 {
		return 0, fmt.Errorf("integer is wider than 8 bytes")
	}
	var value uint64
	for _, b := range data {
		value = value<<8 | uint64(b)
	}
	return value, nil
}

func parseCDNIndexSelected(data []byte, wanted map[string]struct{}, archiveKey string) (map[string]ArchiveEntry, error) {
	toc, err := parseCDNIndexTOC(data)
	if err != nil {
		return nil, err
	}
	dataBytes := toc.footer.PageCount * toc.footer.PageSize
	if len(data) < dataBytes+toc.footer.TailSize {
		return nil, fmt.Errorf("CDN index body is truncated: got %d want %d", len(data), dataBytes+toc.footer.TailSize)
	}
	entries := make(map[string]ArchiveEntry)
	pages := make(map[int]struct{})
	for key := range wanted {
		raw, decodeErr := hex.DecodeString(key)
		if decodeErr != nil {
			return nil, fmt.Errorf("invalid encoding key %q: %w", key, decodeErr)
		}
		if page, ok := toc.candidatePage(raw); ok {
			pages[page] = struct{}{}
		}
	}
	for page := range pages {
		start := page * toc.footer.PageSize
		found, parseErr := parseCDNIndexPage(toc, page, data[start:start+toc.footer.PageSize], wanted, archiveKey)
		if parseErr != nil {
			return nil, parseErr
		}
		for key, entry := range found {
			entries[key] = entry
		}
	}
	return entries, nil
}

func (l *cdnFileLocator) LocateFiles(encodingKeys map[string]struct{}) (map[string]struct{}, error) {
	found := make(map[string]struct{})
	if l == nil || l.remote == nil || l.key == "" || len(encodingKeys) == 0 {
		return found, nil
	}
	for key := range encodingKeys {
		if _, err := hex.DecodeString(key); err != nil {
			return nil, fmt.Errorf("invalid encoding key %q: %w", key, err)
		}
	}

	ctx := context.Background()
	tocStage := resource.StartStage("casc-file-index-toc", resource.StageOptions{ParentID: l.stage, Wave: l.wave, DependsOn: []resource.StageDependency{resource.Dependency(l.stage, resource.StageRelationHard)}})
	toc, full, tail, err := l.loadFileTOC(ctx)
	if err != nil {
		resource.FinishStage(tocStage, err)
		fallbackStage := resource.StartStage("casc-file-index-full-fallback", resource.StageOptions{ParentID: l.stage, Wave: l.wave, Condition: "toc-error", DependsOn: []resource.StageDependency{resource.Dependency(tocStage, resource.StageRelationFallback)}})
		found, fallbackErr := l.locateFilesFull(encodingKeys)
		resource.FinishStage(fallbackStage, fallbackErr)
		return found, fallbackErr
	}
	resource.FinishStage(tocStage, nil)
	if len(full) > 0 {
		pagesStage := resource.StartStage("casc-file-index-pages", resource.StageOptions{ParentID: l.stage, Wave: l.wave, Condition: "full-index-cache", DependsOn: []resource.StageDependency{resource.Dependency(tocStage, resource.StageRelationHard)}})
		entries, parseErr := parseCDNIndexSelected(full, encodingKeys, "")
		if parseErr != nil {
			resource.FinishStage(pagesStage, parseErr)
			return nil, parseErr
		}
		for key := range entries {
			found[key] = struct{}{}
		}
		resource.FinishStage(pagesStage, nil)
		return found, nil
	}

	pages := candidatePages(toc, encodingKeys)
	pageOrder := make([]int, 0, len(pages))
	for page := range pages {
		pageOrder = append(pageOrder, page)
	}
	sort.Ints(pageOrder)
	type pageResult struct {
		page int
		data []byte
		err  error
	}
	pageCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	pagesStage := resource.StartStage("casc-file-index-pages", resource.StageOptions{ParentID: l.stage, Wave: l.wave, DependsOn: []resource.StageDependency{resource.Dependency(tocStage, resource.StageRelationHard)}})
	workers := l.remote.ResourcePlan().MetadataWorkers
	if workers > len(pageOrder) {
		workers = len(pageOrder)
	}
	jobs := make(chan int)
	results := make(chan pageResult)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for page := range jobs {
				data, pageErr := l.loadFilePage(pageCtx, toc, page, tail)
				select {
				case results <- pageResult{page: page, data: data, err: pageErr}:
				case <-pageCtx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, page := range pageOrder {
			select {
			case jobs <- page:
			case <-pageCtx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	var pageErr error
	for result := range results {
		if result.err != nil {
			if pageErr == nil {
				pageErr = result.err
				cancel()
			}
			continue
		}
		entries, parseErr := parseCDNIndexPage(toc, result.page, result.data, encodingKeys, "")
		if parseErr != nil {
			if pageErr == nil {
				pageErr = parseErr
				cancel()
			}
			continue
		}
		for key := range entries {
			found[key] = struct{}{}
		}
	}
	if pageErr != nil {
		resource.FinishStage(pagesStage, pageErr)
		fallbackStage := resource.StartStage("casc-file-index-full-fallback", resource.StageOptions{ParentID: l.stage, Wave: l.wave, Condition: "page-error", DependsOn: []resource.StageDependency{resource.Dependency(pagesStage, resource.StageRelationFallback)}})
		found, fallbackErr := l.locateFilesFull(encodingKeys)
		resource.FinishStage(fallbackStage, fallbackErr)
		return found, fallbackErr
	}
	resource.FinishStage(pagesStage, nil)
	return found, nil
}

func (l *cdnFileLocator) locateFilesFull(encodingKeys map[string]struct{}) (map[string]struct{}, error) {
	found := make(map[string]struct{})
	data, err := l.remote.downloadArchiveIndex(l.key)
	if err != nil {
		return nil, err
	}
	entries, err := parseCDNIndexSelected(data, encodingKeys, "")
	if err != nil {
		return nil, err
	}
	for key := range entries {
		found[key] = struct{}{}
	}
	return found, nil
}

func (l *cdnFileLocator) loadFileTOC(ctx context.Context) (cdnIndexTOC, []byte, []byte, error) {
	tailName := l.key + ".index.tail"
	if full, err := l.remote.Cache.GetObject(l.key + ".index"); err == nil && len(full) > 0 {
		toc, parseErr := parseCDNIndexTOC(full)
		if parseErr != nil {
			return cdnIndexTOC{}, nil, nil, parseErr
		}
		return toc, full, nil, nil
	}
	if tail, err := l.remote.Cache.GetObject(tailName); err == nil && len(tail) > 0 {
		toc, parseErr := parseCDNIndexTOC(tail)
		return toc, nil, tail, parseErr
	}
	if l.size <= cdnIndexFooterSize {
		return cdnIndexTOC{}, nil, nil, fmt.Errorf("file index size is unavailable")
	}

	lock, err := l.remote.Cache.AcquireObjectLock(tailName, 5*time.Minute)
	if err != nil {
		return cdnIndexTOC{}, nil, nil, err
	}
	defer lock.Release()
	if tail, cacheErr := l.remote.Cache.GetObject(tailName); cacheErr == nil && len(tail) > 0 {
		toc, parseErr := parseCDNIndexTOC(tail)
		return toc, nil, tail, parseErr
	}

	probe := 4 << 10
	if probe > l.size {
		probe = l.size
	}
	tail, err := l.fetchFileIndexRange(ctx, l.size-probe, probe)
	if err != nil {
		return cdnIndexTOC{}, nil, nil, err
	}
	footer, err := parseCDNIndexFooter(tail)
	if err != nil {
		return cdnIndexTOC{}, nil, nil, err
	}
	if footer.TailSize > l.size {
		return cdnIndexTOC{}, nil, nil, fmt.Errorf("file index TOC is larger than the configured index size")
	}
	if footer.TailSize > len(tail) {
		prefixLength := footer.TailSize - len(tail)
		prefix, fetchErr := l.fetchFileIndexRange(ctx, l.size-footer.TailSize, prefixLength)
		if fetchErr != nil {
			return cdnIndexTOC{}, nil, nil, fetchErr
		}
		tail = append(prefix, tail...)
	}
	toc, err := parseCDNIndexTOC(tail)
	if err != nil {
		return cdnIndexTOC{}, nil, nil, err
	}
	if err := l.remote.Cache.StoreObject(tailName, tail); err != nil {
		return cdnIndexTOC{}, nil, nil, err
	}
	return toc, nil, tail, nil
}

func (l *cdnFileLocator) loadFilePage(ctx context.Context, toc cdnIndexTOC, page int, tail []byte) ([]byte, error) {
	name := fmt.Sprintf("%s.index.page-%d", l.key, page)
	if pageData, err := l.remote.Cache.GetObject(name); err == nil && len(pageData) > 0 {
		return pageData, nil
	}
	pageStart := page * toc.footer.PageSize
	pageEnd := pageStart + toc.footer.PageSize
	tailStart := l.size - len(tail)
	if len(tail) > 0 && tailStart < pageEnd {
		overlapStart := tailStart
		if overlapStart < pageStart {
			overlapStart = pageStart
		}
		prefixLength := overlapStart - pageStart
		pageData := make([]byte, toc.footer.PageSize)
		if prefixLength > 0 {
			prefixName := fmt.Sprintf("%s.index.page-%d.prefix-%d", l.key, page, prefixLength)
			prefix, prefixErr := l.remote.Cache.GetObject(prefixName)
			if prefixErr != nil || len(prefix) != prefixLength {
				lock, lockErr := l.remote.Cache.AcquireObjectLock(prefixName, 5*time.Minute)
				if lockErr != nil {
					return nil, lockErr
				}
				prefix, prefixErr = l.remote.Cache.GetObject(prefixName)
				if prefixErr != nil || len(prefix) != prefixLength {
					prefix, prefixErr = l.fetchFileIndexRange(ctx, pageStart, prefixLength)
					if prefixErr == nil {
						prefixErr = l.remote.Cache.StoreObject(prefixName, prefix)
					}
				}
				_ = lock.Release()
				if prefixErr != nil {
					return nil, prefixErr
				}
			}
			copy(pageData, prefix)
		}
		tailOffset := overlapStart - tailStart
		copy(pageData[prefixLength:], tail[tailOffset:tailOffset+pageEnd-overlapStart])
		return pageData, nil
	}

	lock, err := l.remote.Cache.AcquireObjectLock(name, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	if pageData, cacheErr := l.remote.Cache.GetObject(name); cacheErr == nil && len(pageData) > 0 {
		return pageData, nil
	}
	pageData, err := l.fetchFileIndexRange(ctx, pageStart, toc.footer.PageSize)
	if err != nil {
		return nil, err
	}
	if err := toc.verifyPage(page, pageData); err != nil {
		return nil, err
	}
	if err := l.remote.Cache.StoreObject(name, pageData); err != nil {
		return nil, err
	}
	return pageData, nil
}

func (l *cdnFileLocator) fetchFileIndexRange(ctx context.Context, offset, length int) ([]byte, error) {
	cdnFile := FormatCDNKey(l.key) + ".index"
	var data []byte
	var err error
	if l.remote.fetchPartial != nil {
		data, err = l.remote.fetchPartial(cdnFile, offset, length)
	} else {
		url := l.remote.Host + "data/" + cdnFile
		release, acquireErr := l.remote.ResourceScheduler().Acquire(ctx, resource.MetadataPool)
		if acquireErr != nil {
			return nil, acquireErr
		}
		data, err = httpRangeSingleContext(ctx, url, offset, offset+length-1)
		release()
	}
	if err != nil {
		return nil, err
	}
	if len(data) == l.size && length != l.size {
		if storeErr := l.remote.Cache.StoreObject(l.key+".index", data); storeErr != nil {
			return nil, storeErr
		}
		return nil, fmt.Errorf("file index server returned the full object for range %d+%d", offset, length)
	}
	if len(data) != length {
		return nil, fmt.Errorf("file index range %s %d+%d returned %d bytes", l.key, offset, length, len(data))
	}
	return data, nil
}

func (l *cdnArchiveLocator) LocateArchives(encodingKeys map[string]struct{}) (map[string]ArchiveEntry, error) {
	resolved := make(map[string]ArchiveEntry)
	if l == nil || l.remote == nil || len(encodingKeys) == 0 || len(l.archiveKeys) == 0 {
		return resolved, nil
	}
	for key := range encodingKeys {
		if _, err := hex.DecodeString(key); err != nil {
			return nil, fmt.Errorf("invalid encoding key %q: %w", key, err)
		}
	}

	archiveKeys := l.archiveKeys
	if l.probeLimit > 0 && len(archiveKeys) > l.probeLimit {
		archiveKeys = archiveKeys[:l.probeLimit]
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workers := l.remote.ResourcePlan().MetadataWorkers
	if workers > len(archiveKeys) {
		workers = len(archiveKeys)
	}
	jobs := make(chan string)
	results := make(chan archiveLocateResult)
	unresolved := make(map[string]struct{}, len(encodingKeys))
	for key := range encodingKeys {
		unresolved[key] = struct{}{}
	}
	var unresolvedMu sync.Mutex
	snapshotUnresolved := func() map[string]struct{} {
		unresolvedMu.Lock()
		defer unresolvedMu.Unlock()
		result := make(map[string]struct{}, len(unresolved))
		for key := range unresolved {
			result[key] = struct{}{}
		}
		return result
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case archive, ok := <-jobs:
					if !ok {
						return
					}
					wanted := snapshotUnresolved()
					if len(wanted) == 0 {
						return
					}
					entries, err := l.locateArchive(ctx, archive, wanted)
					select {
					case results <- archiveLocateResult{archive: archive, entries: entries, err: err}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	go func() {
		for _, archive := range archiveKeys {
			select {
			case jobs <- archive:
			case <-ctx.Done():
				close(jobs)
				return
			}
		}
		close(jobs)
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	var failures []string
	attempts := 0
	for result := range results {
		attempts++
		if result.err != nil {
			failures = append(failures, result.archive+": "+result.err.Error())
		}
		unresolvedMu.Lock()
		for key, entry := range result.entries {
			if _, ok := unresolved[key]; ok {
				resolved[key] = entry
				delete(unresolved, key)
			}
		}
		complete := len(unresolved) == 0
		unresolvedMu.Unlock()
		if complete {
			cancel()
		}
	}
	sort.Strings(failures)
	if len(resolved) == 0 && attempts > 0 && len(failures) == attempts {
		return nil, fmt.Errorf("all archive locator requests failed: %s", strings.Join(failures, "; "))
	}
	if len(resolved) < len(encodingKeys) && len(archiveKeys) < len(l.archiveKeys) {
		if !l.adaptive {
			return resolved, fmt.Errorf("archive locator probe budget exhausted after %d of %d indexes (%d of %d keys resolved)", len(archiveKeys), len(l.archiveKeys), len(resolved), len(encodingKeys))
		}
		// A bounded first wave keeps ordinary point queries cheap when the
		// direct locators are available. If they miss, correctness takes
		// precedence and the remaining archive indexes are probed adaptively.
		remaining := make(map[string]struct{}, len(encodingKeys)-len(resolved))
		for key := range encodingKeys {
			if _, ok := resolved[key]; !ok {
				remaining[key] = struct{}{}
			}
		}
		tail := *l
		tail.archiveKeys = append([]string(nil), l.archiveKeys[len(archiveKeys):]...)
		tail.probeLimit = 0
		tailResolved, tailErr := tail.LocateArchives(remaining)
		for key, entry := range tailResolved {
			resolved[key] = entry
		}
		if tailErr != nil {
			return resolved, tailErr
		}
	}
	if len(resolved) < len(encodingKeys) {
		return resolved, fmt.Errorf("archive locator exhausted %d indexes (%d of %d keys resolved)", len(l.archiveKeys), len(resolved), len(encodingKeys))
	}
	return resolved, nil
}

func (l *cdnArchiveLocator) locateArchive(ctx context.Context, archive string, wanted map[string]struct{}) (map[string]ArchiveEntry, error) {
	tocStage := resource.StartStage("casc-archive-toc", resource.StageOptions{ParentID: l.stage, Wave: l.wave, Instance: archive, DependsOn: []resource.StageDependency{resource.Dependency(l.stage, resource.StageRelationHard)}})
	toc, full, tail, err := l.loadArchiveTOC(ctx, archive)
	if err != nil {
		resource.FinishStage(tocStage, err)
		return nil, err
	}
	resource.FinishStage(tocStage, nil)
	pagesStage := resource.StartStage("casc-archive-pages", resource.StageOptions{ParentID: l.stage, Wave: l.wave, Instance: archive, DependsOn: []resource.StageDependency{resource.Dependency(tocStage, resource.StageRelationHard)}})
	if len(full) > 0 {
		entries, parseErr := parseCDNIndexSelected(full, wanted, archive)
		resource.FinishStage(pagesStage, parseErr)
		return entries, parseErr
	}

	found := make(map[string]ArchiveEntry)
	pages := candidatePages(toc, wanted)
	pageOrder := make([]int, 0, len(pages))
	for page := range pages {
		pageOrder = append(pageOrder, page)
	}
	sort.Ints(pageOrder)
	for _, page := range pageOrder {
		if err := ctx.Err(); err != nil {
			resource.FinishStage(pagesStage, err)
			return found, err
		}
		pageData, err := l.loadArchivePage(ctx, archive, toc, page, tail)
		if err != nil {
			resource.FinishStage(pagesStage, err)
			return found, err
		}
		entries, err := parseCDNIndexPage(toc, page, pageData, wanted, archive)
		if err != nil {
			resource.FinishStage(pagesStage, err)
			return found, err
		}
		for key, entry := range entries {
			found[key] = entry
		}
		if len(found) == len(wanted) {
			break
		}
	}
	resource.FinishStage(pagesStage, nil)
	return found, nil
}

func candidatePages(toc cdnIndexTOC, wanted map[string]struct{}) map[int]struct{} {
	pages := make(map[int]struct{})
	for key := range wanted {
		raw, err := hex.DecodeString(key)
		if err == nil {
			if page, ok := toc.candidatePage(raw); ok {
				pages[page] = struct{}{}
			}
		}
	}
	return pages
}

func (l *cdnArchiveLocator) loadArchiveTOC(ctx context.Context, archive string) (cdnIndexTOC, []byte, []byte, error) {
	tailName := archive + ".index.tail"
	tail, cacheErr := l.remote.Cache.GetObject(tailName)
	if cacheErr == nil && len(tail) > 0 {
		toc, err := parseCDNIndexTOC(tail)
		return toc, nil, tail, err
	}
	full, fullErr := l.remote.Cache.GetObject(archive + ".index")
	if fullErr == nil && len(full) > 0 {
		toc, err := parseCDNIndexTOC(full)
		if err != nil {
			return cdnIndexTOC{}, nil, nil, err
		}
		// The complete raw index already contains both the TOC and candidate
		// pages. Persisting slices beside it duplicates payload bytes and turns
		// the first point lookup after a full diagnose into thousands of writes.
		return toc, full, nil, nil
	}

	lock, err := l.remote.Cache.AcquireObjectLock(tailName, 5*time.Minute)
	if err != nil {
		return cdnIndexTOC{}, nil, nil, err
	}
	defer lock.Release()
	tail, cacheErr = l.remote.Cache.GetObject(tailName)
	if cacheErr != nil || len(tail) == 0 {
		url := l.remote.Host + "data/" + FormatCDNKey(archive) + ".index"
		probe := l.tailProbe
		if probe <= 0 {
			probe = 4 << 10
		}
		release, acquireErr := l.remote.ResourceScheduler().Acquire(ctx, resource.MetadataPool)
		if acquireErr != nil {
			return cdnIndexTOC{}, nil, nil, acquireErr
		}
		initial, rangeErr := httpRangeSuffixInfoContext(ctx, url, probe)
		release()
		if rangeErr != nil {
			cacheErr = rangeErr
		} else {
			cacheErr = nil
			tail = initial.data
			footer, footerErr := parseCDNIndexFooter(tail)
			if footerErr != nil {
				cacheErr = footerErr
			} else if footer.TailSize > initial.total {
				cacheErr = fmt.Errorf("archive index TOC is larger than the response object")
			} else {
				if footer.TailSize > len(tail) {
					prefixLength := footer.TailSize - len(tail)
					prefixStart := initial.start - prefixLength
					if prefixStart < 0 {
						cacheErr = fmt.Errorf("archive index TOC prefix starts before the object")
					} else {
						prefix, prefixErr := l.fetchArchiveIndexRange(ctx, archive, prefixStart, prefixLength)
						if prefixErr != nil {
							cacheErr = prefixErr
						} else {
							tail = append(prefix, tail...)
						}
					}
				}
			}
		}
		if cacheErr == nil && ctx.Err() == nil {
			cacheErr = l.remote.Cache.StoreObject(tailName, tail)
		}
	}
	if cacheErr != nil {
		return cdnIndexTOC{}, nil, nil, cacheErr
	}
	toc, err := parseCDNIndexTOC(tail)
	return toc, nil, tail, err
}

func (l *cdnArchiveLocator) loadArchivePage(ctx context.Context, archive string, toc cdnIndexTOC, page int, tail []byte) ([]byte, error) {
	name := fmt.Sprintf("%s.index.page-%d", archive, page)
	pageData, err := l.remote.Cache.GetObject(name)
	if err == nil && len(pageData) > 0 {
		return pageData, nil
	}
	pageStart := page * toc.footer.PageSize
	pageEnd := pageStart + toc.footer.PageSize
	indexSize := toc.footer.PageCount*toc.footer.PageSize + toc.footer.TailSize
	tailStart := indexSize - len(tail)
	if len(tail) > 0 && tailStart < pageEnd {
		overlapStart := tailStart
		if overlapStart < pageStart {
			overlapStart = pageStart
		}
		prefixLength := overlapStart - pageStart
		pageData = make([]byte, toc.footer.PageSize)
		if prefixLength > 0 {
			prefixName := fmt.Sprintf("%s.index.page-%d.prefix-%d", archive, page, prefixLength)
			prefix, prefixErr := l.remote.Cache.GetObject(prefixName)
			if prefixErr != nil || len(prefix) != prefixLength {
				lock, lockErr := l.remote.Cache.AcquireObjectLock(prefixName, 5*time.Minute)
				if lockErr != nil {
					return nil, lockErr
				}
				prefix, prefixErr = l.remote.Cache.GetObject(prefixName)
				if prefixErr != nil || len(prefix) != prefixLength {
					prefix, prefixErr = l.fetchArchiveIndexRange(ctx, archive, pageStart, prefixLength)
					if prefixErr == nil {
						prefixErr = l.remote.Cache.StoreObject(prefixName, prefix)
					}
				}
				_ = lock.Release()
				if prefixErr != nil {
					return nil, prefixErr
				}
			}
			copy(pageData, prefix)
		}
		tailOffset := overlapStart - tailStart
		copy(pageData[prefixLength:], tail[tailOffset:tailOffset+pageEnd-overlapStart])
		return pageData, nil
	}
	lock, err := l.remote.Cache.AcquireObjectLock(name, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	pageData, err = l.remote.Cache.GetObject(name)
	if err == nil && len(pageData) > 0 {
		return pageData, nil
	}
	pageData, err = l.fetchArchiveIndexRange(ctx, archive, pageStart, toc.footer.PageSize)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := l.remote.Cache.StoreObject(name, pageData); err != nil {
		return nil, err
	}
	return pageData, nil
}

func (l *cdnArchiveLocator) fetchArchiveIndexRange(ctx context.Context, archive string, offset, length int) ([]byte, error) {
	cdnFile := FormatCDNKey(archive) + ".index"
	var data []byte
	var err error
	if l.remote.fetchPartial != nil {
		data, err = l.remote.fetchPartial(cdnFile, offset, length)
	} else {
		url := l.remote.Host + "data/" + cdnFile
		release, acquireErr := l.remote.ResourceScheduler().Acquire(ctx, resource.MetadataPool)
		if acquireErr != nil {
			return nil, acquireErr
		}
		data, err = httpRangeSingleContext(ctx, url, offset, offset+length-1)
		release()
	}
	if err != nil {
		return nil, err
	}
	if len(data) != length {
		return nil, fmt.Errorf("archive index range %s %d+%d returned %d bytes", archive, offset, length, len(data))
	}
	return data, nil
}

func httpRangeSuffix(url string, length int) ([]byte, error) {
	return httpRangeSuffixContext(context.Background(), url, length)
}

func httpRangeSuffixContext(ctx context.Context, url string, length int) ([]byte, error) {
	result, err := httpRangeSuffixInfoContext(ctx, url, length)
	return result.data, err
}

type suffixRangeInfo struct {
	data  []byte
	start int
	total int
}

func httpRangeSuffixInfoContext(ctx context.Context, url string, length int) (suffixRangeInfo, error) {
	if length <= 0 {
		return suffixRangeInfo{}, fmt.Errorf("suffix range length must be positive")
	}
	var lastErr error
	for attempt := 0; attempt < httpDownloadAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return suffixRangeInfo{}, err
		}
		req.Header.Set("Range", "bytes=-"+strconv.Itoa(length))
		resp, err := httpClient.Do(req)
		if err == nil {
			data, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil && (resp.StatusCode == http.StatusPartialContent || resp.StatusCode == http.StatusOK) {
				start, total := 0, len(data)
				if resp.StatusCode == http.StatusPartialContent {
					var end int
					if _, scanErr := fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); scanErr != nil || start < 0 || end < start || end-start+1 != len(data) || total <= end {
						lastErr = fmt.Errorf("invalid Content-Range %q for suffix range %d", resp.Header.Get("Content-Range"), length)
						continue
					}
				}
				return suffixRangeInfo{data: data, start: start, total: total}, nil
			}
			if readErr != nil {
				lastErr = readErr
			} else {
				lastErr = fmt.Errorf("HTTP %d for suffix range %d", resp.StatusCode, length)
				if permanentHTTPStatus(resp.StatusCode) {
					return suffixRangeInfo{}, lastErr
				}
			}
		} else {
			lastErr = err
		}
		if attempt+1 < httpDownloadAttempts {
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}
	return suffixRangeInfo{}, lastErr
}

func httpRangeSingleContext(ctx context.Context, url string, start, end int) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < httpDownloadAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		resp, err := httpClient.Do(req)
		if err == nil {
			data, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil && (resp.StatusCode == http.StatusPartialContent || resp.StatusCode == http.StatusOK) {
				return data, nil
			}
			if readErr != nil {
				lastErr = readErr
			} else {
				lastErr = fmt.Errorf("HTTP %d for range %d-%d", resp.StatusCode, start, end)
				if permanentHTTPStatus(resp.StatusCode) {
					return nil, lastErr
				}
			}
		} else {
			lastErr = err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt+1 < httpDownloadAttempts {
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}
	return nil, lastErr
}

func permanentHTTPStatus(status int) bool {
	return status >= 400 && status < 500 && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests
}

func archiveIndexTailProbe(config map[string]string) int {
	maxIndexBytes := 0
	for _, raw := range strings.Fields(config["archivesIndexSize"]) {
		value, err := strconv.Atoi(raw)
		if err == nil && value > maxIndexBytes {
			maxIndexBytes = value
		}
	}
	if maxIndexBytes <= cdnIndexFooterSize {
		return 64 << 10
	}
	// CDN index pages have at most 16-byte keys and 16-byte checksums in
	// current supported formats. Config sizes can be reordered relative to
	// archive keys, but their maximum still bounds every TOC in the set.
	pageCount := (maxIndexBytes + (4 << 10) - 1) / (4 << 10)
	probe := pageCount*32 + cdnIndexFooterSize
	return (probe + 4095) &^ 4095
}
