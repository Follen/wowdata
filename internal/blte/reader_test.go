package blte

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"testing"

	"wowdata/internal/crypto"
	"wowdata/internal/resource"
	"wowdata/internal/tact"
)

// buildSingleBlockBLTE creates a BLTE with header_size=0 containing raw data.
// The first byte of data should be the block type flag (0x4E for normal).
func buildSingleBlockBLTE(payload []byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(0x45544c42)) // BLTE
	binary.Write(buf, binary.BigEndian, int32(0))              // header size
	buf.Write(payload)
	return buf.Bytes()
}

// buildMultiBlockBLTE creates a BLTE with explicit header and multiple blocks.
func buildMultiBlockBLTE(blocks []blteBlock) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(0x45544c42)) // magic

	headerSize := int32(24*len(blocks) + 12)
	binary.Write(buf, binary.BigEndian, headerSize)

	// Frame header: 0x0F + 3-byte big-endian block count
	buf.WriteByte(0x0F)
	buf.Write([]byte{byte(len(blocks) >> 16), byte(len(blocks) >> 8), byte(len(blocks))})

	for _, b := range blocks {
		binary.Write(buf, binary.BigEndian, int32(len(b.data)))
		binary.Write(buf, binary.BigEndian, int32(b.decompSize))
		binary.Write(buf, binary.BigEndian, uint64(0))
		binary.Write(buf, binary.BigEndian, uint64(0))
	}

	for _, b := range blocks {
		buf.Write(b.data)
	}

	return buf.Bytes()
}

type blteBlock struct {
	data       []byte
	decompSize int
}

func TestCheckBLTE(t *testing.T) {
	valid := buildSingleBlockBLTE([]byte{0x4E, 't', 'e', 's', 't'})
	if !Check(valid) {
		t.Fatal("Check failed for valid BLTE")
	}

	tooShort := []byte{0x42, 0x4C, 0x54}
	if Check(tooShort) {
		t.Fatal("Check should fail for < 4 bytes")
	}

	notBLTE := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if Check(notBLTE) {
		t.Fatal("Check should fail for non-BLTE magic")
	}
}

func TestParseBLTEHeaderSingleBlock(t *testing.T) {
	data := buildSingleBlockBLTE([]byte{0x4E, 'h', 'e', 'l', 'l', 'o'})
	meta := ParseBLTEHeader(data)

	if len(meta.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(meta.Blocks))
	}
	if meta.DataStart != 8 {
		t.Fatalf("dataStart = %d, want 8", meta.DataStart)
	}
}

func TestParseBLTEHeaderMultiBlock(t *testing.T) {
	blocks := []blteBlock{
		{data: []byte{0x4E, 'a', 'b', 'c'}, decompSize: 3},
		{data: []byte{0x4E, 'd', 'e', 'f'}, decompSize: 3},
	}
	data := buildMultiBlockBLTE(blocks)
	meta := ParseBLTEHeader(data)

	if len(meta.Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(meta.Blocks))
	}
	if meta.DataStart != 24*2+12 {
		t.Fatalf("dataStart = %d, want %d", meta.DataStart, 24*2+12)
	}
}

func TestReadSingleUncompressedBlock(t *testing.T) {
	plaintext := []byte("Hello, BLTE World!")
	payload := append([]byte{0x4E}, plaintext...)
	data := buildSingleBlockBLTE(payload)

	reader, err := NewReader(data)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	result, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(result, plaintext) {
		t.Fatalf("got %q, want %q", result, plaintext)
	}
}

