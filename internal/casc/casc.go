package casc

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"

	"wowdata/internal/blte"
	"wowdata/internal/resource"
)

const (
	encMagic  = 0x4E45
	rootMagic = 0x4D465354
)

type RootType struct {
	ContentFlags ContentFlag
	LocaleFlags  LocaleFlag
}

type RootEntry struct {
	TypeIndex  int
	ContentKey string
}

type EncodingEntry struct {
	Key  string
	Size int64
}

type CASCSource struct {
	EncodingEntries map[string]EncodingEntry
	RootTypes       []RootType
	RootEntries     map[uint32][]RootEntry
	Locale          LocaleFlag
	Archives        map[string]ArchiveEntry
	BuildConfig     map[string]string
	CDNConfig       map[string]string
}

type ArchiveEntry struct {
	Key    string
	Size   int32
	Offset int32
}

func NewCASCSource() *CASCSource {
	return &CASCSource{
		EncodingEntries: make(map[string]EncodingEntry),
		RootEntries:     make(map[uint32][]RootEntry),
		Archives:        make(map[string]ArchiveEntry),
		Locale:          LocaleZhCN,
	}
}

func (c *CASCSource) GetValidRootEntries() []uint32 {
	var entries []uint32
	for fdid, rootEntries := range c.RootEntries {
		for _, entry := range rootEntries {
			if entry.TypeIndex < len(c.RootTypes) {
				rt := c.RootTypes[entry.TypeIndex]
				if rt.LocaleFlags&c.Locale != 0 && rt.ContentFlags&ContentLowViolence == 0 {
					entries = append(entries, fdid)
					break
				}
			}
		}
	}
	return entries
}

func (c *CASCSource) FileExists(fdid uint32) bool {
	root, ok := c.RootEntries[fdid]
	if !ok {
		return false
	}
	for _, entry := range root {
		if entry.TypeIndex < len(c.RootTypes) {
			rt := c.RootTypes[entry.TypeIndex]
			if rt.LocaleFlags&c.Locale != 0 && rt.ContentFlags&ContentLowViolence == 0 {
				return true
			}
		}
	}
	return false
}

func (c *CASCSource) GetFile(fdid uint32) (string, error) {
	_, encKey, err := c.ResolveFileKeys(fdid)
	return encKey, err
}

func (c *CASCSource) ResolveFileKeys(fdid uint32) (string, string, error) {
	root, ok := c.RootEntries[fdid]
	if !ok {
		return "", "", fmt.Errorf("fileDataID does not exist in root: %d", fdid)
	}

	var contentKey string
	for _, entry := range root {
		if entry.TypeIndex < len(c.RootTypes) {
			rt := c.RootTypes[entry.TypeIndex]
			if rt.LocaleFlags&c.Locale != 0 && rt.ContentFlags&ContentLowViolence == 0 {
				contentKey = entry.ContentKey
				break
			}
		}
	}
	if contentKey == "" {
		return "", "", fmt.Errorf("no root entry found for locale: %d", c.Locale)
	}

	enc, ok := c.EncodingEntries[contentKey]
	if !ok {
		return "", "", fmt.Errorf("no encoding entry found: %s", contentKey)
	}
	return contentKey, enc.Key, nil
}

func (c *CASCSource) GetEncodingKeyForContentKey(contentKey string) (string, error) {
	enc, ok := c.EncodingEntries[contentKey]
	if !ok {
		return "", fmt.Errorf("no encoding entry found: %s", contentKey)
	}
	return enc.Key, nil
}

func (c *CASCSource) GetEncodingSizeForContentKey(contentKey string) int64 {
	return c.EncodingEntries[contentKey].Size
}

func (c *CASCSource) GetFileEncodingInfo(fdid uint32) (*FileInfo, error) {
	contentKey, encKey, err := c.ResolveFileKeys(fdid)
	if err != nil {
		return nil, err
	}
	info := &FileInfo{
		FileDataID:  fdid,
		ContentKey:  contentKey,
		EncodingKey: encKey,
		Enc:         encKey,
		Size:        c.GetEncodingSizeForContentKey(contentKey),
	}
	if archive, ok := c.Archives[encKey]; ok {
		info.Archive = &FileArchiveInfo{Key: archive.Key, Offset: archive.Offset, Length: archive.Size}
	}
	return info, nil
}

func FormatCDNKey(key string) string {
	if len(key) < 4 {
		return key
	}
	return key[0:2] + "/" + key[2:4] + "/" + key
}

