package hotfix

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)

const (
	FileMagic        uint32 = 0x48544658 // "XFTH" little endian
	RecordHeaderSize        = 32         // V9, retained for compatibility/tests
	FileHeaderSize          = 44         // V5+, retained for compatibility/tests
)

type Header struct {
	Signature uint32
	Version   uint32
	Build     int32
}
type Entry struct {
	Region        uint32
	PushID        int32
	UniqueID      uint32
	TableHash     uint32
	RecordID      uint32
	Status        uint8
	PayloadOffset int64
	PayloadLength int
}

type Reader struct {
	ra            io.ReaderAt
	size          int64
	closeFile     *os.File
	Header        Header
	Count         int64
	headerSize    int64
	recordVersion uint32
}

func OpenDBCache(path string) (*Reader, error) {
	return openDBCache(path, true)
}

// OpenDBCacheLazy validates the file header without scanning every record.
// Query paths backed by a reusable sidecar do not need to pay a full-file scan
// merely to populate Reader.Count.
func OpenDBCacheLazy(path string) (*Reader, error) {
	return openDBCache(path, false)
}

func openDBCache(path string, scan bool) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	r, err := openDBCacheReader(f, st.Size(), scan)
	if err != nil {
		f.Close()
		return nil, err
	}
	r.closeFile = f
	return r, nil
}

func OpenDBCacheReader(ra io.ReaderAt, size int64) (*Reader, error) {
	return openDBCacheReader(ra, size, true)
}

func openDBCacheReader(ra io.ReaderAt, size int64, scan bool) (*Reader, error) {
	if size < 12 {
		return nil, errf("hotfix_corrupt_cache", "header", "file is shorter than 12 bytes")
	}
	b := make([]byte, min64(size, FileHeaderSize))
	if _, err := ra.ReadAt(b, 0); err != nil && err != io.EOF {
		return nil, errf("hotfix_corrupt_cache", "header", "%v", err)
	}
	h := Header{binary.LittleEndian.Uint32(b[0:4]), binary.LittleEndian.Uint32(b[4:8]), int32(binary.LittleEndian.Uint32(b[8:12]))}
	if h.Signature != FileMagic {
		return nil, errf("hotfix_corrupt_cache", "magic", "unexpected signature 0x%08x", h.Signature)
	}
	if h.Version < 1 || h.Version > 9 {
		return nil, errf("hotfix_corrupt_cache", "version", "unsupported DBCache version %d", h.Version)
	}
	headerSize := int64(12)
	if h.Version >= 5 {
		headerSize = FileHeaderSize
		if size < headerSize {
			return nil, errf("hotfix_corrupt_cache", "header", "version %d file is shorter than %d bytes", h.Version, headerSize)
		}
	}
	recordVersion := h.Version
	if h.Version == 8 {
		recordVersion = detectV8RecordVersion(ra, size, headerSize)
	}
	r := &Reader{ra: ra, size: size, Header: h, headerSize: headerSize, recordVersion: recordVersion}
	if scan {
		if err := r.scan(); err != nil {
			return nil, err
		}
	}
	return r, nil
}

var _ io.Closer = (*Reader)(nil)

func (r *Reader) Close() error {
	if r.closeFile != nil {
		return r.closeFile.Close()
	}
	return nil
}

func (r *Reader) scan() error {
	return r.ForEachEntry(func(Entry) error { r.Count++; return nil })
}

func (r *Reader) ForEachEntry(fn func(Entry) error) error {
	off := r.headerSize
	headerSize := recordHeaderSize(r.recordVersion)
	hdr := make([]byte, headerSize)
	for off < r.size {
		if r.size-off < int64(headerSize) {
			return errf("hotfix_corrupt_cache", "record_header", "truncated header at %d", off)
		}
		if _, err := r.ra.ReadAt(hdr, off); err != nil {
			return errf("hotfix_corrupt_cache", "record_header", "read at %d: %v", off, err)
		}
		if sig := binary.LittleEndian.Uint32(hdr[0:4]); sig != FileMagic {
			return errf("hotfix_corrupt_cache", "record_magic", "unexpected signature 0x%08x at %d", sig, off)
		}
		e, n, err := decodeEntryHeader(r.recordVersion, hdr, off)
		if err != nil {
			return err
		}
		if n < 0 || int64(n) > r.size-off-int64(headerSize) {
			return errf("hotfix_corrupt_cache", "payload", "invalid size %d at %d", n, off)
		}
		e.PayloadOffset = off + int64(headerSize)
		e.PayloadLength = int(n)
		if err := fn(e); err != nil {
			return err
		}
		off += int64(headerSize) + int64(n)
	}
	return nil
}

func recordHeaderSize(version uint32) int {
	switch version {
	case 1, 7:
		return 24
	case 2, 3, 4, 5, 6, 8:
		return 28
	case 9:
		return 32
	default:
		return 0
	}
}

