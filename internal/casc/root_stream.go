package casc

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"

	"wowdata/internal/blte"
	"wowdata/internal/resource"
)

const selectedRootStreamingThreshold = 32 << 20
const selectedRootPointLookupLimit = 2

func (c *CASCSource) ParseRootFileSelectedAdaptive(data []byte, fileDataIDs []uint32, workers int) (int, error) {
	return c.parseRootFileSelectedAdaptive(data, fileDataIDs, workers, nil)
}

func (c *CASCSource) parseRootFileSelectedAdaptiveForLocale(data []byte, fileDataIDs []uint32, workers int) (int, error) {
	c.markSelectedRootTargets(fileDataIDs)
	return c.parseRootFileSelectedAdaptive(data, fileDataIDs, workers, &rootTypeFilter{locale: c.Locale})
}

func (c *CASCSource) parseRootFileSelectedAdaptive(data []byte, fileDataIDs []uint32, workers int, filter *rootTypeFilter) (int, error) {
	streaming, err := shouldStreamSelectedRoot(data, fileDataIDs)
	if err != nil {
		return 0, err
	}
	if !streaming {
		return c.parseRootFileSelectedWithWorkers(data, fileDataIDs, workers, filter)
	}
	return c.parseRootFileSelectedStreaming(data, fileDataIDs, filter)
}

func shouldStreamSelectedRoot(data []byte, fileDataIDs []uint32) (bool, error) {
	header := blte.ParseBLTEHeader(data)
	if header == nil {
		return false, fmt.Errorf("invalid BLTE root header")
	}
	return len(fileDataIDs) > selectedRootPointLookupLimit && header.TotalSize > selectedRootStreamingThreshold, nil
}

func selectedRootWorkerBudget(data []byte, fileDataIDs []uint32, workers int) int {
	if workers <= 1 {
		return 1
	}
	streaming, err := shouldStreamSelectedRoot(data, fileDataIDs)
	if err == nil && streaming {
		return 1
	}
	return workers - 1
}

// ParseRootFileSelectedStreaming scans a BLTE root forward-only. Memory is
// bounded by one decoded BLTE block, a small read buffer, and matched entries.
func (c *CASCSource) ParseRootFileSelectedStreaming(data []byte, fileDataIDs []uint32) (int, error) {
	return c.parseRootFileSelectedStreaming(data, fileDataIDs, nil)
}

func (c *CASCSource) parseRootFileSelectedStreaming(data []byte, fileDataIDs []uint32, filter *rootTypeFilter) (int, error) {
	reader, err := blte.NewReader(data)
	if err != nil {
		return 0, err
	}
	wanted := make(map[uint32]struct{}, len(fileDataIDs))
	for _, id := range fileDataIDs {
		wanted[id] = struct{}{}
	}
	input := &rootStreamInput{reader: bufio.NewReaderSize(reader.StreamReader(), 128*1024), remaining: int64(reader.Size())}
	initial := input.remaining
	count, parseErr := c.parseRootSelectedStream(input, wanted, filter)
	resource.RecordCASCMetadata(int(initial - input.remaining))
	return count, parseErr
}

type rootStreamInput struct {
	reader    *bufio.Reader
	remaining int64
}

func (r *rootStreamInput) readFull(data []byte) error {
	if int64(len(data)) > r.remaining {
		return fmt.Errorf("truncated root stream: need %d bytes, have %d", len(data), r.remaining)
	}
	if _, err := io.ReadFull(r.reader, data); err != nil {
		return err
	}
	r.remaining -= int64(len(data))
	return nil
}

