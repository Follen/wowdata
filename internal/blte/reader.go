package blte

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"

	"wowdata/internal/crypto"
)

const (
	blteMagic       = 0x45544c42
	encTypeSalsa20  = 0x53
	emptyHash       = "00000000000000000000000000000000"
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

	headerSize := int(int32(binary.BigEndian.Uint32(data[4:])))
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
			blocks[i].CompSize = int(int32(binary.BigEndian.Uint32(data[pos:])))
			blocks[i].DecompSize = int(int32(binary.BigEndian.Uint32(data[pos+4:])))
			blocks[i].Hash = hex.EncodeToString(data[pos+8 : pos+24])
		} else {
			blocks[i].CompSize = size - 8
			blocks[i].DecompSize = size - 9
			blocks[i].Hash = emptyHash
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
	data     []byte
	header   *BLTEHeader
	keys     KeyProvider
	partial  bool

	blockIndex      int
	blockWriteIndex int
	buf             bytes.Buffer
	bltePos         int
	originalOffset  int
}

func NewReader(data []byte) (*Reader, error) {
	return newReader(data, nil, false)
}

func NewReaderWithKeys(data []byte, keys KeyProvider) (*Reader, error) {
	return newReader(data, keys, false)
}

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

func (r *Reader) processBlock() error {
	if r.blockIndex >= len(r.header.Blocks) {
		return io.EOF
	}

	block := r.header.Blocks[r.blockIndex]
	blockStart := r.bltePos
	blockEnd := blockStart + block.CompSize

	if block.Hash != emptyHash {
		blockData := r.data[blockStart:blockEnd]
		actualHash := fmt.Sprintf("%x", md5.Sum(blockData))
		if actualHash != block.Hash {
			return &IntegrityError{Expected: block.Hash, Actual: actualHash}
		}
	}

	if err := r.handleBlock(blockStart, blockEnd, r.blockIndex); err != nil {
		return err
	}

	r.bltePos = blockEnd
	r.blockIndex++
	return nil
}

func (r *Reader) handleBlock(blockStart, blockEnd int, index int) error {
	return r.handleBlockData(r.data, blockStart, blockEnd, index)
}

func (r *Reader) handleBlockData(data []byte, blockStart, blockEnd int, index int) error {
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

	case 0x46: // Frame (recursive) — not implemented
		return fmt.Errorf("[BLTE] No frame decoder implemented!")

	case 0x4E: // Normal (uncompressed)
		r.buf.Write(data[blockStart+1 : blockEnd])
		return nil

	case 0x5A: // Zlib compressed
		return r.decompressBlockData(data, blockStart+1, blockEnd, index)

	default:
		return fmt.Errorf("Unknown BLTE block type: 0x%02X", flag)
	}
}

func (r *Reader) decompressBlockData(data []byte, blockStart, blockEnd int, index int) error {
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
	keyNameSize := data[pos]
	pos++
	if keyNameSize == 0 || keyNameSize != 8 {
		return nil, fmt.Errorf("[BLTE] Unexpected keyNameSize: %d", keyNameSize)
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

	ivSize := data[pos]
	pos++
	if (ivSize != 4 && ivSize != 8) || ivSize > 8 {
		return nil, fmt.Errorf("[BLTE] Unexpected ivSize: %d", ivSize)
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
