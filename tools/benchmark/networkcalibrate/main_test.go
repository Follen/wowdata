package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProbeMetadataAndDisjointMultiRanges(t *testing.T) {
	payload := []byte(strings.Repeat("0123456789abcdef", 1024))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value := r.Header.Get("Range"); value != "" {
			var start, end int
			fmt.Sscanf(value, "bytes=%d-%d", &start, &end)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
			w.WriteHeader(206)
			w.Write(payload[start : end+1])
			return
		}
		w.Write(payload)
	}))
	defer server.Close()
	metadata := probe(context.Background(), time.Second, 1, 1, probeSpec{"metadata", server.URL, "metadata", 0, 1, false})
	if metadata.Status != "pass" || metadata.ResponseBytes != int64(len(payload)) {
		t.Fatalf("metadata=%+v", metadata)
	}
	multi := probe(context.Background(), time.Second, 1, 2, probeSpec{"multi", server.URL, "range-multi", int64(len(payload)), 4, false})
	if multi.Status != "pass" || multi.ResponseBytes != int64(len(payload)) || len(multi.Ranges) != 4 || multi.Requests != 4 || multi.Connections != 4 || multi.ReusedConnections != 0 {
		t.Fatalf("multi=%+v", multi)
	}
}

func TestRunInterleavesAndBuildsSelection(t *testing.T) {
	payload := []byte(strings.Repeat("x", 4<<20))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value := r.Header.Get("Range"); value != "" {
			var start, end int
			fmt.Sscanf(value, "bytes=%d-%d", &start, &end)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
			w.WriteHeader(206)
			w.Write(payload[start : end+1])
			return
		}
		w.Write(payload)
	}))
	defer server.Close()
	dir := t.TempDir()
	metadata := tournamentReport{Summary: []tournamentSummary{{Candidate: 24, SuccessfulRuns: 10, P50WallMs: 5, P95WallMs: 9}, {Candidate: 36, SuccessfulRuns: 10, P50WallMs: 5.2, P95WallMs: 6}}}
	largeRange := tournamentReport{Summary: []tournamentSummary{{Candidate: 2, SuccessfulRuns: 10, P50WallMs: 5.4, P95WallMs: 6}, {Candidate: 4, SuccessfulRuns: 10, P50WallMs: 5, P95WallMs: 10}}}
	chunk := tournamentReport{Summary: []tournamentSummary{{Candidate: 1, SuccessfulRuns: 10, P50WallMs: 8, P95WallMs: 12}, {Candidate: 4, SuccessfulRuns: 10, P50WallMs: 6.7, P95WallMs: 12}, {Candidate: 8, SuccessfulRuns: 10, P50WallMs: 6.6, P95WallMs: 11}}}
	write := func(name string, v any) string {
		path := filepath.Join(dir, name)
		data, _ := json.Marshal(v)
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	r, err := run(config{CDNMetadataURL: server.URL, CDNRangeURL: server.URL, GitHubURL: server.URL, DBDURL: server.URL, MetadataTournament: write("m.json", metadata), LargeRangeTournament: write("l.json", largeRange), ChunkTournament: write("c.json", chunk), Rounds: 2, RangeMiB: 1, MultiConnections: 1, Timeout: time.Second, TotalTimeout: time.Minute, MaxRequests: 100, MaxResponseBytes: 128 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Samples) != 14 || len(r.Sources) != 7 || r.Selection.Metadata.Initial != 36 || r.Selection.LargeRange.Initial != 2 || r.Selection.Chunk.Initial != 8 {
		t.Fatalf("report=%+v", r)
	}
	wantOrder := []string{
		"cdn-metadata", "github-revision", "dbd-raw", "cdn-single", "cdn-multi", "cdn-small-4", "cdn-small-24",
		"github-revision", "dbd-raw", "cdn-single", "cdn-multi", "cdn-small-4", "cdn-small-24", "cdn-metadata",
	}
	for i, sample := range r.Samples {
		if sample.Name != wantOrder[i] {
			t.Fatalf("sample %d name=%q, want %q", i, sample.Name, wantOrder[i])
		}
	}
	for _, sample := range r.Samples {
		switch sample.Name {
		case "cdn-small-4":
			if sample.Status != "pass" || sample.ResponseBytes != 512<<10 || sample.Requests != 4 || sample.Connections != 0 || sample.ReusedConnections != 4 {
				t.Fatalf("cdn-small-4=%+v", sample)
			}
		case "cdn-small-24":
			if sample.Status != "pass" || sample.ResponseBytes != 1536<<10 || sample.Requests != 24 || sample.Connections != 0 || sample.ReusedConnections != 24 {
				t.Fatalf("cdn-small-24=%+v", sample)
			}
		}
	}
}
func TestConfigBounds(t *testing.T) {
	if _, err := run(config{}); err == nil {
		t.Fatal("expected invalid config error")
	}
}
