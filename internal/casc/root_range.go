package casc

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"

	"wowdata/internal/resource"
)

const defaultRootDeltaBatchBytes = 4 * 1024 * 1024

func rootDeltaBatchBytes() int {
	raw := strings.TrimSpace(os.Getenv("WOWDATA_ROOT_DELTA_BATCH_KIB"))
	if raw == "" {
		return defaultRootDeltaBatchBytes
	}
	kib, err := strconv.Atoi(raw)
	if err != nil || kib < 16 || kib > 16384 {
		return defaultRootDeltaBatchBytes
	}
	return kib * 1024
}

// ParseRootReaderSelected performs a projected root scan over decompressed
// ranges. It reads FileDataID deltas and matched content keys, while skipping
// unrelated key and name-hash payloads without decoding their BLTE blocks.
func (c *CASCSource) ParseRootReaderSelected(reader decompressedRangeReader, fileDataIDs []uint32, workers int) (int, error) {
	return c.parseRootReaderSelected(reader, fileDataIDs, workers, nil)
}

func (c *CASCSource) parseRootReaderSelectedForLocale(reader decompressedRangeReader, fileDataIDs []uint32, workers int) (int, error) {
	c.markSelectedRootTargets(fileDataIDs)
	return c.parseRootReaderSelected(reader, fileDataIDs, workers, &rootTypeFilter{locale: c.Locale})
}

func (c *CASCSource) parseRootReaderSelected(reader decompressedRangeReader, fileDataIDs []uint32, workers int, filter *rootTypeFilter) (count int, err error) {
	if reader == nil || reader.Size() < 4 {
		return 0, fmt.Errorf("root data too short")
	}
	wanted := make(map[uint32]struct{}, len(fileDataIDs))
	for _, id := range fileDataIDs {
		wanted[id] = struct{}{}
	}
	input := rootRangeInput{reader: reader, size: reader.Size(), workers: workers}
	defer func() { resource.RecordCASCMetadata(input.bytesRead) }()
	first, err := input.u32(0)
	if err != nil {
		return 0, err
	}
	if first != rootMagic {
		pos, err := c.parseRootRangeBlock(&input, 4, first, 0, false, true, wanted, filter)
		if err != nil {
			return 0, err
		}
		for pos < input.size {
			numRecords, readErr := input.u32(pos)
			if readErr != nil {
				return 0, readErr
			}
			pos, err = c.parseRootRangeBlock(&input, pos+4, numRecords, 0, false, true, wanted, filter)
			if err != nil {
				return 0, err
			}
		}
		return len(c.RootEntries), nil
	}
	header, err := input.read(0, minRootRangeInt(24, input.size))
	if err != nil || len(header) < 12 {
		return 0, fmt.Errorf("root header: %w", err)
	}
	headerSize := binary.LittleEndian.Uint32(header[4:])
	version := binary.LittleEndian.Uint32(header[8:])
	var totalFileCount, namedFileCount uint32
	if headerSize != 0x18 {
		totalFileCount = headerSize
		version = 0
		headerSize = 12
	} else {
		if version != 1 && version != 2 {
			return 0, fmt.Errorf("unknown root version: %d", version)
		}
		if len(header) < 20 {
			return 0, fmt.Errorf("root header is truncated")
		}
		totalFileCount = binary.LittleEndian.Uint32(header[12:])
		namedFileCount = binary.LittleEndian.Uint32(header[16:])
	}
	allowNameless := totalFileCount != namedFileCount
	pos := int(headerSize)
	for pos < input.size {
		numRecords, readErr := input.u32(pos)
		if readErr != nil {
			return 0, readErr
		}
		pos, err = c.parseRootRangeBlock(&input, pos+4, numRecords, version, allowNameless, false, wanted, filter)
		if err != nil {
			return 0, err
		}
	}
	return len(c.RootEntries), nil
}

type rootRangeInput struct {
	reader    decompressedRangeReader
	size      int
	workers   int
	bytesRead int
}

func (r *rootRangeInput) read(offset, length int) ([]byte, error) {
	if offset < 0 || length < 0 || offset > r.size || length > r.size-offset {
		return nil, fmt.Errorf("root range %d+%d exceeds %d", offset, length, r.size)
	}
	data, err := r.reader.ReadRange(offset, length, r.workers)
	r.bytesRead += len(data)
	return data, err
}