func decodeEntryHeader(version uint32, b []byte, off int64) (Entry, int32, error) {
	if len(b) < 4 || binary.LittleEndian.Uint32(b[0:4]) != FileMagic {
		return Entry{}, 0, errf("hotfix_corrupt_cache", "record_magic", "unexpected signature at %d", off)
	}
	var e Entry
	var n int32
	switch version {
	case 1:
		e.PushID = int32(binary.LittleEndian.Uint32(b[4:8]))
		n = int32(binary.LittleEndian.Uint32(b[8:12]))
		e.TableHash = binary.LittleEndian.Uint32(b[12:16])
		e.RecordID = binary.LittleEndian.Uint32(b[16:20])
		if b[20] != 0 {
			e.Status = 1
		}
	case 2, 3, 4, 5, 6:
		e.PushID = int32(binary.LittleEndian.Uint32(b[8:12]))
		n = int32(binary.LittleEndian.Uint32(b[12:16]))
		e.TableHash = binary.LittleEndian.Uint32(b[16:20])
		e.RecordID = binary.LittleEndian.Uint32(b[20:24])
		if b[24] != 0 {
			e.Status = 1
		}
	case 7:
		e.PushID = int32(binary.LittleEndian.Uint32(b[4:8]))
		e.TableHash = binary.LittleEndian.Uint32(b[8:12])
		e.RecordID = binary.LittleEndian.Uint32(b[12:16])
		n = int32(binary.LittleEndian.Uint32(b[16:20]))
		e.Status = b[20]
	case 8:
		e.PushID = int32(binary.LittleEndian.Uint32(b[4:8]))
		e.UniqueID = binary.LittleEndian.Uint32(b[8:12])
		e.TableHash = binary.LittleEndian.Uint32(b[12:16])
		e.RecordID = binary.LittleEndian.Uint32(b[16:20])
		n = int32(binary.LittleEndian.Uint32(b[20:24]))
		e.Status = b[24]
	case 9:
		e.Region = binary.LittleEndian.Uint32(b[4:8])
		e.PushID = int32(binary.LittleEndian.Uint32(b[8:12]))
		e.UniqueID = binary.LittleEndian.Uint32(b[12:16])
		e.TableHash = binary.LittleEndian.Uint32(b[16:20])
		e.RecordID = binary.LittleEndian.Uint32(b[20:24])
		n = int32(binary.LittleEndian.Uint32(b[24:28]))
		e.Status = b[28]
	default:
		return Entry{}, 0, errf("hotfix_corrupt_cache", "version", "unsupported record version %d", version)
	}
	return e, n, nil
}

func detectV8RecordVersion(ra io.ReaderAt, size, off int64) uint32 {
	// Blizzard briefly emitted version=8 files using the V7 entry layout.
	// Prefer V8 only when the first record leads exactly to EOF or to another
	// XFTH signature; otherwise fall back to V7 as DBCD does.
	for _, version := range []uint32{8, 7} {
		hs := recordHeaderSize(version)
		if size-off < int64(hs) {
			continue
		}
		b := make([]byte, hs)
		if _, err := ra.ReadAt(b, off); err != nil {
			continue
		}
		_, n, err := decodeEntryHeader(version, b, off)
		if err != nil || n < 0 {
			continue
		}
		next := off + int64(hs) + int64(n)
		if next == size {
			return version
		}
		if next+4 <= size {
			magic := make([]byte, 4)
			if _, err := ra.ReadAt(magic, next); err == nil && binary.LittleEndian.Uint32(magic) == FileMagic {
				return version
			}
		}
	}
	return 8
}