func (r *rootStreamInput) readU32() (uint32, error) {
	var data [4]byte
	if err := r.readFull(data[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(data[:]), nil
}

func (r *rootStreamInput) skip(count int64) error {
	if count < 0 || count > r.remaining {
		return fmt.Errorf("truncated root stream: skip %d bytes, have %d", count, r.remaining)
	}
	var scratch [64 * 1024]byte
	for count > 0 {
		step := int64(len(scratch))
		if count < step {
			step = count
		}
		if err := r.readFull(scratch[:int(step)]); err != nil {
			return err
		}
		count -= step
	}
	return nil
}

func (c *CASCSource) parseRootSelectedStream(input *rootStreamInput, wanted map[uint32]struct{}, filter *rootTypeFilter) (int, error) {
	first, err := input.readU32()
	if err != nil {
		return 0, err
	}
	if first != rootMagic {
		if err := c.parseRootStreamBlock(input, first, 0, false, true, wanted, filter); err != nil {
			return 0, err
		}
		for input.remaining > 0 {
			numRecords, err := input.readU32()
			if err != nil {
				return 0, err
			}
			if err := c.parseRootStreamBlock(input, numRecords, 0, false, true, wanted, filter); err != nil {
				return 0, err
			}
		}
		return len(c.RootEntries), nil
	}

	headerSize, err := input.readU32()
	if err != nil {
		return 0, err
	}
	version, err := input.readU32()
	if err != nil {
		return 0, err
	}
	var totalFileCount, namedFileCount uint32
	if headerSize != 0x18 {
		totalFileCount = headerSize
		version = 0
	} else {
		if version != 1 && version != 2 {
			return 0, fmt.Errorf("unknown root version: %d", version)
		}
		totalFileCount, err = input.readU32()
		if err != nil {
			return 0, err
		}
		namedFileCount, err = input.readU32()
		if err != nil {
			return 0, err
		}
		if err := input.skip(int64(headerSize) - 20); err != nil {
			return 0, err
		}
	}
	allowNameless := totalFileCount != namedFileCount
	for input.remaining > 0 {
		numRecords, err := input.readU32()
		if err != nil {
			return 0, err
		}
		if err := c.parseRootStreamBlock(input, numRecords, version, allowNameless, false, wanted, filter); err != nil {
			return 0, err
		}
	}
	return len(c.RootEntries), nil
}

func (c *CASCSource) parseRootStreamBlock(input *rootStreamInput, numRecords, version uint32, allowNameless, classic bool, wanted map[uint32]struct{}, filter *rootTypeFilter) error {
	var contentFlags ContentFlag
	var localeFlags LocaleFlag
	if classic || version <= 1 {
		value, err := input.readU32()
		if err != nil {
			return err
		}
		contentFlags = ContentFlag(value)
		value, err = input.readU32()
		if err != nil {
			return err
		}
		localeFlags = LocaleFlag(value)
	} else {
		value, err := input.readU32()
		if err != nil {
			return err
		}
		localeFlags = LocaleFlag(value)
		cflags1, err := input.readU32()
		if err != nil {
			return err
		}
		cflags2, err := input.readU32()
		if err != nil {
			return err
		}
		var cflags3 [1]byte
		if err := input.readFull(cflags3[:]); err != nil {
			return err
		}
		contentFlags = ContentFlag(cflags1 | cflags2 | (uint32(cflags3[0]) << 17))
	}
	keyBytes := 16
	if classic {
		keyBytes = 24
	}
	hashBytes := 0
	if !classic && !(allowNameless && contentFlags&ContentNoNameHash != 0) {
		hashBytes = 8
	}
	if !filter.accepts(contentFlags, localeFlags) {
		return input.skip(int64(numRecords) * int64(4+keyBytes+hashBytes))
	}

	if int64(numRecords) > input.remaining/4 {
		return fmt.Errorf("root block record count %d exceeds remaining data", numRecords)
	}
	selected := make(map[uint32]uint32, len(wanted))
	fdid := uint32(0)
	var deltas [64 * 1024]byte
	for base := uint32(0); base < numRecords; {
		count := uint32(len(deltas) / 4)
		if remaining := numRecords - base; remaining < count {
			count = remaining
		}
		data := deltas[:int(count)*4]
		if err := input.readFull(data); err != nil {
			return err
		}
		for index := uint32(0); index < count; index++ {
			nextID := fdid + uint32(int32(binary.LittleEndian.Uint32(data[index*4:])))
			if _, ok := wanted[nextID]; ok {
				selected[base+index] = nextID
			}
			fdid = nextID + 1
		}
		base += count
	}

	typeIndex := len(c.RootTypes)
	if int64(numRecords) > input.remaining/int64(keyBytes) {
		return fmt.Errorf("root block key count %d exceeds remaining data", numRecords)
	}
	var key [24]byte
	for index := uint32(0); index < numRecords; index++ {
		if err := input.readFull(key[:keyBytes]); err != nil {
			return err
		}
		if id, ok := selected[index]; ok {
			contentKey := hex.EncodeToString(key[:16])
			c.RootEntries[id] = append(c.RootEntries[id], RootEntry{TypeIndex: typeIndex, ContentKey: contentKey})
		}
	}
	if !classic && !(allowNameless && contentFlags&ContentNoNameHash != 0) {
		if err := input.skip(int64(numRecords) * 8); err != nil {
			return err
		}
	}
	c.RootTypes = append(c.RootTypes, RootType{ContentFlags: contentFlags, LocaleFlags: localeFlags})
	return nil
}
