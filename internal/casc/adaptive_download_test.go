package casc

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestAdaptiveResumeChunkSize(t *testing.T) {
	tests := []struct {
		size int64
		want int64
	}{
		{1 << 20, 1 << 20},
		{16 << 20, 4 << 20},
		{64 << 20, 4 << 20},
		{128 << 20, 8 << 20},
	}
	for _, tt := range tests {
		if got := adaptiveResumeChunkSize(tt.size); got != tt.want {
			t.Fatalf("size=%d chunk=%d, want %d", tt.size, got, tt.want)
		}
	}
}

func TestBuildResumeJobsMergesOnlyAdjacentMissingChunks(t *testing.T) {
	state := resumeState{Complete: []bool{true, false, false, false, true, false}}
	jobs := buildResumeJobs(state, 6<<20, 1<<20, 4<<20)
	if len(jobs) != 2 {
		t.Fatalf("jobs=%d, want 2: %#v", len(jobs), jobs)
	}
	if jobs[0].start != 1<<20 || jobs[0].end != 4<<20-1 || len(jobs[0].indexes) != 3 {
		t.Fatalf("merged job=%#v", jobs[0])
	}
	if jobs[1].start != 5<<20 || jobs[1].end != 6<<20-1 || len(jobs[1].indexes) != 1 {
		t.Fatalf("separate job=%#v", jobs[1])
	}
}

func TestBuildResumeJobsKeepsSegmentsWhenMergeDisabled(t *testing.T) {
	state := resumeState{Complete: []bool{false, false, false}}
	jobs := buildResumeJobs(state, 3<<20, 1<<20, 0)
	if len(jobs) != 3 {
		t.Fatalf("jobs=%d, want 3: %#v", len(jobs), jobs)
	}
}

func TestAdaptiveSmallObjectUsesFullGet(t *testing.T) {
	payload := bytes.Repeat([]byte("s"), 32<<10)
	var fullGets, rangedGets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
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
	if fullGets.Load() != 1 || rangedGets.Load() != 0 {
		t.Fatalf("full=%d ranged=%d, want 1/0", fullGets.Load(), rangedGets.Load())
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
