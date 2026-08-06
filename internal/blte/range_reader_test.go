package blte

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
)

func buildRangedBLTE(blocks ...[]byte) []byte {
	headerSize := 12 + len(blocks)*24
	data := make([]byte, headerSize)
	copy(data, "BLTE")
	binary.BigEndian.PutUint32(data[4:], uint32(headerSize))
	data[8] = 0x0f
	data[9] = byte(len(blocks) >> 16)
	data[10] = byte(len(blocks) >> 8)
	data[11] = byte(len(blocks))
	for index, payload := range blocks {
		raw := append([]byte{0x4e}, payload...)
		pos := 12 + index*24
		binary.BigEndian.PutUint32(data[pos:], uint32(len(raw)))
		binary.BigEndian.PutUint32(data[pos+4:], uint32(len(payload)))
		hash := md5.Sum(raw)
		copy(data[pos+8:], hash[:])
		data = append(data, raw...)
	}
	return data
}

func TestRangeReaderFetchesOnlyOverlappingBlocks(t *testing.T) {
	data := buildRangedBLTE([]byte("abcd"), []byte("efgh"), []byte("ijkl"))
	fetched := make(map[string]int)
	reader, err := NewRangeReader(func(offset, length int) ([]byte, error) {
		fetched[fmt.Sprintf("%d:%d", offset, length)]++
		return append([]byte(nil), data[offset:offset+length]...), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := reader.ReadRange(5, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fg" {
		t.Fatalf("range = %q", got)
	}
	if len(fetched) != 3 {
		t.Fatalf("fetches = %#v", fetched)
	}
	if _, err := reader.ReadRange(4, 4, 2); err != nil {
		t.Fatal(err)
	}
	if len(fetched) != 3 {
		t.Fatalf("cached fetches = %#v", fetched)
	}
}

func TestRangeReaderVerifiesBlockHash(t *testing.T) {
	data := buildRangedBLTE([]byte("abcd"))
	reader, err := NewRangeReader(func(offset, length int) ([]byte, error) {
		result := append([]byte(nil), data[offset:offset+length]...)
		if offset >= 36 {
			result[len(result)-1] ^= 0xff
		}
		return result, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadRange(0, 4, 1); err == nil {
		t.Fatal("expected integrity error")
	}
}

func TestRangeReaderCoalescesAdjacentBlocks(t *testing.T) {
	data := buildRangedBLTE([]byte("abcd"), []byte("efgh"), []byte("ijkl"))
	fetched := make(map[string]int)
	var mu sync.Mutex
	reader, err := NewRangeReader(func(offset, length int) ([]byte, error) {
		mu.Lock()
		fetched[fmt.Sprintf("%d:%d", offset, length)]++
		mu.Unlock()
		return append([]byte(nil), data[offset:offset+length]...), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	const readers = 8
	var wg sync.WaitGroup
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, readErr := reader.ReadRange(0, 12, 3)
			if readErr != nil || string(got) != "abcdefghijkl" {
				t.Errorf("range=%q err=%v", got, readErr)
			}
		}()
	}
	wg.Wait()
	if len(fetched) != 3 {
		t.Fatalf("fetches=%#v", fetched)
	}
	headerSize := 12 + 3*24
	if got := fetched[fmt.Sprintf("%d:%d", headerSize, 15)]; got != 1 {
		t.Fatalf("coalesced fetch count=%d, all=%#v", got, fetched)
	}
}

func TestRangeReaderCanDisableCoalescingForTournament(t *testing.T) {
	t.Setenv("WOWDATA_BLTE_COALESCE", "0")
	data := buildRangedBLTE([]byte("abcd"), []byte("efgh"), []byte("ijkl"))
	fetched := make(map[string]int)
	var mu sync.Mutex
	reader, err := NewRangeReader(func(offset, length int) ([]byte, error) {
		mu.Lock()
		fetched[fmt.Sprintf("%d:%d", offset, length)]++
		mu.Unlock()
		return append([]byte(nil), data[offset:offset+length]...), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadRange(0, 12, 3); err != nil {
		t.Fatal(err)
	}
	if len(fetched) != 5 {
		t.Fatalf("fetches=%#v", fetched)
	}
}

func TestRangeReaderCoalescedFetchVerifiesEveryBlock(t *testing.T) {
	data := buildRangedBLTE([]byte("abcd"), []byte("efgh"), []byte("ijkl"))
	headerSize := 12 + 3*24
	reader, err := NewRangeReader(func(offset, length int) ([]byte, error) {
		result := append([]byte(nil), data[offset:offset+length]...)
		if offset == headerSize && length == 15 {
			result[7] ^= 0xff
		}
		return result, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadRange(0, 12, 3); err == nil {
		t.Fatal("expected integrity error")
	}
}

func TestRangeReaderCoalescesConcurrentPartialOverlapWithoutDuplicateBlocks(t *testing.T) {
	data := buildRangedBLTE([]byte("abcd"), []byte("efgh"), []byte("ijkl"))
	headerSize := 12 + 3*24
	var mu sync.Mutex
	blockCalls := 0
	blockBytes := 0
	reader, err := NewRangeReader(func(offset, length int) ([]byte, error) {
		if offset >= headerSize {
			mu.Lock()
			blockCalls++
			blockBytes += length
			mu.Unlock()
		}
		return append([]byte(nil), data[offset:offset+length]...), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan string, 2)
	var wg sync.WaitGroup
	for _, span := range [][2]int{{0, 8}, {4, 8}} {
		span := span
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, readErr := reader.ReadRange(span[0], span[1], 2)
			if readErr != nil {
				t.Errorf("ReadRange(%v): %v", span, readErr)
				return
			}
			results <- string(got)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for result := range results {
		seen[result] = true
	}
	if !seen["abcdefgh"] || !seen["efghijkl"] {
		t.Fatalf("results=%#v", seen)
	}
	if blockCalls != 2 || blockBytes != 15 {
		t.Fatalf("block calls=%d bytes=%d", blockCalls, blockBytes)
	}
}

func TestRangeReaderUsesLogicalChecksumInsideNormalBlock(t *testing.T) {
	data := buildRangedBLTE([]byte("abcdefghijkl"))
	fetches := make(map[string]int)
	reader, err := NewRangeReader(func(offset, length int) ([]byte, error) {
		fetches[fmt.Sprintf("%d:%d", offset, length)]++
		return append([]byte(nil), data[offset:offset+length]...), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := md5.Sum([]byte("def"))
	got, err := reader.ReadVerifiedRange(3, 3, expected[:], 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "def" {
		t.Fatalf("range = %q", got)
	}
	blockLength := len("abcdefghijkl") + 1
	if fetches[fmt.Sprintf("36:%d", blockLength)] != 0 {
		t.Fatalf("whole block was fetched: %#v", fetches)
	}
}
