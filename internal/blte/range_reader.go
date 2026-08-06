package blte

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync"

	"wowdata/internal/resource"
)

const maxCoalescedCompressedBytes = 8 * 1024 * 1024

type CompressedRangeSource func(offset, length int) ([]byte, error)

type rangeBlock struct {
	once   sync.Once
	data   []byte
	err    error
	loaded bool
}

// RangeReader decodes only the compressed BLTE blocks needed for requested
// decompressed ranges. The source returns original byte ranges and is expected
// to provide persistence and integrity metadata when used over a network.
type RangeReader struct {
	header   *BLTEHeader
	source   CompressedRangeSource
	keys     KeyProvider
	blocks   []rangeBlock
	fetchMu  sync.RWMutex
	coalesce bool
}

func NewRangeReader(source CompressedRangeSource) (*RangeReader, error) {
	return NewRangeReaderWithKeys(source, defaultKeys)
}

func NewRangeReaderWithKeys(source CompressedRangeSource, keys KeyProvider) (*RangeReader, error) {
	if source == nil {
		return nil, fmt.Errorf("[BLTE] range source is nil")
	}
	prefix, err := source(0, 12)
	if err != nil {
		return nil, err
	}
	if len(prefix) != 12 || !Check(prefix) {
		return nil, fmt.Errorf("[BLTE] invalid ranged header prefix")
	}
	headerSize := int(binary.BigEndian.Uint32(prefix[4:8]))
	if headerSize < 12 {
		return nil, fmt.Errorf("[BLTE] ranged reader requires a framed header")
	}
	headerData := make([]byte, headerSize)
	copy(headerData, prefix)
	if headerSize > len(prefix) {
		remainder, fetchErr := source(len(prefix), headerSize-len(prefix))
		if fetchErr != nil {
			return nil, fetchErr
		}
		if len(remainder) != headerSize-len(prefix) {
			return nil, fmt.Errorf("[BLTE] ranged header returned %d bytes, want %d", len(remainder), headerSize-len(prefix))
		}
		copy(headerData[len(prefix):], remainder)
	}
	header := ParseBLTEHeader(headerData)
	if header == nil {
		return nil, fmt.Errorf("[BLTE] invalid ranged header")
	}
	return &RangeReader{
		header: header, source: source, keys: keys, blocks: make([]rangeBlock, len(header.Blocks)),
		coalesce: strings.TrimSpace(os.Getenv("WOWDATA_BLTE_COALESCE")) != "0",
	}, nil
}

func (r *RangeReader) Size() int { return r.header.TotalSize }

func (r *RangeReader) ReadRange(offset, length, workers int) ([]byte, error) {
	if offset < 0 || length < 0 || offset > r.header.TotalSize || length > r.header.TotalSize-offset {
		return nil, fmt.Errorf("[BLTE] decompressed range %d+%d exceeds size %d", offset, length, r.header.TotalSize)
	}
	if length == 0 {
		return []byte{}, nil
	}
	type overlap struct {
		index, outputOffset, blockOffset, length int
	}
	end := offset + length
	decompressedOffset := 0
	overlaps := make([]overlap, 0, 2)
	for index, block := range r.header.Blocks {
		blockEnd := decompressedOffset + block.DecompSize
		if blockEnd > offset && decompressedOffset < end {
			start := maxRangeInt(offset, decompressedOffset)
			stop := minRangeInt(end, blockEnd)
			overlaps = append(overlaps, overlap{index: index, outputOffset: start - offset, blockOffset: start - decompressedOffset, length: stop - start})
		}
		decompressedOffset = blockEnd
		if decompressedOffset >= end {
			break
		}
	}
	if r.coalesce && len(overlaps) > 1 {
		indices := make([]int, len(overlaps))
		for index, item := range overlaps {
			indices[index] = item.index
		}
		if err := r.prefetchBlocks(indices); err != nil {
			return nil, err
		}
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(overlaps) {
		workers = len(overlaps)
	}
	output := make([]byte, length)
	jobs := make(chan overlap)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				decoded, err := r.decodeBlock(item.index)
				if err == nil && item.blockOffset+item.length > len(decoded) {
					err = fmt.Errorf("[BLTE] block %d range exceeds decoded size", item.index)
				}
				if err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
					continue
				}
				copy(output[item.outputOffset:], decoded[item.blockOffset:item.blockOffset+item.length])
			}
		}()
	}
	for _, item := range overlaps {
		jobs <- item
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return output, nil
}

// ReadVerifiedRange can avoid fetching a whole uncompressed BLTE block when
// the enclosing format supplies a checksum for the requested logical range.
func (r *RangeReader) ReadVerifiedRange(offset, length int, expectedMD5 []byte, workers int) ([]byte, error) {
	if len(expectedMD5) != md5.Size {
		return nil, fmt.Errorf("[BLTE] verified range requires a 16-byte checksum")
	}
	blockIndex, blockOffset, ok := r.singleBlockRange(offset, length)
	if !ok {
		data, err := r.ReadRange(offset, length, workers)
		return verifyLogicalRange(data, expectedMD5, err)
	}
	block := r.header.Blocks[blockIndex]
	compressedOffset := r.header.DataStart + block.FileOffset
	typeByte, err := r.source(compressedOffset, 1)
	if err != nil || len(typeByte) != 1 || typeByte[0] != 0x4e {
		data, readErr := r.ReadRange(offset, length, workers)
		return verifyLogicalRange(data, expectedMD5, readErr)
	}
	data, err := r.source(compressedOffset+1+blockOffset, length)
	if err != nil {
		return nil, err
	}
	if len(data) != length {
		return nil, fmt.Errorf("[BLTE] direct range returned %d bytes, want %d", len(data), length)
	}
	return verifyLogicalRange(data, expectedMD5, nil)
}