func (c *CASCSource) ParseRootFile(data []byte) (int, error) {
	reader, err := blte.NewReader(data)
	if err != nil {
		return 0, err
	}
	buf, err := reader.ReadAll()
	if err != nil {
		return 0, err
	}
	return c.parseRoot(buf)
}

// ParseRootFileSelected keeps only the requested FileDataIDs while still
// validating every block boundary in the root file.
func (c *CASCSource) ParseRootFileSelected(data []byte, fileDataIDs []uint32) (int, error) {
	return c.ParseRootFileSelectedWithWorkers(data, fileDataIDs, 1)
}

func (c *CASCSource) ParseRootFileSelectedWithWorkers(data []byte, fileDataIDs []uint32, workers int) (int, error) {
	return c.parseRootFileSelectedWithWorkers(data, fileDataIDs, workers, nil)
}

func (c *CASCSource) parseRootFileSelectedForLocale(data []byte, fileDataIDs []uint32, workers int) (int, error) {
	c.markSelectedRootTargets(fileDataIDs)
	return c.parseRootFileSelectedWithWorkers(data, fileDataIDs, workers, &rootTypeFilter{locale: c.Locale})
}

func (c *CASCSource) markSelectedRootTargets(fileDataIDs []uint32) {
	for _, id := range fileDataIDs {
		if _, exists := c.RootEntries[id]; !exists {
			c.RootEntries[id] = nil
		}
	}
}

func (c *CASCSource) parseRootFileSelectedWithWorkers(data []byte, fileDataIDs []uint32, workers int, filter *rootTypeFilter) (int, error) {
	reader, err := blte.NewReader(data)
	if err != nil {
		return 0, err
	}
	buf, err := reader.ReadAllParallel(workers)
	if err != nil {
		return 0, err
	}
	wanted := make(map[uint32]struct{}, len(fileDataIDs))
	for _, id := range fileDataIDs {
		wanted[id] = struct{}{}
	}
	return c.parseRootWithFilter(buf, wanted, filter)
}

func (c *CASCSource) parseRoot(data []byte) (int, error) {
	return c.parseRootWithFilter(data, nil, nil)
}

func (c *CASCSource) parseRootSelected(data []byte, wanted map[uint32]struct{}) (int, error) {
	return c.parseRootWithFilter(data, wanted, nil)
}

type rootTypeFilter struct {
	locale LocaleFlag
}

func (f *rootTypeFilter) accepts(contentFlags ContentFlag, localeFlags LocaleFlag) bool {
	return f == nil || localeFlags&f.locale != 0 && contentFlags&ContentLowViolence == 0
}

func (c *CASCSource) parseRootWithFilter(data []byte, wanted map[uint32]struct{}, filter *rootTypeFilter) (count int, err error) {
	defer func() {
		if err == nil {
			resource.RecordCASCMetadata(len(data))
		}
	}()
	if len(data) < 4 {
		return 0, fmt.Errorf("root data too short")
	}

	magic := binary.LittleEndian.Uint32(data)
	pos := 4

	if magic == rootMagic {
		headerSize := binary.LittleEndian.Uint32(data[pos:])
		pos += 4
		version := binary.LittleEndian.Uint32(data[pos:])
		pos += 4

		if headerSize != 0x18 {
			version = 0
		} else if version != 1 && version != 2 {
			return 0, fmt.Errorf("unknown root version: %d", version)
		}

		var totalFileCount, namedFileCount uint32
		if version == 0 {
			totalFileCount = headerSize
			namedFileCount = version
			headerSize = 12
		} else {
			totalFileCount = binary.LittleEndian.Uint32(data[pos:])
			pos += 4
			namedFileCount = binary.LittleEndian.Uint32(data[pos:])
			pos += 4
		}
		allowNameless := totalFileCount != namedFileCount

		pos = int(headerSize)
		return c.parseRootBlocks(data, &pos, version, allowNameless, false, wanted, filter)
	} else {
		pos = 0
		return c.parseRootBlocks(data, &pos, 0, false, true, wanted, filter)
	}
}

