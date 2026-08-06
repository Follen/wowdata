package blte

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"wowdata/internal/crypto"
	"wowdata/internal/resource"
	"wowdata/internal/tact"
)

const (
	blteMagic      = 0x45544c42
	encTypeSalsa20 = 0x53
	emptyHash      = "00000000000000000000000000000000"
)

type EncryptionError struct {
	Key string
}

func (e *EncryptionError) Error() string {
	return "[BLTE] Missing decryption key " + e.Key
}

type IntegrityError struct {
	Expected string
	Actual   string
}

func (e *IntegrityError) Error() string {
	return fmt.Sprintf("[BLTE] Invalid block data hash. Expected %s, got %s!", e.Expected, e.Actual)
}

type BlockMeta struct {
	CompSize   int
	DecompSize int
	Hash       string
	FileOffset int
}

type BLTEHeader struct {
	Blocks    []BlockMeta
	DataStart int
	TotalSize int
}

type KeyProvider interface {
	GetKey(keyName string) (string, error)
}

// Check returns true if data starts with the BLTE magic number.
func Check(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	return binary.LittleEndian.Uint32(data) == blteMagic
}

// ParseBLTEHeader parses a BLTE header without allocating a full reader.
// Returns nil on parse errors (invalid magic, too short, etc).
func ParseBLTEHeader(data []byte) *BLTEHeader {
	size := len(data)
	if size < 8 {
		return nil
	}

	magic := binary.LittleEndian.Uint32(data)
	if magic != blteMagic {
		return nil
	}

	rawHeaderSize := uint64(binary.BigEndian.Uint32(data[4:]))
	if rawHeaderSize > uint64(size) || rawHeaderSize > uint64(^uint(0)>>1) {
		return nil
	}
	headerSize := int(rawHeaderSize)
	numBlocks := 1
	dataStart := 8

	if headerSize > 0 {
		if size < 12 {
			return nil
		}
		if data[8] != 0x0F {
			return nil
		}
		numBlocks = int(data[9])<<16 | int(data[10])<<8 | int(data[11])
		if numBlocks == 0 {
			return nil
		}
		frameHeaderSize := 24*numBlocks + 12
		if headerSize != frameHeaderSize {
			return nil
		}
		if size < frameHeaderSize {
			return nil
		}
		dataStart = headerSize
	}

	blocks := make([]BlockMeta, numBlocks)
	fileOffset := 0
	totalDecompSize := 0

	for i := 0; i < numBlocks; i++ {
		if headerSize != 0 {
			pos := 12 + i*24
			compressed := uint64(binary.BigEndian.Uint32(data[pos:]))
			decompressed := uint64(binary.BigEndian.Uint32(data[pos+4:]))
			if compressed == 0 || decompressed > uint64(^uint(0)>>1) || compressed > uint64(^uint(0)>>1) {
				return nil
			}
			blocks[i].CompSize = int(compressed)
			blocks[i].DecompSize = int(decompressed)
			blocks[i].Hash = hex.EncodeToString(data[pos+8 : pos+24])
		} else {
			blocks[i].CompSize = size - 8
			blocks[i].DecompSize = size - 9
			blocks[i].Hash = emptyHash
		}
		if blocks[i].CompSize < 0 || blocks[i].DecompSize < 0 || fileOffset > int(^uint(0)>>1)-blocks[i].CompSize || totalDecompSize > int(^uint(0)>>1)-blocks[i].DecompSize {
			return nil
		}
		blocks[i].FileOffset = fileOffset
		fileOffset += blocks[i].CompSize
		totalDecompSize += blocks[i].DecompSize
	}

	return &BLTEHeader{
		Blocks:    blocks,
		DataStart: dataStart,
		TotalSize: totalDecompSize,
	}
}

type Reader struct {
	data    []byte
	header  *BLTEHeader
	keys    KeyProvider
	partial bool
	meter   bool

	blockIndex      int
	blockWriteIndex int
	buf             bytes.Buffer
	bltePos         int
	originalOffset  int
	rangeMu         sync.Mutex
	rangeBlocks     map[int][]byte
}

func NewReader(data []byte) (*Reader, error) {
	return newReader(data, defaultKeys, false)
}

func NewReaderFromBytes(data []byte) (*Reader, error) { return NewReader(data) }

func NewReaderWithKeys(data []byte, keys KeyProvider) (*Reader, error) {
	return newReader(data, keys, false)
}

func NewPartialReader(data []byte) (*Reader, error) {
	return newReader(data, defaultKeys, true)
}

var defaultKeys KeyProvider

func SetDefaultKeyProvider(keys KeyProvider) {
	defaultKeys = keys
}