func (r *RangeReader) singleBlockRange(offset, length int) (int, int, bool) {
	if offset < 0 || length < 0 || offset > r.header.TotalSize || length > r.header.TotalSize-offset {
		return 0, 0, false
	}
	decompressedOffset := 0
	for index, block := range r.header.Blocks {
		blockEnd := decompressedOffset + block.DecompSize
		if offset >= decompressedOffset && offset+length <= blockEnd {
			return index, offset - decompressedOffset, true
		}
		decompressedOffset = blockEnd
	}
	return 0, 0, false
}

func verifyLogicalRange(data, expected []byte, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	actual := md5.Sum(data)
	if !equalRangeBytes(actual[:], expected) {
		return nil, &IntegrityError{Expected: fmt.Sprintf("%x", expected), Actual: fmt.Sprintf("%x", actual)}
	}
	return data, nil
}

func equalRangeBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var different byte
	for index := range a {
		different |= a[index] ^ b[index]
	}
	return different == 0
}

func (r *RangeReader) decodeBlock(index int) ([]byte, error) {
	r.fetchMu.RLock()
	defer r.fetchMu.RUnlock()
	state := &r.blocks[index]
	state.once.Do(func() {
		defer func() { state.loaded = true }()
		block := r.header.Blocks[index]
		offset := r.header.DataStart + block.FileOffset
		raw, err := r.source(offset, block.CompSize)
		if err != nil {
			state.err = err
			return
		}
		if len(raw) != block.CompSize {
			state.err = fmt.Errorf("[BLTE] ranged block %d returned %d bytes, want %d", index, len(raw), block.CompSize)
			return
		}
		state.data, state.err = r.decodeRawBlock(index, raw)
	})
	return state.data, state.err
}

func (r *RangeReader) prefetchBlocks(indices []int) error {
	r.fetchMu.Lock()
	defer r.fetchMu.Unlock()
	for cursor := 0; cursor < len(indices); {
		if r.blocks[indices[cursor]].loaded {
			cursor++
			continue
		}
		end := cursor + 1
		length := r.header.Blocks[indices[cursor]].CompSize
		for end < len(indices) && indices[end] == indices[end-1]+1 && !r.blocks[indices[end]].loaded &&
			length <= maxCoalescedCompressedBytes-r.header.Blocks[indices[end]].CompSize {
			length += r.header.Blocks[indices[end]].CompSize
			end++
		}
		first := indices[cursor]
		last := indices[end-1]
		offset := r.header.DataStart + r.header.Blocks[first].FileOffset
		raw, err := r.source(offset, length)
		if err == nil && len(raw) != length {
			err = fmt.Errorf("[BLTE] ranged blocks %d-%d returned %d bytes, want %d", first, last, len(raw), length)
		}
		if err != nil {
			for _, index := range indices[cursor:end] {
				state := &r.blocks[index]
				state.once.Do(func() {
					state.err = err
					state.loaded = true
				})
			}
			return err
		}
		position := 0
		for _, index := range indices[cursor:end] {
			block := r.header.Blocks[index]
			blockRaw := raw[position : position+block.CompSize]
			state := &r.blocks[index]
			state.once.Do(func() {
				state.data, state.err = r.decodeRawBlock(index, blockRaw)
				state.loaded = true
			})
			if state.err != nil {
				return state.err
			}
			position += block.CompSize
		}
		cursor = end
	}
	return nil
}

func (r *RangeReader) decodeRawBlock(index int, raw []byte) ([]byte, error) {
	block := r.header.Blocks[index]
	if len(raw) != block.CompSize {
		return nil, fmt.Errorf("[BLTE] ranged block %d returned %d bytes, want %d", index, len(raw), block.CompSize)
	}
	if block.Hash != emptyHash {
		actual := fmt.Sprintf("%x", md5.Sum(raw))
		if actual != block.Hash {
			return nil, &IntegrityError{Expected: block.Hash, Actual: actual}
		}
	}
	decoder := &Reader{header: r.header, keys: r.keys}
	if err := decoder.handleBlockData(raw, 0, len(raw), index); err != nil {
		return nil, err
	}
	data := append([]byte(nil), decoder.buf.Bytes()...)
	if len(data) != block.DecompSize {
		return nil, fmt.Errorf("[BLTE] block %d decoded size %d, expected %d", index, len(data), block.DecompSize)
	}
	resource.RecordBLTEBlock(raw[0], block.CompSize, len(data))
	return data, nil
}

func minRangeInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxRangeInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