func (r *rootRangeInput) u32(offset int) (uint32, error) {
	data, err := r.read(offset, 4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(data), nil
}

func (c *CASCSource) parseRootRangeBlock(input *rootRangeInput, pos int, numRecords, version uint32, allowNameless, classic bool, wanted map[uint32]struct{}, filter *rootTypeFilter) (int, error) {
	flagsSize := 8
	if !classic && version == 2 {
		flagsSize = 13
	}
	flags, err := input.read(pos, flagsSize)
	if err != nil {
		return 0, fmt.Errorf("root block flags: %w", err)
	}
	var contentFlags ContentFlag
	var localeFlags LocaleFlag
	if classic || version <= 1 {
		contentFlags = ContentFlag(binary.LittleEndian.Uint32(flags))
		localeFlags = LocaleFlag(binary.LittleEndian.Uint32(flags[4:]))
	} else {
		localeFlags = LocaleFlag(binary.LittleEndian.Uint32(flags))
		cflags1 := binary.LittleEndian.Uint32(flags[4:])
		cflags2 := binary.LittleEndian.Uint32(flags[8:])
		contentFlags = ContentFlag(cflags1 | cflags2 | (uint32(flags[12]) << 17))
	}
	pos += flagsSize
	keyBytes := 16
	if classic {
		keyBytes += 8
	}
	hashBytes := 0
	if !classic && !(allowNameless && contentFlags&ContentNoNameHash != 0) {
		hashBytes = 8
	}
	if !filter.accepts(contentFlags, localeFlags) {
		blockBytes := uint64(numRecords) * uint64(4+keyBytes+hashBytes)
		if blockBytes > uint64(input.size-pos) {
			return 0, fmt.Errorf("root filtered block payload exceeds remaining data")
		}
		return pos + int(blockBytes), nil
	}
	if uint64(numRecords) > uint64(input.size-pos)/4 {
		return 0, fmt.Errorf("root block record count %d exceeds remaining data", numRecords)
	}
	type selectedRecord struct {
		index uint32
		id    uint32
	}
	selected := make([]selectedRecord, 0, len(wanted))
	fdid := uint32(0)
	batchBytes := rootDeltaBatchBytes()
	for base := uint32(0); base < numRecords; {
		count := uint32(batchBytes / 4)
		if remaining := numRecords - base; remaining < count {
			count = remaining
		}
		data, readErr := input.read(pos+int(base)*4, int(count)*4)
		if readErr != nil {
			return 0, fmt.Errorf("root FileDataID deltas: %w", readErr)
		}
		for index := uint32(0); index < count; index++ {
			nextID := fdid + uint32(int32(binary.LittleEndian.Uint32(data[index*4:])))
			if _, ok := wanted[nextID]; ok {
				selected = append(selected, selectedRecord{index: base + index, id: nextID})
			}
			fdid = nextID + 1
		}
		base += count
	}
	pos += int(numRecords) * 4
	if uint64(numRecords) > uint64(input.size-pos)/uint64(keyBytes) {
		return 0, fmt.Errorf("root content keys exceed remaining data")
	}
	typeIndex := len(c.RootTypes)
	for _, record := range selected {
		key, readErr := input.read(pos+int(record.index)*keyBytes, 16)
		if readErr != nil {
			return 0, fmt.Errorf("root content key: %w", readErr)
		}
		c.RootEntries[record.id] = append(c.RootEntries[record.id], RootEntry{TypeIndex: typeIndex, ContentKey: hex.EncodeToString(key)})
	}
	pos += int(numRecords) * keyBytes
	if !classic && !(allowNameless && contentFlags&ContentNoNameHash != 0) {
		if uint64(numRecords) > uint64(input.size-pos)/8 {
			return 0, fmt.Errorf("root name hashes exceed remaining data")
		}
		pos += int(numRecords) * 8
	}
	c.RootTypes = append(c.RootTypes, RootType{ContentFlags: contentFlags, LocaleFlags: localeFlags})
	return pos, nil
}

func minRootRangeInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