var _ KeyProvider = (*tact.KeyRing)(nil)

func newReader(data []byte, keys KeyProvider, partial bool) (*Reader, error) {
	header := ParseBLTEHeader(data)
	if header == nil {
		return nil, fmt.Errorf("[BLTE] invalid header")
	}
	return &Reader{
		data:    data,
		header:  header,
		keys:    keys,
		partial: partial,
		meter:   true,
		bltePos: header.DataStart,
	}, nil
}

func (r *Reader) ReadAll() ([]byte, error) {
	for r.blockIndex < len(r.header.Blocks) {
		if err := r.processBlock(); err != nil {
			return nil, err
		}
	}
	return r.buf.Bytes(), nil
}

// ReadAllParallel decodes independent BLTE blocks concurrently while writing
// them into their deterministic decompressed offsets. Memory is bounded by the
// final output plus at most one compressed block result per worker.
func (r *Reader) ReadAllParallel(workers int) ([]byte, error) {
	if workers <= 1 || len(r.header.Blocks) <= 1 {
		return r.ReadAll()
	}
	if r.blockIndex != 0 || r.buf.Len() != 0 {
		return nil, fmt.Errorf("[BLTE] parallel read requires a fresh reader")
	}
	if workers > len(r.header.Blocks) {
		workers = len(r.header.Blocks)
	}
	offsets := make([]int, len(r.header.Blocks))
	total := 0
	for i, block := range r.header.Blocks {
		if block.DecompSize < 0 || total > int(^uint(0)>>1)-block.DecompSize {
			return nil, fmt.Errorf("[BLTE] invalid decompressed size for block %d", i)
		}
		offsets[i] = total
		total += block.DecompSize
	}
	output := make([]byte, total)
	jobs := make(chan int)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				decoded, err := r.decodeBlock(index)
				if err == nil && len(decoded) != r.header.Blocks[index].DecompSize {
					err = fmt.Errorf("[BLTE] Block %d decoded size %d, expected %d", index, len(decoded), r.header.Blocks[index].DecompSize)
				}
				if err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
					continue
				}
				copy(output[offsets[index]:], decoded)
			}
		}()
	}
	for index := range r.header.Blocks {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	r.blockIndex = len(r.header.Blocks)
	r.bltePos = len(r.data)
	return output, nil
}

func (r *Reader) Size() int { return r.header.TotalSize }

// StreamReader decodes one BLTE block at a time and releases it before moving
// to the next block. It is intended for forward-only parsers that should not
// materialize or cache the full decompressed payload.
func (r *Reader) StreamReader() io.Reader {
	return &streamReader{reader: r}
}

type streamReader struct {
	reader *Reader
	index  int
	block  []byte
	offset int
	err    error
}

func (s *streamReader) Read(output []byte) (int, error) {
	if len(output) == 0 {
		return 0, nil
	}
	for s.offset >= len(s.block) {
		if s.err != nil {
			return 0, s.err
		}
		if s.index >= len(s.reader.header.Blocks) {
			return 0, io.EOF
		}
		s.block, s.err = s.reader.decodeBlock(s.index)
		s.index++
		s.offset = 0
		if s.err != nil {
			return 0, s.err
		}
	}
	n := copy(output, s.block[s.offset:])
	s.offset += n
	if s.offset == len(s.block) {
		s.block = nil
		s.offset = 0
	}
	return n, nil
}