func (c *CASCSource) parseRootBlocks(data []byte, pos *int, version uint32, allowNameless, classic bool, wanted map[uint32]struct{}, filter *rootTypeFilter) (int, error) {
	for *pos < len(data) {
		if *pos+4 > len(data) {
			break
		}
		numRecords := binary.LittleEndian.Uint32(data[*pos:])
		*pos += 4

		var contentFlags ContentFlag
		var localeFlags LocaleFlag

		if classic || version <= 1 {
			if err := requireRootBytes(data, *pos, 8, "root block flags"); err != nil {
				return 0, err
			}
			contentFlags = ContentFlag(binary.LittleEndian.Uint32(data[*pos:]))
			*pos += 4
			localeFlags = LocaleFlag(binary.LittleEndian.Uint32(data[*pos:]))
			*pos += 4
		} else if version == 2 {
			if err := requireRootBytes(data, *pos, 13, "root v2 block flags"); err != nil {
				return 0, err
			}
			localeFlags = LocaleFlag(binary.LittleEndian.Uint32(data[*pos:]))
			*pos += 4
			cflags1 := binary.LittleEndian.Uint32(data[*pos:])
			*pos += 4
			cflags2 := binary.LittleEndian.Uint32(data[*pos:])
			*pos += 4
			cflags3 := uint32(data[*pos])
			*pos++
			contentFlags = ContentFlag(cflags1 | cflags2 | (cflags3 << 17))
		}

		keyBytes := 16
		if classic {
			keyBytes += 8
		}
		hashBytes := 0
		if !classic && !(allowNameless && contentFlags&ContentNoNameHash != 0) {
			hashBytes = 8
		}
		if !filter.accepts(contentFlags, localeFlags) {
			blockBytes := int64(numRecords) * int64(4+keyBytes+hashBytes)
			if blockBytes > int64(len(data)-*pos) {
				return 0, fmt.Errorf("root filtered block payload out of bounds at offset %d need %d bytes in %d-byte root", *pos, blockBytes, len(data))
			}
			*pos += int(blockBytes)
			continue
		}

		// Read delta-encoded FDIDs
		if err := requireRootBytes(data, *pos, int(numRecords)*4, "root fileDataID deltas"); err != nil {
			return 0, err
		}
		type selectedRecord struct {
			index uint32
			id    uint32
		}
		selected := make([]selectedRecord, 0, len(wanted))
		fdid := uint32(0)
		for i := uint32(0); i < numRecords; i++ {
			nextID := fdid + uint32(int32(binary.LittleEndian.Uint32(data[*pos:])))
			*pos += 4
			if wanted == nil {
				selected = append(selected, selectedRecord{index: i, id: nextID})
			} else if _, ok := wanted[nextID]; ok {
				selected = append(selected, selectedRecord{index: i, id: nextID})
			}
			fdid = nextID + 1
		}

		typeIndex := len(c.RootTypes)

		// Read content keys
		if err := requireRootBytes(data, *pos, int(numRecords)*keyBytes, "root content keys"); err != nil {
			return 0, err
		}
		keysStart := *pos
		for _, record := range selected {
			keyOffset := keysStart + int(record.index)*keyBytes
			key := hex.EncodeToString(data[keyOffset : keyOffset+16])
			c.RootEntries[record.id] = append(c.RootEntries[record.id], RootEntry{TypeIndex: typeIndex, ContentKey: key})
		}
		*pos += int(numRecords) * keyBytes

		// Skip lookup hashes for non-classic
		if !classic {
			if !(allowNameless && contentFlags&ContentNoNameHash != 0) {
				if err := requireRootBytes(data, *pos, int(8*numRecords), "root lookup hashes"); err != nil {
					return 0, err
				}
				*pos += int(8 * numRecords)
			}
		}

		c.RootTypes = append(c.RootTypes, RootType{ContentFlags: contentFlags, LocaleFlags: localeFlags})
	}
	return len(c.RootEntries), nil
}

func requireRootBytes(data []byte, pos int, size int, context string) error {
	if size < 0 || pos < 0 || pos+size > len(data) {
		return fmt.Errorf("%s out of bounds at offset %d need %d bytes in %d-byte root", context, pos, size, len(data))
	}
	return nil
}

func (c *CASCSource) ParseEncodingFile(data []byte) error {
	reader, err := blte.NewReader(data)
	if err != nil {
		return err
	}
	buf, err := reader.ReadAll()
	if err != nil {
		return err
	}
	return c.parseEncoding(buf)
}

// ParseEncodingFileSelected retains only requested content keys. It is used by
// point and batch queries to avoid materializing the global encoding catalog.
func (c *CASCSource) ParseEncodingFileSelected(data []byte, contentKeys []string) error {
	return c.ParseEncodingFileSelectedWithWorkers(data, contentKeys, 1)
}

