package hotfix

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"wowdata/internal/dbd"
)

func TestLatestReturnsWholePushBatch(t *testing.T) {
	s := MemorySource{Records: []Record{{Product: "wow", Build: "69137", Region: 196, Locale: "zhCN", TableName: "SpellPowerDifficulty", PushID: 10, RecordID: 1}, {Product: "wow", Build: "69137", Region: 196, Locale: "zhCN", TableName: "SpellPowerDifficulty", PushID: 11, RecordID: 2}, {Product: "wow", Build: "69137", Region: 196, Locale: "zhCN", TableName: "SpellPowerDifficulty", PushID: 11, RecordID: 3}}}
	r, err := s.Query(context.Background(), Query{Product: "wow", Build: "69137", Region: 196, Locale: "zhCN", Table: "SpellPowerDifficulty", Latest: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Records) != 2 || r.Records[0].RecordID != 2 || r.Records[1].RecordID != 3 {
		t.Fatalf("latest=%#v", r.Records)
	}
}

func makeCache(t *testing.T, magic uint32, version uint32, payload []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "DBCache.bin")
	b := make([]byte, 44+32+len(payload))
	binary.LittleEndian.PutUint32(b[0:4], magic)
	binary.LittleEndian.PutUint32(b[4:8], version)
	binary.LittleEndian.PutUint32(b[8:12], 69137)
	binary.LittleEndian.PutUint32(b[44:48], magic)
	binary.LittleEndian.PutUint32(b[48:52], 196)
	binary.LittleEndian.PutUint32(b[52:56], 11)
	binary.LittleEndian.PutUint32(b[56:60], 7)
	binary.LittleEndian.PutUint32(b[60:64], 99)
	binary.LittleEndian.PutUint32(b[64:68], 1234)
	binary.LittleEndian.PutUint32(b[68:72], uint32(len(payload)))
	b[72] = 1
	copy(b[76:], payload)
	if err := os.WriteFile(p, b, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDBCacheReaderAndSidecar(t *testing.T) {
	p := makeCache(t, FileMagic, 9, []byte{1, 2, 3})
	r, err := OpenDBCache(p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.Header.Build != 69137 || r.Count != 1 {
		t.Fatalf("header/count=%+v %d", r.Header, r.Count)
	}
	rows, err := r.QueryEntries(Query{Product: "wow", Build: "69137", Region: 196, Locale: "zhCN", TableHash: u32(99), RecordID: u32(1234)})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Records) != 1 || string(rows.Records[0].RawData) != string([]byte{1, 2, 3}) {
		t.Fatalf("rows=%#v", rows)
	}
	sp := filepath.Join(t.TempDir(), "DBCache.sidecar")
	if err := WriteSidecar(sp, r); err != nil {
		t.Fatal(err)
	}
	s, err := OpenSidecar(sp)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Find(99, 1234)
	if err != nil || len(got) != 1 || got[0].PayloadLength != 3 {
		t.Fatalf("sidecar=%#v err=%v", got, err)
	}
}
func TestDBCacheRejectsMagicVersionAndTruncation(t *testing.T) {
	for name, p := range map[string]string{"magic": makeCache(t, 0, 9, nil), "version": makeCache(t, FileMagic, 10, nil)} {
		if _, err := OpenDBCache(p); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	p := makeCache(t, FileMagic, 9, []byte{1, 2, 3})
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, b[:len(b)-1], 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDBCache(p); err == nil {
		t.Fatal("truncated payload accepted")
	}
}

func makeVersionedCache(t *testing.T, declared, layout uint32) string {
	t.Helper()
	headerSize := 12
	if declared >= 5 {
		headerSize = 44
	}
	recordSize := recordHeaderSize(layout)
	payload := []byte{1, 2, 3}
	b := make([]byte, headerSize+2*(recordSize+len(payload)))
	binary.LittleEndian.PutUint32(b[0:4], FileMagic)
	binary.LittleEndian.PutUint32(b[4:8], declared)
	binary.LittleEndian.PutUint32(b[8:12], 69137)
	off := headerSize
	for i := 0; i < 2; i++ {
		h := b[off : off+recordSize]
		binary.LittleEndian.PutUint32(h[0:4], FileMagic)
		switch layout {
		case 1:
			binary.LittleEndian.PutUint32(h[4:8], uint32(10+i))
			binary.LittleEndian.PutUint32(h[8:12], uint32(len(payload)))
			binary.LittleEndian.PutUint32(h[12:16], 99)
			binary.LittleEndian.PutUint32(h[16:20], uint32(1234+i))
			h[20] = 1
		case 2, 3, 4, 5, 6:
			binary.LittleEndian.PutUint32(h[4:8], layout)
			binary.LittleEndian.PutUint32(h[8:12], uint32(10+i))
			binary.LittleEndian.PutUint32(h[12:16], uint32(len(payload)))
			binary.LittleEndian.PutUint32(h[16:20], 99)
			binary.LittleEndian.PutUint32(h[20:24], uint32(1234+i))
			h[24] = 1
		case 7:
			binary.LittleEndian.PutUint32(h[4:8], uint32(10+i))
			binary.LittleEndian.PutUint32(h[8:12], 99)
			binary.LittleEndian.PutUint32(h[12:16], uint32(1234+i))
			binary.LittleEndian.PutUint32(h[16:20], uint32(len(payload)))
			h[20] = 3
		case 8:
			binary.LittleEndian.PutUint32(h[4:8], uint32(10+i))
			binary.LittleEndian.PutUint32(h[8:12], uint32(70+i))
			binary.LittleEndian.PutUint32(h[12:16], 99)
			binary.LittleEndian.PutUint32(h[16:20], uint32(1234+i))
			binary.LittleEndian.PutUint32(h[20:24], uint32(len(payload)))
			h[24] = 3
		case 9:
			binary.LittleEndian.PutUint32(h[4:8], 196)
			binary.LittleEndian.PutUint32(h[8:12], uint32(10+i))
			binary.LittleEndian.PutUint32(h[12:16], uint32(70+i))
			binary.LittleEndian.PutUint32(h[16:20], 99)
			binary.LittleEndian.PutUint32(h[20:24], uint32(1234+i))
			binary.LittleEndian.PutUint32(h[24:28], uint32(len(payload)))
			h[28] = 3
		}
		copy(b[off+recordSize:], payload)
		off += recordSize + len(payload)
	}
	p := filepath.Join(t.TempDir(), "DBCache.bin")
	if err := os.WriteFile(p, b, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDBCacheVersionsOneThroughNine(t *testing.T) {
	for version := uint32(1); version <= 9; version++ {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			p := makeVersionedCache(t, version, version)
			r, err := OpenDBCache(p)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if r.Count != 2 || r.Header.Version != version {
				t.Fatalf("header=%+v count=%d", r.Header, r.Count)
			}
			var first Entry
			if err := r.ForEachEntry(func(e Entry) error { first = e; return io.EOF }); err != io.EOF {
				t.Fatalf("iteration error=%v", err)
			}
			if first.PushID != 10 || first.TableHash != 99 || first.RecordID != 1234 || first.PayloadLength != 3 {
				t.Fatalf("entry=%+v", first)
			}
			if version >= 7 && first.Status != 3 || version < 7 && first.Status != 1 {
				t.Fatalf("status=%d", first.Status)
			}
		})
	}
	t.Run("declared-v8-layout-v7", func(t *testing.T) {
		r, err := OpenDBCache(makeVersionedCache(t, 8, 7))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		if r.recordVersion != 7 || r.Count != 2 {
			t.Fatalf("recordVersion=%d count=%d", r.recordVersion, r.Count)
		}
	})
}
func u32(v uint32) *uint32 { return &v }

func TestDecodePositionalPayload(t *testing.T) {
	entry := &dbd.DBDEntry{Fields: []dbd.DBDField{{Name: "ID", Type: "int", IsSigned: false, Size: 4}, {Name: "Value", Type: "float", Size: 4}, {Name: "Name", Type: "string", ArrayLen: -1}}}
	payload := make([]byte, 15)
	binary.LittleEndian.PutUint32(payload[0:4], 42)
	binary.LittleEndian.PutUint32(payload[4:8], uint32(math.Float32bits(1.5)))
	binary.LittleEndian.PutUint32(payload[8:12], 3)
	copy(payload[12:], []byte("abc"))
	got, err := DecodePayload(payload, entry)
	if err != nil {
		t.Fatal(err)
	}
	if got["ID"] != uint64(42) || got["Name"] != "abc" {
		t.Fatalf("decoded=%#v", got)
	}
}

func TestDecodePositionalData(t *testing.T) {
	entry := &dbd.DBDEntry{Fields: []dbd.DBDField{{Name: "ID", Type: "int", IsSigned: false}, {Name: "Power", Type: "int", IsSigned: true}, {Name: "Name", Type: "string"}, {Name: "Values", Type: "float", ArrayLen: 2}}}
	got, err := DecodePositionalData([]any{"42", "-7", "Fire", []any{"1.5", 2}}, entry)
	if err != nil {
		t.Fatal(err)
	}
	if got["ID"] != uint64(42) || got["Power"] != int64(-7) || got["Name"] != "Fire" {
		t.Fatalf("decoded=%#v", got)
	}
	values := got["Values"].([]any)
	if values[0] != float64(1.5) || values[1] != float64(2) {
		t.Fatalf("values=%#v", values)
	}
}

func TestFallbackOnlyForLatest(t *testing.T) {
	primary := failingSource{}
	recent := MemorySource{Records: []Record{{Product: "wow", Build: "69137", Region: 1, Locale: "enUS", PushID: 3, RecordID: 9}}, Coverage: Coverage{Source: "raidbots"}}
	s := FallbackSource{Primary: primary, Recent: recent}
	r, err := s.Query(context.Background(), Query{Product: "wow", Build: "69137", Region: 1, Locale: "enUS", Latest: true, RecordID: u32(9)})
	if err != nil || r.Source != "raidbots" || len(r.Warnings) != 1 {
		t.Fatalf("fallback=%#v err=%v", r, err)
	}
	if _, err = s.Query(context.Background(), Query{Product: "wow", Build: "69137", Region: 1, Locale: "enUS", Page: 2, RecordID: u32(9)}); err == nil {
		t.Fatal("history fallback accepted")
	}
}

type failingSource struct{}

func (failingSource) Query(context.Context, Query) (Result, error) {
	return Result{}, errf("hotfix_unavailable", "wago", "offline")
}
