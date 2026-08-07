package casc

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdaptiveResumeWorkersScaleByObjectSize(t *testing.T) {
	if got := adaptiveResumeWorkers(4, 4<<20, 1<<20); got != 1 {
		t.Fatalf("small workers=%d, want 1", got)
	}
	if got := adaptiveResumeWorkers(4, 16<<20, 1<<20); got != 2 {
		t.Fatalf("medium workers=%d, want 2", got)
	}
	if got := adaptiveResumeWorkers(4, 64<<20, 1<<20); got != 4 {
		t.Fatalf("large workers=%d, want 4", got)
	}
}

func TestAdaptiveResumeChunkSizeUsesSmallRangesForLargeObjects(t *testing.T) {
	if got := adaptiveResumeChunkSize(4<<20, 184<<20); got != rangeChunkSize {
		t.Fatalf("large adaptive chunk=%d, want %d", got, rangeChunkSize)
	}
	if got := adaptiveResumeChunkSize(4<<20, 16<<20); got != 4<<20 {
		t.Fatalf("medium adaptive chunk=%d, want 4MiB", got)
	}
	if got := adaptiveResumeChunkSize(2<<20, 184<<20); got != rangeChunkSize {
		t.Fatalf("explicit large adaptive chunk=%d, want %d", got, rangeChunkSize)
	}
}

func TestAdaptiveRangeTransportUsesParallelHTTP1Connections(t *testing.T) {
	transport, ok := adaptiveRangeHTTPClient.Transport.(instrumentedTransport)
	if !ok {
		t.Fatalf("adaptive range transport type=%T", adaptiveRangeHTTPClient.Transport)
	}
	base, ok := transport.base.(*http.Transport)
	if !ok {
		t.Fatalf("adaptive range base type=%T", transport.base)
	}
	if base.ForceAttemptHTTP2 || base.TLSNextProto == nil || len(base.TLSNextProto) != 0 {
		t.Fatalf("adaptive range transport still permits HTTP/2: force=%v tlsNextProto=%v", base.ForceAttemptHTTP2, base.TLSNextProto)
	}
}

func TestAdaptiveResumeSkipsHeadForImmutableState(t *testing.T) {
	payload := bytes.Repeat([]byte("r"), rangeChunkSize*2+17)
	var heads, ranges atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			heads.Add(1)
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.Header().Set("ETag", "stable")
			return
		}
		var start, end int
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		ranges.Add(1)
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer server.Close()
	dir := t.TempDir()
	part, statePath := filepath.Join(dir, "object.part"), filepath.Join(dir, "object.state.json")
	if err := os.WriteFile(part, payload, 0644); err != nil {
		t.Fatal(err)
	}
	state := resumeState{URL: server.URL, Size: int64(len(payload)), ChunkSize: rangeChunkSize, ETag: "stable", AcceptRanges: "bytes", Complete: []bool{true, false, false}, Hashes: []string{hashBytes(payload[:rangeChunkSize]), "", ""}}
	if err := saveResumeState(statePath, state); err != nil {
		t.Fatal(err)
	}
	data, err := DownloadHTTPConcurrentResumableWithOptions(server.URL, part, statePath, ResumeOptions{Workers: 2, TTL: time.Hour, MergeRanges: true, AssumeImmutable: true})
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("download err=%v equal=%v", err, bytes.Equal(data, payload))
	}
	if heads.Load() != 0 || ranges.Load() != 1 {
		t.Fatalf("heads=%d ranges=%d, want 0/1", heads.Load(), ranges.Load())
	}
}

func TestAdaptiveSmallObjectUsesFullGet(t *testing.T) {
	payload := bytes.Repeat([]byte("s"), 32<<10)
	var heads, fullGets, rangedGets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			heads.Add(1)
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			return
		}
		if r.Header.Get("Range") == "" {
			fullGets.Add(1)
		} else {
			rangedGets.Add(1)
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	dir := t.TempDir()
	data, err := DownloadHTTPConcurrentResumableWithOptions(server.URL, filepath.Join(dir, "object.part"), filepath.Join(dir, "object.state.json"), ResumeOptions{Adaptive: true})
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("download err=%v equal=%v", err, bytes.Equal(data, payload))
	}
	if heads.Load() != 1 || fullGets.Load() != 1 || rangedGets.Load() != 0 {
		t.Fatalf("heads=%d full=%d ranged=%d, want 1/1/0", heads.Load(), fullGets.Load(), rangedGets.Load())
	}
}

func TestAdaptiveRangeMergeReducesRequests(t *testing.T) {
	payload := bytes.Repeat([]byte("m"), rangeChunkSize*3+17)
	var ranges atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			return
		}
		var start, end int
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		ranges.Add(1)
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer server.Close()
	dir := t.TempDir()
	data, err := DownloadHTTPConcurrentResumableWithOptions(server.URL, filepath.Join(dir, "object.part"), filepath.Join(dir, "object.state.json"), ResumeOptions{ChunkSize: rangeChunkSize, MergeRanges: true})
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("download err=%v equal=%v", err, bytes.Equal(data, payload))
	}
	if ranges.Load() != 1 {
		t.Fatalf("range requests=%d, want 1", ranges.Load())
	}
}

func TestEquivalentRangeURLsShareImmutableObject(t *testing.T) {
	payload := bytes.Repeat([]byte("h"), rangeChunkSize*3+17)
	var primaryRanges, alternateRanges atomic.Int32
	handler := func(counter *atomic.Int32, allowHead bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				if !allowHead {
					t.Fatalf("alternate host received HEAD")
				}
				w.Header().Set("Accept-Ranges", "bytes")
				w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
				return
			}
			var start, end int
			_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
			counter.Add(1)
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start : end+1])
		}
	}
	primary := httptest.NewServer(handler(&primaryRanges, true))
	defer primary.Close()
	alternate := httptest.NewServer(handler(&alternateRanges, false))
	defer alternate.Close()
	dir := t.TempDir()
	data, err := DownloadHTTPConcurrentResumableWithOptions(primary.URL, filepath.Join(dir, "object.part"), filepath.Join(dir, "object.state.json"), ResumeOptions{
		Workers: 2, ChunkSize: rangeChunkSize, RangeURLs: []string{alternate.URL},
	})
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("download err=%v equal=%v", err, bytes.Equal(data, payload))
	}
	if primaryRanges.Load() == 0 || alternateRanges.Load() == 0 {
		t.Fatalf("range distribution primary=%d alternate=%d", primaryRanges.Load(), alternateRanges.Load())
	}
}