func (c *CASCSource) ParseEncodingFileSelectedWithWorkers(data []byte, contentKeys []string, workers int) error {
	reader, err := blte.NewReader(data)
	if err != nil {
		return err
	}
	buf, err := reader.ReadAllParallel(workers)
	if err != nil {
		return err
	}
	wanted := make(map[string]struct{}, len(contentKeys))
	for _, key := range contentKeys {
		raw, decodeErr := hex.DecodeString(key)
		if decodeErr != nil {
			return fmt.Errorf("invalid content key %q: %w", key, decodeErr)
		}
		wanted[string(raw)] = struct{}{}
	}
	return c.parseEncodingSelected(buf, wanted)
}

func (c *CASCSource) parseEncodingSelected(data []byte, wanted map[string]struct{}) (err error) {
	defer func() {
		if err == nil {
			resource.RecordCASCMetadata(len(data))
		}
	}()
	meta, err := parseEncodingMetadata(data)
	if err != nil {
		return err
	}
	for page := 0; page < meta.pageCount; page++ {
		if err := scanEncodingPage(data, meta.pagesStart+meta.pageSize*page, meta.pageSize, meta.cKeySize, meta.eKeySize, wanted, c.EncodingEntries); err != nil {
			return fmt.Errorf("encoding page %d: %w", page, err)
		}
	}
	return nil
}

type decompressedRangeReader interface {
	Size() int
	ReadRange(offset, length, workers int) ([]byte, error)
}

type verifiedDecompressedRangeReader interface {
	ReadVerifiedRange(offset, length int, expectedMD5 []byte, workers int) ([]byte, error)
}

func (c *CASCSource) parseEncodingReaderSelected(reader decompressedRangeReader, wanted map[string]struct{}, workers int) error {
	if len(wanted) == 0 {
		return nil
	}
	headerLength := 22
	if reader.Size() < headerLength {
		headerLength = reader.Size()
	}
	headerData, err := reader.ReadRange(0, headerLength, 1)
	if err != nil {
		return err
	}
	meta, err := parseEncodingMetadataSized(headerData, reader.Size())
	if err != nil {
		return err
	}
	prefix, err := reader.ReadRange(0, meta.pagesStart, workers)
	if err != nil {
		return err
	}
	stride := meta.cKeySize + 16
	indexStart := meta.pagesStart - meta.pageCount*stride
	if indexStart < 22 || indexStart > len(prefix) {
		return fmt.Errorf("invalid encoding page index")
	}
	pages := make(map[int]struct{}, len(wanted))
	for rawKey := range wanted {
		key := []byte(rawKey)
		if len(key) != meta.cKeySize {
			return fmt.Errorf("content key size %d, expected %d", len(key), meta.cKeySize)
		}
		page := sort.Search(meta.pageCount, func(index int) bool {
			offset := indexStart + index*stride
			return bytes.Compare(prefix[offset:offset+meta.cKeySize], key) > 0
		}) - 1
		if page < 0 {
			page = 0
		}
		pages[page] = struct{}{}
	}
	pageNumbers := make([]int, 0, len(pages))
	for page := range pages {
		pageNumbers = append(pageNumbers, page)
	}
	sort.Ints(pageNumbers)
	for _, page := range pageNumbers {
		start := meta.pagesStart + page*meta.pageSize
		length := meta.pageSize
		if start > reader.Size() {
			return fmt.Errorf("encoding page %d starts beyond data", page)
		}
		if length > reader.Size()-start {
			length = reader.Size() - start
		}
		checksumOffset := indexStart + page*stride + meta.cKeySize
		checksum := prefix[checksumOffset : checksumOffset+md5.Size]
		var pageData []byte
		var readErr error
		if verified, ok := reader.(verifiedDecompressedRangeReader); ok && !isZeroKey(checksum) {
			pageData, readErr = verified.ReadVerifiedRange(start, length, checksum, workers)
		} else {
			pageData, readErr = reader.ReadRange(start, length, workers)
			if readErr == nil && !isZeroKey(checksum) {
				actual := md5.Sum(pageData)
				if !bytes.Equal(actual[:], checksum) {
					readErr = fmt.Errorf("encoding page %d checksum mismatch", page)
				}
			}
		}
		if readErr != nil {
			return fmt.Errorf("encoding page %d: %w", page, readErr)
		}
		if scanErr := scanEncodingPage(pageData, 0, len(pageData), meta.cKeySize, meta.eKeySize, wanted, c.EncodingEntries); scanErr != nil {
			return fmt.Errorf("encoding page %d: %w", page, scanErr)
		}
	}
	return nil
}

type encodingMetadata struct {
	cKeySize, eKeySize  int
	pageSize, pageCount int
	pagesStart          int
}