func TestReadAllParallelPreservesBlockOrderAndCompression(t *testing.T) {
	compressBlock := func(value string) blteBlock {
		var compressed bytes.Buffer
		writer := zlib.NewWriter(&compressed)
		_, _ = writer.Write([]byte(value))
		_ = writer.Close()
		return blteBlock{data: append([]byte{0x5a}, compressed.Bytes()...), decompSize: len(value)}
	}
	data := buildMultiBlockBLTE([]blteBlock{
		{data: []byte{0x4e, 'a', 'b'}, decompSize: 2},
		compressBlock("cd"),
		{data: []byte{0x4e, 'e', 'f'}, decompSize: 2},
	})
	reader, err := NewReader(data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reader.ReadAllParallel(3)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(result); got != "abcdef" {
		t.Fatalf("parallel result = %q", got)
	}
}

func TestReadRangeCrossesBlocksAndReusesDecodedData(t *testing.T) {
	resource.ResetWorkMetrics()
	data := buildMultiBlockBLTE([]blteBlock{
		{data: []byte{0x4e, 'a', 'b', 'c'}, decompSize: 3},
		{data: []byte{0x4e, 'd', 'e', 'f'}, decompSize: 3},
		{data: []byte{0x4e, 'g', 'h', 'i'}, decompSize: 3},
	})
	reader, err := NewReader(data)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reader.ReadRange(2, 5, 2)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "cdefg" {
		t.Fatalf("range = %q", got)
	}
	again, err := reader.ReadRange(3, 3, 2)
	if err != nil || string(again) != "def" {
		t.Fatalf("reused range = %q, %v", again, err)
	}
	if len(reader.rangeBlocks) != 3 {
		t.Fatalf("cached blocks = %d", len(reader.rangeBlocks))
	}
	if units := workCounterUnits("blte-normal-decode"); units != 9 {
		t.Fatalf("normal decoded units = %d, want 9 without recounting cached blocks", units)
	}
}

func TestStreamReaderPreservesBlocksWithoutRangeCache(t *testing.T) {
	data := buildMultiBlockBLTE([]blteBlock{
		{data: []byte{0x4e, 'a', 'b', 'c'}, decompSize: 3},
		{data: []byte{0x4e, 'd', 'e', 'f'}, decompSize: 3},
		{data: []byte{0x4e, 'g', 'h', 'i'}, decompSize: 3},
	})
	reader, err := NewReader(data)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader.StreamReader())
	if err != nil || string(got) != "abcdefghi" {
		t.Fatalf("stream = %q, %v", got, err)
	}
	if len(reader.rangeBlocks) != 0 || reader.buf.Len() != 0 {
		t.Fatalf("stream retained decoded caches: ranges=%d buffer=%d", len(reader.rangeBlocks), reader.buf.Len())
	}
}

func TestReadZlibCompressedBlock(t *testing.T) {
	plaintext := []byte("compressed BLTE data with some more bytes to make it interesting")

	var compBuf bytes.Buffer
	w := zlib.NewWriter(&compBuf)
	w.Write(plaintext)
	w.Close()

	payload := append([]byte{0x5A}, compBuf.Bytes()...)
	data := buildSingleBlockBLTE(payload)

	reader, err := NewReader(data)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	result, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(result, plaintext) {
		t.Fatalf("decompressed result mismatch: got %d bytes, want %d", len(result), len(plaintext))
	}
}

func TestReadRecursiveFrameBlock(t *testing.T) {
	resource.ResetWorkMetrics()
	inner := buildSingleBlockBLTE(append([]byte{0x4E}, []byte("nested frame payload")...))
	outer := buildSingleBlockBLTE(append([]byte{0x46}, inner...))

	reader, err := NewReader(outer)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	result, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(result) != "nested frame payload" {
		t.Fatalf("result = %q", result)
	}
	if units := workCounterUnits("blte-nested-decode"); units != uint64(len(result)) {
		t.Fatalf("nested decoded units = %d, want %d", units, len(result))
	}
	if units := workCounterUnits("blte-normal-decode"); units != 0 {
		t.Fatalf("nested child was double counted as normal work: %d", units)
	}
}

func workCounterUnits(class string) uint64 {
	for _, counter := range resource.SnapshotWorkMetrics().Counters {
		if counter.Class == class {
			return counter.Units
		}
	}
	return 0
}

