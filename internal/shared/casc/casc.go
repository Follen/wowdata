package casc

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"wowdata/internal/shared/blte"
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

func (c *CASCSource) parseRoot(data []byte) (int, error) {
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
		return c.parseRootBlocks(data, &pos, version, allowNameless, false)
	} else {
		pos = 0
		return c.parseRootBlocks(data, &pos, 0, false, true)
	}
}

func (c *CASCSource) parseRootBlocks(data []byte, pos *int, version uint32, allowNameless, classic bool) (int, error) {
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

		// Read delta-encoded FDIDs
		if err := requireRootBytes(data, *pos, int(numRecords)*4, "root fileDataID deltas"); err != nil {
			return 0, err
		}
		fdids := make([]uint32, numRecords)
		fdid := uint32(0)
		for i := uint32(0); i < numRecords; i++ {
			nextID := fdid + uint32(int32(binary.LittleEndian.Uint32(data[*pos:])))
			*pos += 4
			fdids[i] = nextID
			fdid = nextID + 1
		}

		typeIndex := len(c.RootTypes)

		// Read content keys
		keyBytes := 16
		if classic {
			keyBytes += 8
		}
		if err := requireRootBytes(data, *pos, int(numRecords)*keyBytes, "root content keys"); err != nil {
			return 0, err
		}
		for i := uint32(0); i < numRecords; i++ {
			fdid := fdids[i]
			key := hex.EncodeToString(data[*pos : *pos+16])
			*pos += 16
			if classic {
				*pos += 8 // skip hash
			}
			c.RootEntries[fdid] = append(c.RootEntries[fdid], RootEntry{TypeIndex: typeIndex, ContentKey: key})
		}

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
	for i := 0; i < cKeyPageCount; i++ {
		pageStart := pagesStart + cKeyPageSize*i
		pos = pageStart

		for pos < pageStart+cKeyPageSize && pos < len(data) {
			keysCount := data[pos]
			pos++
			if keysCount == 0 {
				break
			}

			// int40BE size
			size := int64(data[pos])<<32 | int64(binary.BigEndian.Uint32(data[pos+1:]))
			pos += 5

			cKey := hex.EncodeToString(data[pos : pos+hashSizeCKey])
			pos += hashSizeCKey
			eKey := hex.EncodeToString(data[pos : pos+hashSizeEKey])
			pos += hashSizeEKey

			c.EncodingEntries[cKey] = EncodingEntry{Key: eKey, Size: size}

			pos += hashSizeEKey * (int(keysCount) - 1)
		}
	}
	return nil
}

func (c *CASCSource) ParseArchiveIndex(data []byte, archiveKey string) {
	pos := len(data) - 12
	if pos < 0 {
		return
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
		c.Archives[hash] = ArchiveEntry{Key: archiveKey, Size: size, Offset: offset}
	}
}

func (c *CASCSource) SetBuildConfig(cfg map[string]string) {
	c.BuildConfig = cfg
}

func (c *CASCSource) SetCDNConfig(cfg map[string]string) {
	c.CDNConfig = cfg
}