func parseEncodingMetadata(data []byte) (encodingMetadata, error) {
	return parseEncodingMetadataSized(data, len(data))
}

func parseEncodingMetadataSized(data []byte, totalSize int) (encodingMetadata, error) {
	if len(data) < 22 {
		return encodingMetadata{}, fmt.Errorf("encoding data too short")
	}
	if binary.LittleEndian.Uint16(data) != encMagic {
		return encodingMetadata{}, fmt.Errorf("invalid encoding magic: %d", binary.LittleEndian.Uint16(data))
	}
	pos := 3
	meta := encodingMetadata{cKeySize: int(data[pos]), eKeySize: int(data[pos+1])}
	pos += 2
	meta.pageSize = int(int16(binary.BigEndian.Uint16(data[pos:]))) * 1024
	pos += 4
	meta.pageCount = int(int32(binary.BigEndian.Uint32(data[pos:])))
	pos += 4
	pos += 5
	specBlockSize := int(int32(binary.BigEndian.Uint32(data[pos:])))
	pos += 4
	meta.pagesStart = pos + specBlockSize + meta.pageCount*(meta.cKeySize+16)
	if meta.pageCount < 0 || meta.pageSize <= 0 || meta.cKeySize <= 0 || meta.eKeySize <= 0 || meta.pagesStart < 0 || meta.pagesStart > totalSize {
		return encodingMetadata{}, fmt.Errorf("invalid encoding page metadata")
	}
	return meta, nil
}

func scanEncodingPage(data []byte, start, size, cKeySize, eKeySize int, wanted map[string]struct{}, output map[string]EncodingEntry) error {
	if start < 0 || size < 0 || start > len(data) {
		return fmt.Errorf("page start out of bounds")
	}
	end := start + size
	if end > len(data) {
		end = len(data)
	}
	for pos := start; pos < end; {
		keysCount := int(data[pos])
		pos++
		if keysCount == 0 {
			break
		}
		entrySize := 5 + cKeySize + eKeySize*keysCount
		if pos+entrySize > end {
			return fmt.Errorf("entry at offset %d exceeds page", pos-1)
		}
		contentSize := int64(data[pos])<<32 | int64(binary.BigEndian.Uint32(data[pos+1:]))
		pos += 5
		rawCKey := data[pos : pos+cKeySize]
		pos += cKeySize
		rawEKey := data[pos : pos+eKeySize]
		pos += eKeySize * keysCount
		if _, ok := wanted[string(rawCKey)]; ok {
			output[hex.EncodeToString(rawCKey)] = EncodingEntry{Key: hex.EncodeToString(rawEKey), Size: contentSize}
		}
	}
	return nil
}

func (c *CASCSource) parseEncoding(data []byte) error {
	if len(data) < 20 {
		return fmt.Errorf("encoding data too short")
	}

	magic := binary.LittleEndian.Uint16(data)
	if magic != encMagic {
		return fmt.Errorf("invalid encoding magic: %d", magic)
	}
	pos := 2
	pos++ // version
	hashSizeCKey := int(data[pos])
	pos++
	hashSizeEKey := int(data[pos])
	pos++
	cKeyPageSize := int(int16(binary.BigEndian.Uint16(data[pos:]))) * 1024
	pos += 2
	pos += 2 // eKeyPageSize
	cKeyPageCount := int(int32(binary.BigEndian.Uint32(data[pos:])))
	pos += 4
	pos += 5 // eKeyPageCount + unk11
	specBlockSize := int(int32(binary.BigEndian.Uint32(data[pos:])))
	pos += 4

	pos += specBlockSize + (cKeyPageCount * (hashSizeCKey + 16))

	pagesStart := pos
	if cKeyPageCount < 0 || cKeyPageSize <= 0 || pagesStart < 0 || pagesStart > len(data) {
		return fmt.Errorf("invalid encoding page metadata")
	}
	pageEntries := make([]map[string]EncodingEntry, cKeyPageCount)
	jobs := make(chan int)
	var firstErr error
	var errMu sync.Mutex
	workerCount := runtime.GOMAXPROCS(0)
	if workerCount > cKeyPageCount {
		workerCount = cKeyPageCount
	}
	if workerCount < 1 {
		workerCount = 1
	}
	var wg sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for page := range jobs {
				entries, parseErr := parseEncodingPage(data, pagesStart+cKeyPageSize*page, cKeyPageSize, hashSizeCKey, hashSizeEKey)
				if parseErr != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("encoding page %d: %w", page, parseErr)
					}
					errMu.Unlock()
					continue
				}
				pageEntries[page] = entries
			}
		}()
	}
	for page := 0; page < cKeyPageCount; page++ {
		jobs <- page
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	for _, entries := range pageEntries {
		for cKey, entry := range entries {
			c.EncodingEntries[cKey] = entry
		}
	}
	return nil
}

