package casc

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"

	"wowdata/internal/blte"
)

type recordingRootReader struct {
	data  []byte
	reads [][2]int
}

func (r *recordingRootReader) Size() int { return len(r.data) }

func (r *recordingRootReader) ReadRange(offset, length, _ int) ([]byte, error) {
	if offset < 0 || length < 0 || offset > len(r.data) || length > len(r.data)-offset {
		return nil, fmt.Errorf("invalid range %d+%d", offset, length)
	}
	r.reads = append(r.reads, [2]int{offset, offset + length})
	return r.data[offset : offset+length], nil
}

func TestRootDeltaBatchBytes(t *testing.T) {
	t.Setenv("WOWDATA_ROOT_DELTA_BATCH_KIB", "")
	if got := rootDeltaBatchBytes(); got != 4*1024*1024 {
		t.Fatalf("default batch=%d", got)
	}
	t.Setenv("WOWDATA_ROOT_DELTA_BATCH_KIB", "8192")
	if got := rootDeltaBatchBytes(); got != 8*1024*1024 {
		t.Fatalf("configured batch=%d", got)
	}
	t.Setenv("WOWDATA_ROOT_DELTA_BATCH_KIB", "999999")
	if got := rootDeltaBatchBytes(); got != 4*1024*1024 {
		t.Fatalf("invalid batch=%d", got)
	}
}

func TestParseRootReaderSelectedMatchesMaterializedParser(t *testing.T) {
	raw := make([]byte, 4+8+3*4+3*24)
	pos := 0
	binary.LittleEndian.PutUint32(raw[pos:], 3)
	pos += 4
	binary.LittleEndian.PutUint32(raw[pos:], 0)
	pos += 4
	binary.LittleEndian.PutUint32(raw[pos:], uint32(LocaleEnUS))
	pos += 4
	for _, delta := range []uint32{10, 9, 9} {
		binary.LittleEndian.PutUint32(raw[pos:], delta)
		pos += 4
	}
	for _, value := range []byte{0xaa, 0xbb, 0xcc} {
		for index := 0; index < 16; index++ {
			raw[pos+index] = value
		}
		pos += 24
	}
	want := NewCASCSource()
	if _, err := want.parseRootSelected(raw, map[uint32]struct{}{20: {}}); err != nil {
		t.Fatal(err)
	}
	reader, err := blte.NewReader(buildCascTestBLTE(raw))
	if err != nil {
		t.Fatal(err)
	}
	got := NewCASCSource()
	if _, err := got.ParseRootReaderSelected(reader, []uint32{20}, 2); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.RootEntries, want.RootEntries) || !reflect.DeepEqual(got.RootTypes, want.RootTypes) {
		t.Fatalf("range parser differs: got=%#v/%#v want=%#v/%#v", got.RootEntries, got.RootTypes, want.RootEntries, want.RootTypes)
	}
}

func TestParseRootReaderSelectedLocaleSkipsRejectedBlockPayloads(t *testing.T) {
	const fdid = uint32(42)
	type block struct {
		locale  LocaleFlag
		content ContentFlag
		keyByte byte
	}
	blocks := []block{
		{locale: LocaleEnGB, keyByte: 0xaa},
		{locale: LocaleEnUS, content: ContentLowViolence, keyByte: 0xbb},
		{locale: LocaleEnUS, keyByte: 0xcc},
	}
	raw := make([]byte, len(blocks)*(4+8+4+24))
	pos := 0
	rejectedPayloads := make([][2]int, 0, 2)
	for index, item := range blocks {
		binary.LittleEndian.PutUint32(raw[pos:], 1)
		pos += 4
		binary.LittleEndian.PutUint32(raw[pos:], uint32(item.content))
		binary.LittleEndian.PutUint32(raw[pos+4:], uint32(item.locale))
		pos += 8
		payloadStart := pos
		binary.LittleEndian.PutUint32(raw[pos:], fdid)
		pos += 4
		for keyIndex := 0; keyIndex < 16; keyIndex++ {
			raw[pos+keyIndex] = item.keyByte
		}
		pos += 24
		if index < 2 {
			rejectedPayloads = append(rejectedPayloads, [2]int{payloadStart, pos})
		}
	}

	reader := &recordingRootReader{data: raw}
	filtered := NewCASCSource()
	filtered.Locale = LocaleEnUS
	if _, err := filtered.parseRootReaderSelectedForLocale(reader, []uint32{fdid}, 2); err != nil {
		t.Fatal(err)
	}
	entries := filtered.RootEntries[fdid]
	if len(entries) != 1 || entries[0].ContentKey != "cccccccccccccccccccccccccccccccc" {
		t.Fatalf("filtered entries = %#v", entries)
	}
	if len(filtered.RootTypes) != 1 || filtered.RootTypes[0].LocaleFlags != LocaleEnUS || filtered.RootTypes[0].ContentFlags != 0 {
		t.Fatalf("filtered root types = %#v", filtered.RootTypes)
	}
	for name, parse := range map[string]func(*CASCSource) error{
		"buffered": func(source *CASCSource) error {
			_, err := source.parseRootFileSelectedForLocale(buildCascTestBLTE(raw), []uint32{fdid}, 2)
			return err
		},
		"streaming": func(source *CASCSource) error {
			_, err := source.parseRootFileSelectedStreaming(buildCascTestBLTE(raw), []uint32{fdid}, &rootTypeFilter{locale: source.Locale})
			return err
		},
	} {
		source := NewCASCSource()
		source.Locale = LocaleEnUS
		if err := parse(source); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(source.RootEntries, filtered.RootEntries) || !reflect.DeepEqual(source.RootTypes, filtered.RootTypes) {
			t.Fatalf("%s differs: entries=%#v types=%#v", name, source.RootEntries, source.RootTypes)
		}
	}
	for _, read := range reader.reads {
		for _, rejected := range rejectedPayloads {
			if read[0] < rejected[1] && read[1] > rejected[0] {
				t.Fatalf("read %v intersects rejected payload %v; all reads=%v", read, rejected, reader.reads)
			}
		}
	}

	generic := NewCASCSource()
	if _, err := generic.parseRootSelected(raw, map[uint32]struct{}{fdid: {}}); err != nil {
		t.Fatal(err)
	}
	if len(generic.RootTypes) != 3 || len(generic.RootEntries[fdid]) != 3 {
		t.Fatalf("generic parser lost variants: entries=%#v types=%#v", generic.RootEntries[fdid], generic.RootTypes)
	}
}

func TestParseRootReaderSelectedLocalePreservesMissingLocaleError(t *testing.T) {
	raw := make([]byte, 4+8+4+24)
	binary.LittleEndian.PutUint32(raw, 1)
	binary.LittleEndian.PutUint32(raw[4:], 0)
	binary.LittleEndian.PutUint32(raw[8:], uint32(LocaleEnGB))
	binary.LittleEndian.PutUint32(raw[12:], 42)
	reader := &recordingRootReader{data: raw}
	source := NewCASCSource()
	source.Locale = LocaleEnUS
	if _, err := source.parseRootReaderSelectedForLocale(reader, []uint32{42}, 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := source.ResolveFileKeys(42); err == nil || err.Error() != "no root entry found for locale: 2" {
		t.Fatalf("ResolveFileKeys error = %v", err)
	}
}
