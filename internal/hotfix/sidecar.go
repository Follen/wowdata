package hotfix

import (
	"bufio"
	"container/heap"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const (
	SidecarMagic              uint32 = 0x31534648 // "HFS1"
	SidecarVersion            uint32 = 2
	SidecarHeaderSize         int64  = 32
	SidecarEntrySize          int64  = 40
	maxInMemorySidecarEntries int64  = 262144
	maxInMemorySidecarBytes   int64  = 16 << 20
)

type Sidecar struct {
	f             *os.File
	count         uint64
	sourceSize    uint64
	sourceBuild   int32
	sourceVersion uint32
	data          []byte
}

func WriteSidecar(path string, r *Reader) error {
	if r == nil {
		return errf("hotfix_invalid_query", "sidecar", "reader is nil")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	var entries []SidecarEntry
	var chunks []string
	var count uint64
	work := ""
	var err error
	if sidecarFitsMemory(r) {
		entries, err = collectSidecarEntries(r)
		count = uint64(len(entries))
	} else {
		work, err = os.MkdirTemp(dir, ".hotfix-sidecar-sort-")
		if err == nil {
			chunks, count, err = writeSortedChunks(work, r, 32768)
		}
	}
	if work != "" {
		defer os.RemoveAll(work)
	}
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	good := false
	defer func() {
		f.Close()
		if !good {
			_ = os.Remove(tmp)
		}
	}()
	h := make([]byte, SidecarHeaderSize)
	binary.LittleEndian.PutUint32(h[0:4], SidecarMagic)
	binary.LittleEndian.PutUint32(h[4:8], SidecarVersion)
	binary.LittleEndian.PutUint64(h[8:16], count)
	binary.LittleEndian.PutUint64(h[16:24], uint64(r.size))
	binary.LittleEndian.PutUint32(h[24:28], uint32(r.Header.Build))
	binary.LittleEndian.PutUint32(h[28:32], r.Header.Version)
	if _, err = f.Write(h); err != nil {
		return err
	}
	if entries != nil {
		body := make([]byte, len(entries)*int(SidecarEntrySize))
		for i, entry := range entries {
			start := i * int(SidecarEntrySize)
			encodeSidecarEntry(body[start:start+int(SidecarEntrySize)], entry)
		}
		if _, err = f.Write(body); err != nil {
			return err
		}
	} else {
		bw := bufio.NewWriterSize(f, 256<<10)
		if err = mergeSortedChunks(bw, chunks); err == nil {
			err = bw.Flush()
		}
		if err != nil {
			return err
		}
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	backup := path + ".old"
	_ = os.Remove(backup)
	if _, statErr := os.Stat(path); statErr == nil {
		if err = os.Rename(path, backup); err != nil {
			return err
		}
	}
	if err = os.Rename(tmp, path); err != nil {
		_ = os.Rename(backup, path)
		return err
	}
	_ = os.Remove(backup)
	good = true
	return nil
}

func sidecarFitsMemory(r *Reader) bool {
	if r.size <= r.headerSize {
		return true
	}
	return (r.size-r.headerSize)/24 <= maxInMemorySidecarEntries
}

func collectSidecarEntries(r *Reader) ([]SidecarEntry, error) {
	capacity := 0
	if r.size > r.headerSize {
		max := (r.size - r.headerSize) / 24
		if max > maxInMemorySidecarEntries {
			max = maxInMemorySidecarEntries
		}
		capacity = int(max)
	}
	out := make([]SidecarEntry, 0, capacity)
	err := r.ForEachEntry(func(e Entry) error {
		out = append(out, SidecarEntry{e.TableHash, e.RecordID, e.PushID, e.Region, e.Status, uint64(e.PayloadOffset), uint32(e.PayloadLength)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortSidecar(out)
	return out, nil
}

func encodeSidecarEntry(b []byte, e SidecarEntry) {
	clear(b)
	binary.LittleEndian.PutUint32(b[0:4], e.TableHash)
	binary.LittleEndian.PutUint32(b[4:8], e.RecordID)
	binary.LittleEndian.PutUint32(b[8:12], uint32(e.PushID))
	binary.LittleEndian.PutUint32(b[12:16], e.Region)
	b[16] = e.Status
	binary.LittleEndian.PutUint64(b[20:28], e.PayloadOffset)
	binary.LittleEndian.PutUint32(b[28:32], e.PayloadLength)
}

func decodeSidecarEntry(b []byte) SidecarEntry {
	return SidecarEntry{binary.LittleEndian.Uint32(b[0:4]), binary.LittleEndian.Uint32(b[4:8]), int32(binary.LittleEndian.Uint32(b[8:12])), binary.LittleEndian.Uint32(b[12:16]), b[16], binary.LittleEndian.Uint64(b[20:28]), binary.LittleEndian.Uint32(b[28:32])}
}

func writeSortedChunks(dir string, r *Reader, chunkSize int) ([]string, uint64, error) {
	if chunkSize < 1 {
		chunkSize = 1
	}
	entries := make([]SidecarEntry, 0, chunkSize)
	paths := make([]string, 0)
	var count uint64
	flush := func() error {
		if len(entries) == 0 {
			return nil
		}
		sortSidecar(entries)
		path := filepath.Join(dir, fmt.Sprintf("chunk-%06d.bin", len(paths)))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		bw := bufio.NewWriterSize(f, 256<<10)
		buf := make([]byte, SidecarEntrySize)
		for _, entry := range entries {
			encodeSidecarEntry(buf, entry)
			if _, err = bw.Write(buf); err != nil {
				_ = f.Close()
				return err
			}
		}
		if err = bw.Flush(); err == nil {
			err = f.Sync()
		}
		if err == nil {
			err = f.Close()
		} else {
			_ = f.Close()
		}
		if err != nil {
			return err
		}
		paths = append(paths, path)
		entries = entries[:0]
		return nil
	}
	err := r.ForEachEntry(func(e Entry) error {
		entries = append(entries, SidecarEntry{e.TableHash, e.RecordID, e.PushID, e.Region, e.Status, uint64(e.PayloadOffset), uint32(e.PayloadLength)})
		count++
		if len(entries) == cap(entries) {
			return flush()
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if err = flush(); err != nil {
		return nil, 0, err
	}
	return paths, count, nil
}

type chunkCursor struct {
	f     *os.File
	entry SidecarEntry
	index int
}
type chunkHeap []*chunkCursor

func (h chunkHeap) Len() int      { return len(h) }
func (h chunkHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h chunkHeap) Less(i, j int) bool {
	a, b := h[i].entry, h[j].entry
	if a.TableHash != b.TableHash {
		return a.TableHash < b.TableHash
	}
	if a.RecordID != b.RecordID {
		return a.RecordID < b.RecordID
	}
	if a.PushID != b.PushID {
		return a.PushID < b.PushID
	}
	return h[i].index < h[j].index
}
func (h *chunkHeap) Push(x any) { *h = append(*h, x.(*chunkCursor)) }
func (h *chunkHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func readChunkEntry(f *os.File) (SidecarEntry, error) {
	var b [SidecarEntrySize]byte
	_, err := io.ReadFull(f, b[:])
	if err != nil {
		return SidecarEntry{}, err
	}
	return decodeSidecarEntry(b[:]), nil
}

func mergeSortedChunks(dst io.Writer, paths []string) error {
	h := &chunkHeap{}
	defer func() {
		for _, c := range *h {
			_ = c.f.Close()
		}
	}()
	for i, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		entry, err := readChunkEntry(f)
		if err != nil {
			_ = f.Close()
			if errors.Is(err, io.EOF) {
				continue
			}
			return err
		}
		heap.Push(h, &chunkCursor{f: f, entry: entry, index: i})
	}
	buf := make([]byte, SidecarEntrySize)
	for h.Len() > 0 {
		cursor := heap.Pop(h).(*chunkCursor)
		encodeSidecarEntry(buf, cursor.entry)
		if _, err := dst.Write(buf); err != nil {
			return err
		}
		next, err := readChunkEntry(cursor.f)
		if err == nil {
			cursor.entry = next
			heap.Push(h, cursor)
			continue
		}
		_ = cursor.f.Close()
		if !errors.Is(err, io.EOF) {
			return err
		}
	}
	return nil
}

func OpenSidecar(path string) (*Sidecar, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if st.Size() < SidecarHeaderSize {
		f.Close()
		return nil, errf("hotfix_corrupt_cache", "sidecar_header", "truncated sidecar")
	}
	var h, body []byte
	if st.Size() <= maxInMemorySidecarBytes {
		raw := make([]byte, int(st.Size()))
		if _, err = io.ReadFull(f, raw); err != nil {
			f.Close()
			return nil, errf("hotfix_corrupt_cache", "sidecar", "%v", err)
		}
		_ = f.Close()
		f = nil
		h, body = raw[:SidecarHeaderSize], raw[SidecarHeaderSize:]
	} else {
		h = make([]byte, SidecarHeaderSize)
		if _, err = f.ReadAt(h, 0); err != nil {
			f.Close()
			return nil, errf("hotfix_corrupt_cache", "sidecar_header", "%v", err)
		}
	}
	if binary.LittleEndian.Uint32(h[0:4]) != SidecarMagic {
		return nilClose(f, errf("hotfix_corrupt_cache", "sidecar_magic", "unexpected sidecar signature"))
	}
	if binary.LittleEndian.Uint32(h[4:8]) != SidecarVersion {
		return nilClose(f, errf("hotfix_corrupt_cache", "sidecar_version", "unsupported sidecar version"))
	}
	count := binary.LittleEndian.Uint64(h[8:16])
	if int64(count) > (st.Size()-SidecarHeaderSize)/SidecarEntrySize {
		return nilClose(f, errf("hotfix_corrupt_cache", "sidecar_entries", "truncated sidecar entries"))
	}
	return &Sidecar{f: f, count: count, sourceSize: binary.LittleEndian.Uint64(h[16:24]), sourceBuild: int32(binary.LittleEndian.Uint32(h[24:28])), sourceVersion: binary.LittleEndian.Uint32(h[28:32]), data: body}, nil
}

func OpenOrBuildSidecar(path string, r *Reader) (*Sidecar, error) {
	if r == nil {
		return nil, errf("hotfix_invalid_query", "sidecar", "reader is nil")
	}
	if s, err := OpenSidecar(path); err == nil {
		if s.matches(r) {
			return s, nil
		}
		_ = s.Close()
	}
	if err := WriteSidecar(path, r); err != nil {
		// A concurrent builder may have published a valid sidecar first.
		if s, openErr := OpenSidecar(path); openErr == nil {
			if s.matches(r) {
				return s, nil
			}
			_ = s.Close()
		}
		return nil, err
	}
	s, err := OpenSidecar(path)
	if err != nil {
		return nil, err
	}
	if !s.matches(r) {
		_ = s.Close()
		return nil, errf("hotfix_corrupt_cache", "sidecar_identity", "sidecar does not match DBCache source")
	}
	return s, nil
}

func (s *Sidecar) matches(r *Reader) bool {
	return s != nil && r != nil && s.sourceSize == uint64(r.size) && s.sourceBuild == r.Header.Build && s.sourceVersion == r.Header.Version
}
func nilClose(f *os.File, err error) (*Sidecar, error) {
	if f != nil {
		_ = f.Close()
	}
	return nil, err
}
func (s *Sidecar) Close() error {
	if s == nil || s.f == nil {
		return nil
	}
	return s.f.Close()
}
func (s *Sidecar) entry(i uint64) (SidecarEntry, error) {
	if i >= s.count {
		return SidecarEntry{}, io.EOF
	}
	if s.data != nil {
		off := int64(i) * SidecarEntrySize
		return decodeSidecarEntry(s.data[off : off+SidecarEntrySize]), nil
	}
	var b [SidecarEntrySize]byte
	if _, err := s.f.ReadAt(b[:], SidecarHeaderSize+int64(i)*SidecarEntrySize); err != nil {
		return SidecarEntry{}, err
	}
	return SidecarEntry{binary.LittleEndian.Uint32(b[0:4]), binary.LittleEndian.Uint32(b[4:8]), int32(binary.LittleEndian.Uint32(b[8:12])), binary.LittleEndian.Uint32(b[12:16]), b[16], binary.LittleEndian.Uint64(b[20:28]), binary.LittleEndian.Uint32(b[28:32])}, nil
}
func (s *Sidecar) Find(tableHash, recordID uint32) ([]SidecarEntry, error) {
	lo, hi := uint64(0), s.count
	for lo < hi {
		m := (lo + hi) / 2
		e, err := s.entry(m)
		if err != nil {
			return nil, err
		}
		if e.TableHash < tableHash || (e.TableHash == tableHash && e.RecordID < recordID) {
			lo = m + 1
		} else {
			hi = m
		}
	}
	out := make([]SidecarEntry, 0)
	for i := lo; i < s.count; i++ {
		e, err := s.entry(i)
		if err != nil {
			return nil, err
		}
		if e.TableHash != tableHash || e.RecordID != recordID {
			break
		}
		out = append(out, e)
	}
	return out, nil
}
func sortSidecar(e []SidecarEntry) {
	sort.Slice(e, func(i, j int) bool {
		if e[i].TableHash != e[j].TableHash {
			return e[i].TableHash < e[j].TableHash
		}
		if e[i].RecordID != e[j].RecordID {
			return e[i].RecordID < e[j].RecordID
		}
		return e[i].PushID < e[j].PushID
	})
}