func TestReadEncryptedBlock(t *testing.T) {
	innerData := []byte("encrypted BLTE data for testing purposes!")

	nonce := []byte{0x01, 0x02, 0x03, 0x04, 0x00, 0x00, 0x00, 0x00}
	keyHex := "00112233445566778899aabbccddeeff" // 32 hex chars = 16 bytes
	keyBytes, _ := hex.DecodeString(keyHex)

	// Encrypt the inner block (type 0x4E = normal + data)
	innerPayload := append([]byte{0x4E}, innerData...)
	ciphertext := make([]byte, len(innerPayload))
	salsa, _ := crypto.NewSalsa20(nonce, keyBytes, 20)
	salsa.Process(ciphertext, innerPayload)

	// Build encrypted block: keyName (8 bytes, reversed on wire), ivSize, IV (4 bytes), encType (0x53), payload
	keyName := "0123456789abcdef"
	keyNameRaw, _ := hex.DecodeString(keyName)
	// BLTE stores key name bytes in reverse order
	for i, j := 0, len(keyNameRaw)-1; i < j; i, j = i+1, j-1 {
		keyNameRaw[i], keyNameRaw[j] = keyNameRaw[j], keyNameRaw[i]
	}
	var encBuf bytes.Buffer
	encBuf.WriteByte(8) // keyNameSize
	encBuf.Write(keyNameRaw)
	encBuf.WriteByte(4) // ivSize
	encBuf.Write(nonce[:4])
	encBuf.WriteByte(0x53) // Salsa20
	encBuf.Write(ciphertext)

	payload := append([]byte{0x45}, encBuf.Bytes()...)
	data := buildSingleBlockBLTE(payload)

	tactKeys := tact.NewKeyRing()
	tactKeys.AddKey(keyName, keyHex)

	reader, err := NewReaderWithKeys(data, tactKeys)
	if err != nil {
		t.Fatalf("NewReaderWithKeys: %v", err)
	}

	result, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(result, innerData) {
		t.Fatalf("decrypted result mismatch")
	}
}

func TestReadEncryptedMissingKey(t *testing.T) {
	nonce := []byte{0x01, 0x02, 0x03, 0x04, 0x00, 0x00, 0x00, 0x00}
	keyHex := "00112233445566778899aabbccddeeff"
	keyBytes, _ := hex.DecodeString(keyHex)

	innerPayload := append([]byte{0x4E}, []byte("encrypted with unknown key")...)
	ciphertext := make([]byte, len(innerPayload))
	salsa, _ := crypto.NewSalsa20(nonce, keyBytes, 20)
	salsa.Process(ciphertext, innerPayload)

	keyName := "deadbeefdeadbeef"
	keyNameRaw, _ := hex.DecodeString(keyName)
	for i, j := 0, len(keyNameRaw)-1; i < j; i, j = i+1, j-1 {
		keyNameRaw[i], keyNameRaw[j] = keyNameRaw[j], keyNameRaw[i]
	}
	var encBuf bytes.Buffer
	encBuf.WriteByte(8)
	encBuf.Write(keyNameRaw)
	encBuf.WriteByte(4)
	encBuf.Write(nonce[:4])
	encBuf.WriteByte(0x53)
	encBuf.Write(ciphertext)

	payload := append([]byte{0x45}, encBuf.Bytes()...)
	data := buildSingleBlockBLTE(payload)

	reader, err := NewReader(data)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	_, err = reader.ReadAll()
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	encErr, ok := err.(*EncryptionError)
	if !ok {
		t.Fatalf("expected EncryptionError, got %T: %v", err, err)
	}
	if encErr.Key != keyName {
		t.Fatalf("error key = %q, want %q", encErr.Key, keyName)
	}
}