func parseEncodingPage(data []byte, start, size, hashSizeCKey, hashSizeEKey int) (map[string]EncodingEntry, error) {
	if start < 0 || size < 0 || start > len(data) {
		return nil, fmt.Errorf("page start out of bounds")
	}
	end := start + size
	if end > len(data) {
		end = len(data)
	}
	entries := make(map[string]EncodingEntry)
	for pos := start; pos < end; {
		keysCount := int(data[pos])
		pos++
		if keysCount == 0 {
			break
		}
		entrySize := 5 + hashSizeCKey + hashSizeEKey*keysCount
		if hashSizeCKey <= 0 || hashSizeEKey <= 0 || pos+entrySize > end {
			return nil, fmt.Errorf("entry at offset %d exceeds page", pos-1)
		}
		contentSize := int64(data[pos])<<32 | int64(binary.BigEndian.Uint32(data[pos+1:]))
		pos += 5
		cKey := hex.EncodeToString(data[pos : pos+hashSizeCKey])
		pos += hashSizeCKey
		eKey := hex.EncodeToString(data[pos : pos+hashSizeEKey])
		pos += hashSizeEKey * keysCount
		entries[cKey] = EncodingEntry{Key: eKey, Size: contentSize}
	}
	return entries, nil
}

func (c *CASCSource) ParseArchiveIndex(data []byte, archiveKey string) {
	for hash, entry := range ParseArchiveIndexEntries(data, archiveKey) {
		c.Archives[hash] = entry
	}
}

func ParseArchiveIndexEntries(data []byte, archiveKey string) map[string]ArchiveEntry {
	defer resource.RecordCASCMetadata(len(data))
	entries := make(map[string]ArchiveEntry)
	pos := len(data) - 12
	if pos < 0 {
		return entries
	}
	count := int(binary.LittleEndian.Uint32(data[pos:]))

	pos = 0
	for i := 0; i < count && pos+24 <= len(data); i++ {
		hash := hex.EncodeToString(data[pos : pos+16])
		pos += 16
		if hash == strings.Repeat("00", 16) && pos+24 <= len(data) {
			hash = hex.EncodeToString(data[pos : pos+16])
			pos += 16
		}
		size := int32(binary.BigEndian.Uint32(data[pos:]))
		pos += 4
		offset := int32(binary.BigEndian.Uint32(data[pos:]))
		pos += 4
		entries[hash] = ArchiveEntry{Key: archiveKey, Size: size, Offset: offset}
	}
	return entries
}

func ParseArchiveIndexEntriesSelected(data []byte, archiveKey string, encodingKeys map[string]struct{}) map[string]ArchiveEntry {
	defer resource.RecordCASCMetadata(len(data))
	rawWanted := make(map[string]string, len(encodingKeys))
	for key := range encodingKeys {
		raw, err := hex.DecodeString(key)
		if err == nil {
			rawWanted[string(raw)] = key
		}
	}
	entries := make(map[string]ArchiveEntry)
	pos := len(data) - 12
	if pos < 0 {
		return entries
	}
	count := int(binary.LittleEndian.Uint32(data[pos:]))
	pos = 0
	for i := 0; i < count && pos+24 <= len(data); i++ {
		rawKey := data[pos : pos+16]
		pos += 16
		if isZeroKey(rawKey) && pos+24 <= len(data) {
			rawKey = data[pos : pos+16]
			pos += 16
		}
		size := int32(binary.BigEndian.Uint32(data[pos:]))
		pos += 4
		offset := int32(binary.BigEndian.Uint32(data[pos:]))
		pos += 4
		if key, ok := rawWanted[string(rawKey)]; ok {
			entries[key] = ArchiveEntry{Key: archiveKey, Size: size, Offset: offset}
		}
	}
	return entries
}

func isZeroKey(key []byte) bool {
	for _, value := range key {
		if value != 0 {
			return false
		}
	}
	return true
}

func (c *CASCSource) SetBuildConfig(cfg map[string]string) {
	c.BuildConfig = cfg
}

func (c *CASCSource) SetCDNConfig(cfg map[string]string) {
	c.CDNConfig = cfg
}