// ReadRange decodes only BLTE blocks overlapping a decompressed byte range.
// Decoded blocks are retained for subsequent range probes on the same reader.
func (r *Reader) ReadRange(offset, length, workers int) ([]byte, error) {
	if offset < 0 || length < 0 || offset > r.header.TotalSize || length > r.header.TotalSize-offset {
		return nil, fmt.Errorf("[BLTE] decompressed range %d+%d exceeds size %d", offset, length, r.header.TotalSize)
	}
	if length == 0 {
		return []byte{}, nil
	}
	type overlap struct {
		index, outputOffset, blockOffset, length int
	}
	overlaps := make([]overlap, 0, 2)
	decompressedOffset := 0
	end := offset + length
	for index, block := range r.header.Blocks {
		blockEnd := decompressedOffset + block.DecompSize
		if blockEnd > offset && decompressedOffset < end {
			start := offset
			if decompressedOffset > start {
				start = decompressedOffset
			}
			stop := end
			if blockEnd < stop {
				stop = blockEnd
			}
			overlaps = append(overlaps, overlap{index: index, outputOffset: start - offset, blockOffset: start - decompressedOffset, length: stop - start})
		}
		decompressedOffset = blockEnd
		if decompressedOffset >= end {
			break
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
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				decoded, err := r.decodeRangeBlock(item.index)
				if err == nil && item.blockOffset+item.length > len(decoded) {
					err = fmt.Errorf("[BLTE] Block %d range exceeds decoded size", item.index)
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

func (r *Reader) decodeRangeBlock(index int) ([]byte, error) {
	r.rangeMu.Lock()
	if decoded, ok := r.rangeBlocks[index]; ok {
		r.rangeMu.Unlock()
		return decoded, nil
	}
	r.rangeMu.Unlock()
	decoded, err := r.decodeBlock(index)
	if err != nil {
		return nil, err
	}
	// Normal blocks alias immutable input. Compressed blocks own their buffer.
	r.rangeMu.Lock()
	if r.rangeBlocks == nil {
		r.rangeBlocks = make(map[int][]byte)
	}
	if existing, ok := r.rangeBlocks[index]; ok {
		decoded = existing
	} else {
		r.rangeBlocks[index] = decoded
	}
	r.rangeMu.Unlock()
	return decoded, nil
}

func (r *Reader) decodeBlock(index int) ([]byte, error) {
	block := r.header.Blocks[index]
	blockStart := r.header.DataStart + block.FileOffset
	blockEnd := blockStart + block.CompSize
	if block.CompSize < 0 || blockStart < 0 || blockEnd < blockStart || blockEnd > len(r.data) {
		return nil, fmt.Errorf("[BLTE] Block %d bounds %d-%d exceed data size %d", index, blockStart, blockEnd, len(r.data))
	}
	if block.Hash != emptyHash {
		actualHash := fmt.Sprintf("%x", md5.Sum(r.data[blockStart:blockEnd]))
		if actualHash != block.Hash {
			return nil, &IntegrityError{Expected: block.Hash, Actual: actualHash}
		}
	}
	if blockStart < blockEnd && r.data[blockStart] == 0x4e {
		decoded := r.data[blockStart+1 : blockEnd]
		if r.meter {
			resource.RecordBLTEBlock(r.data[blockStart], block.CompSize, len(decoded))
		}
		return decoded, nil
	}
	decoder := &Reader{data: r.data, header: r.header, keys: r.keys, partial: r.partial}
	if err := decoder.handleBlockData(r.data, blockStart, blockEnd, index); err != nil {
		return nil, err
	}
	decoded := decoder.buf.Bytes()
	if r.meter {
		resource.RecordBLTEBlock(r.data[blockStart], block.CompSize, len(decoded))
	}
	return decoded, nil
}

func (r *Reader) processBlock() error {
	if r.blockIndex >= len(r.header.Blocks) {
		return io.EOF
	}

	block := r.header.Blocks[r.blockIndex]
	blockStart := r.bltePos
	blockEnd := blockStart + block.CompSize
	if block.CompSize < 0 || blockStart < 0 || blockEnd < blockStart || blockEnd > len(r.data) {
		return fmt.Errorf("[BLTE] Block %d bounds %d-%d exceed data size %d", r.blockIndex, blockStart, blockEnd, len(r.data))
	}

	if block.Hash != emptyHash {
		blockData := r.data[blockStart:blockEnd]
		actualHash := fmt.Sprintf("%x", md5.Sum(blockData))
		if actualHash != block.Hash {
			return &IntegrityError{Expected: block.Hash, Actual: actualHash}
		}
	}

	before := r.buf.Len()
	if err := r.handleBlock(blockStart, blockEnd, r.blockIndex); err != nil {
		return err
	}
	if r.meter {
		resource.RecordBLTEBlock(r.data[blockStart], block.CompSize, r.buf.Len()-before)
	}

	r.bltePos = blockEnd
	r.blockIndex++
	return nil
}

func (r *Reader) handleBlock(blockStart, blockEnd int, index int) error {
	return r.handleBlockData(r.data, blockStart, blockEnd, index)
}

func (r *Reader) handleBlockData(data []byte, blockStart, blockEnd int, index int) error {
	if blockStart < 0 || blockStart >= blockEnd || blockEnd > len(data) {
		return fmt.Errorf("[BLTE] Invalid block bounds")
	}
	flag := data[blockStart]
	switch flag {
	case 0x45: // Encrypted
		decrypted, err := r.decryptBlockData(data, blockStart+1, blockEnd, index)
		if err != nil {
			if _, ok := err.(*EncryptionError); ok && r.partial {
				zeroes := make([]byte, r.header.Blocks[index].DecompSize)
				r.buf.Write(zeroes)
				return nil
			}
			return err
		}
		return r.handleBlockData(decrypted, 0, len(decrypted), index)

	case 0x46: // Frame (recursive)
		return r.decodeFrameBlock(data, blockStart+1, blockEnd)

	case 0x4E: // Normal (uncompressed)
		r.buf.Write(data[blockStart+1 : blockEnd])
		return nil

	case 0x5A: // Zlib compressed
		return r.decompressBlockData(data, blockStart+1, blockEnd, index)

	default:
		return fmt.Errorf("Unknown BLTE block type: 0x%02X", flag)
	}
}

func (r *Reader) decodeFrameBlock(data []byte, blockStart, blockEnd int) error {
	if blockStart > blockEnd || blockStart < 0 || blockEnd > len(data) {
		return fmt.Errorf("[BLTE] Invalid frame bounds")
	}
	nested, err := newReader(data[blockStart:blockEnd], r.keys, r.partial)
	if err != nil {
		return err
	}
	nested.meter = false
	decoded, err := nested.ReadAll()
	if err != nil {
		return err
	}
	r.buf.Write(decoded)
	return nil
}

func (r *Reader) decompressBlockData(data []byte, blockStart, blockEnd int, index int) error {
	if blockStart < 0 || blockStart > blockEnd || blockEnd > len(data) {
		return fmt.Errorf("[BLTE] Invalid compressed block bounds")
	}
	zr, err := zlib.NewReader(bytes.NewReader(data[blockStart:blockEnd]))
	if err != nil {
		return err
	}
	defer zr.Close()

	decompressed, err := io.ReadAll(zr)
	if err != nil {
		return err
	}

	r.buf.Write(decompressed)
	return nil
}

func (r *Reader) decryptBlock(blockStart, blockEnd int, index int) ([]byte, error) {
	return r.decryptBlockData(r.data, blockStart, blockEnd, index)
}

func (r *Reader) decryptBlockData(data []byte, blockStart, blockEnd int, index int) ([]byte, error) {
	pos := blockStart
	if pos >= blockEnd || pos < 0 || blockEnd > len(data) {
		return nil, fmt.Errorf("[BLTE] Invalid encrypted block bounds")
	}
	keyNameSize := data[pos]
	pos++
	if keyNameSize == 0 || keyNameSize != 8 {
		return nil, fmt.Errorf("[BLTE] Unexpected keyNameSize: %d", keyNameSize)
	}
	if pos+int(keyNameSize) > blockEnd {
		return nil, fmt.Errorf("[BLTE] Unexpected end of data in key name")
	}

	keyNameBytes := make([]string, keyNameSize)
	for i := byte(0); i < keyNameSize; i++ {
		keyNameBytes[keyNameSize-1-i] = fmt.Sprintf("%02x", data[pos])
		pos++
	}
	keyName := ""
	for _, s := range keyNameBytes {
		keyName += s
	}

	if pos >= blockEnd {
		return nil, fmt.Errorf("[BLTE] Unexpected end of data before iv size")
	}
	ivSize := data[pos]
	pos++
	if (ivSize != 4 && ivSize != 8) || ivSize > 8 {
		return nil, fmt.Errorf("[BLTE] Unexpected ivSize: %d", ivSize)
	}
	if pos+int(ivSize) > blockEnd {
		return nil, fmt.Errorf("[BLTE] Unexpected end of data in iv")
	}

	ivShort := make([]byte, ivSize)
	copy(ivShort, data[pos:pos+int(ivSize)])
	pos += int(ivSize)

	if pos >= blockEnd {
		return nil, fmt.Errorf("[BLTE] Unexpected end of data before encryption flag")
	}

	encryptType := data[pos]
	pos++
	if encryptType != encTypeSalsa20 {
		return nil, fmt.Errorf("[BLTE] Unexpected encryption type: 0x%02X", encryptType)
	}

	for i := 0; i < 4; i++ {
		ivShort[i] = ivShort[i] ^ (byte(index>>(i*8)) & 0xFF)
	}

	if r.keys == nil {
		return nil, &EncryptionError{Key: keyName}
	}

	key, err := r.keys.GetKey(keyName)
	if err != nil {
		return nil, &EncryptionError{Key: keyName}
	}

	keyBytes, err := hex.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("[BLTE] invalid key hex: %w", err)
	}

	nonce := make([]byte, 8)
	copy(nonce, ivShort)

	salsa, err := crypto.NewSalsa20(nonce, keyBytes, 20)
	if err != nil {
		return nil, err
	}

	ciphertext := data[pos:blockEnd]
	plaintext := make([]byte, len(ciphertext))
	salsa.Process(plaintext, ciphertext)

	return plaintext, nil
}