func min64(a int64, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func (r *Reader) ReadEntry(e Entry) (Record, error) {
	payload := make([]byte, e.PayloadLength)
	if _, err := r.ra.ReadAt(payload, e.PayloadOffset); err != nil && err != io.EOF {
		return Record{}, errf("hotfix_corrupt_cache", "payload", "read at %d: %v", e.PayloadOffset, err)
	}
	return Record{PushID: e.PushID, UniqueID: e.UniqueID, RecordID: e.RecordID, TableHash: e.TableHash, Region: e.Region, Status: e.Status, Build: fmt.Sprint(r.Header.Build), RawData: payload, PayloadOffset: e.PayloadOffset, PayloadLength: e.PayloadLength}, nil
}

func (r *Reader) QueryEntries(q Query) (Result, error) {
	if err := q.Validate(); err != nil {
		return Result{}, err
	}
	if !r.queryCanMatch(q) {
		return r.emptyResult(q), nil
	}
	entries := make([]Entry, 0, 128)
	err := r.ForEachEntry(func(e Entry) error {
		if q.Region != 0 && e.Region != q.Region || q.TableHash != nil && e.TableHash != *q.TableHash || q.RecordID != nil && e.RecordID != *q.RecordID || q.PushID != nil && e.PushID != *q.PushID || q.Status != nil && e.Status != *q.Status {
			return nil
		}
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	entries = windowEntries(entries, q)
	rows := make([]Record, 0, len(entries))
	for _, e := range entries {
		row, readErr := r.ReadEntry(e)
		if readErr != nil {
			return Result{}, readErr
		}
		row.Product, row.Locale = q.Product, q.Locale
		rows = append(rows, row)
	}
	return Result{Query: q, Source: "dbcache", Coverage: Coverage{Source: "dbcache", Build: q.Build, Region: q.Region, Locale: q.Locale, Complete: true}, Records: rows, Total: int64(len(rows))}, nil
}

func (r *Reader) QuerySidecar(s *Sidecar, q Query) (Result, error) {
	if s == nil || q.TableHash == nil || q.RecordID == nil {
		return r.QueryEntries(q)
	}
	if err := q.Validate(); err != nil {
		return Result{}, err
	}
	if !r.queryCanMatch(q) {
		return r.emptyResult(q), nil
	}
	entries, err := s.Find(*q.TableHash, *q.RecordID)
	if err != nil {
		return Result{}, err
	}
	matched := entries[:0]
	for _, item := range entries {
		if q.Region != 0 && item.Region != q.Region || q.PushID != nil && item.PushID != *q.PushID || q.Status != nil && item.Status != *q.Status {
			continue
		}
		matched = append(matched, item)
	}
	matched = windowSidecarEntries(matched, q)
	rows := make([]Record, 0, len(matched))
	for _, item := range matched {
		row, err := r.ReadEntry(Entry{Region: item.Region, PushID: item.PushID, TableHash: item.TableHash, RecordID: item.RecordID, Status: item.Status, PayloadOffset: int64(item.PayloadOffset), PayloadLength: int(item.PayloadLength)})
		if err != nil {
			return Result{}, err
		}
		row.Product, row.Locale = q.Product, q.Locale
		rows = append(rows, row)
	}
	return Result{Query: q, Source: "dbcache", Coverage: Coverage{Source: "dbcache", Build: q.Build, Region: q.Region, Locale: q.Locale, Complete: true}, Records: rows, Total: int64(len(rows))}, nil
}

func (r *Reader) queryCanMatch(q Query) bool {
	if !buildMatches(q.Build, fmt.Sprint(r.Header.Build)) || q.Table != "" {
		return false
	}
	zero := time.Time{}
	return (q.From == nil || !zero.Before(*q.From)) && (q.To == nil || !zero.After(*q.To))
}

func (r *Reader) emptyResult(q Query) Result {
	return Result{Query: q, Source: "dbcache", Coverage: Coverage{Source: "dbcache", Build: q.Build, Region: q.Region, Locale: q.Locale, Complete: true}, Records: []Record{}}
}

func windowEntries(entries []Entry, q Query) []Entry {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].PushID != entries[j].PushID {
			return entries[i].PushID < entries[j].PushID
		}
		return entries[i].RecordID < entries[j].RecordID
	})
	if q.Latest && len(entries) > 0 {
		max := entries[len(entries)-1].PushID
		start := len(entries) - 1
		for start > 0 && entries[start-1].PushID == max {
			start--
		}
		entries = entries[start:]
	}
	return pageEntries(entries, q.Page, q.Limit)
}

func windowSidecarEntries(entries []SidecarEntry, q Query) []SidecarEntry {
	// Find returns a single table/record range ordered by PushID.
	if q.Latest && len(entries) > 0 {
		max := entries[len(entries)-1].PushID
		start := len(entries) - 1
		for start > 0 && entries[start-1].PushID == max {
			start--
		}
		entries = entries[start:]
	}
	return pageEntries(entries, q.Page, q.Limit)
}

func pageEntries[T any](entries []T, page, limit int) []T {
	if limit <= 0 {
		return entries
	}
	from := 0
	if page > 0 {
		from = page * limit
	}
	if from >= len(entries) {
		return entries[:0]
	}
	to := from + limit
	if to > len(entries) {
		to = len(entries)
	}
	return entries[from:to]
}

type SidecarEntry struct {
	TableHash     uint32
	RecordID      uint32
	PushID        int32
	Region        uint32
	Status        uint8
	PayloadOffset uint64
	PayloadLength uint32
}

func BuildSidecar(r *Reader) []SidecarEntry {
	out := make([]SidecarEntry, 0, r.Count)
	_ = r.ForEachEntry(func(e Entry) error {
		out = append(out, SidecarEntry{e.TableHash, e.RecordID, e.PushID, e.Region, e.Status, uint64(e.PayloadOffset), uint32(e.PayloadLength)})
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].TableHash != out[j].TableHash {
			return out[i].TableHash < out[j].TableHash
		}
		if out[i].RecordID != out[j].RecordID {
			return out[i].RecordID < out[j].RecordID
		}
		return out[i].PushID < out[j].PushID
	})
	return out
}