func TestBlockHashValidation(t *testing.T) {
	plaintext := []byte("hash verified BLTE data!")

	var compBuf bytes.Buffer
	w := zlib.NewWriter(&compBuf)
	w.Write(plaintext)
	w.Close()
	compressed := compBuf.Bytes()

	payload := append([]byte{0x5A}, compressed...)

	// Build multi-block BLTE with proper hashes
	hash := md5.Sum(payload)
	hashHex := hex.EncodeToString(hash[:])

	block := blteBlock{data: payload, decompSize: len(plaintext)}
	blocks := []blteBlock{block}
	data := buildMultiBlockBLTE(blocks)

	// Overwrite hash bytes (bytes 20-35 in header, after 0x0F + 3B numBlocks)
	hashBytes, _ := hex.DecodeString(hashHex)
	copy(data[20:], hashBytes)

	reader, err := NewReader(data)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	result, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(result, plaintext) {
		t.Fatalf("decompressed result mismatch")
	}
}

func TestBlockHashMismatch(t *testing.T) {
	plaintext := []byte("data for bad hash test!")

	var compBuf bytes.Buffer
	w := zlib.NewWriter(&compBuf)
	w.Write(plaintext)
	w.Close()
	compressed := compBuf.Bytes()

	payload := append([]byte{0x5A}, compressed...)
	block := blteBlock{data: payload, decompSize: len(plaintext)}
	data := buildMultiBlockBLTE([]blteBlock{block})

	// Write wrong hash
	wrongHash, _ := hex.DecodeString("deadbeefdeadbeefdeadbeefdeadbeef")
	copy(data[20:], wrongHash)

	reader, err := NewReader(data)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	_, err = reader.ReadAll()
	if err == nil {
		t.Fatal("expected integrity error")
	}
	intErr, ok := err.(*IntegrityError)
	if !ok {
		t.Fatalf("expected IntegrityError, got %T: %v", err, err)
	}
	if intErr.Expected == "" {
		t.Fatal("expected error has empty Expected hash")
	}
}

func TestParseHeaderErrors(t *testing.T) {
	// Too short
	if meta := ParseBLTEHeader([]byte{0x42, 0x4C, 0x54}); meta != nil {
		t.Fatal("expected nil for < 8 bytes")
	}

	// Wrong magic
	wrong := make([]byte, 8)
	binary.LittleEndian.PutUint32(wrong, 0xDEADBEEF)
	if meta := ParseBLTEHeader(wrong); meta != nil {
		t.Fatal("expected nil for wrong magic")
	}

	// Invalid frame header (headerSize > 0 but too small)
	invalid := make([]byte, 12)
	binary.LittleEndian.PutUint32(invalid, 0x45544c42) // magic
	binary.BigEndian.PutUint32(invalid[4:], 100)       // headerSize = 100
	if meta := ParseBLTEHeader(invalid); meta != nil {
		t.Fatal("expected nil for data shorter than headerSize")
	}
}

func TestMultiBlockDecode(t *testing.T) {
	plaintexts := [][]byte{
		[]byte("block one data here!"),
		[]byte("block two is larger with more content to decode"),
	}
	var blocks []blteBlock
	for _, pt := range plaintexts {
		payload := append([]byte{0x4E}, pt...)
		hash := md5.Sum(payload)
		blocks = append(blocks, blteBlock{data: payload, decompSize: len(pt)})

		_ = hash // used to construct header, but buildMultiBlock writes zeros
	}
	data := buildMultiBlockBLTE(blocks)

	// Write correct hashes
	for i, b := range blocks {
		h := md5.Sum(b.data)
		offset := 12 + i*24 + 8 // after 0x0F+3numBlocks + CompSize(4) + DecompSize(4)
		copy(data[offset:], h[:])
	}

	reader, err := NewReader(data)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	result, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	expected := append([]byte(nil), plaintexts[0]...)
	expected = append(expected, plaintexts[1]...)
	if !bytes.Equal(result, expected) {
		t.Fatalf("multi-block result mismatch")
	}
}

// Error type checks
func TestEncryptionErrorInterface(t *testing.T) {
	err := &EncryptionError{Key: "testkey"}
	if err.Error() == "" {
		t.Fatal("empty error string")
	}
}

func TestIntegrityErrorInterface(t *testing.T) {
	err := &IntegrityError{Expected: "aaa", Actual: "bbb"}
	_ = fmt.Sprint(err)
	if err.Error() == "" {
		t.Fatal("empty error string")
	}
}
