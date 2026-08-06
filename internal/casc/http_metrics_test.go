package casc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"wowdata/internal/resource"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestAddHTTPIntervalCountsOnlyNewBytes(t *testing.T) {
	intervals := make(map[string][]byteInterval)
	if got := addHTTPInterval(intervals, "url", byteInterval{start: 0, end: 10}); got != 10 {
		t.Fatalf("first added = %d", got)
	}
	if got := addHTTPInterval(intervals, "url", byteInterval{start: 5, end: 15}); got != 5 {
		t.Fatalf("overlap added = %d", got)
	}
	if got := addHTTPInterval(intervals, "url", byteInterval{start: 0, end: 15}); got != 0 {
		t.Fatalf("duplicate added = %d", got)
	}
}

func TestAddHTTPIntervalDoesNotOverwriteExistingBackingArray(t *testing.T) {
	intervals := map[string][]byteInterval{"url": make([]byteInterval, 0, 8)}
	for i := int64(0); i < 4; i++ {
		if got := addHTTPInterval(intervals, "url", byteInterval{start: i * 10, end: i*10 + 10}); got != 10 {
			t.Fatalf("interval %d added = %d", i, got)
		}
	}
	if got := addHTTPInterval(intervals, "url", byteInterval{start: 0, end: 40}); got != 0 {
		t.Fatalf("duplicate range added = %d", got)
	}
}

func TestHTTPMetricsSeparateCancellationAndDuplicateURLs(t *testing.T) {
	ResetHTTPMetrics()
	first := &metricBody{url: "https://cdn.test/object", interval: byteInterval{start: 0, end: 10}, bytes: 10}
	second := &metricBody{url: "https://cdn.test/object", interval: byteInterval{start: 0, end: 10}, bytes: 10}
	first.record()
	second.record()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://cdn.test/canceled", nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := instrumentedTransport{base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})}
	if _, err := transport.RoundTrip(request); err == nil {
		t.Fatal("expected canceled request error")
	}

	metrics := SnapshotHTTPMetrics()
	if metrics.DuplicateBytes != 10 || metrics.DuplicateByURL["https://cdn.test/object"] != 10 {
		t.Fatalf("duplicate metrics = %#v", metrics)
	}
	if metrics.CanceledRequests != 1 || metrics.FailedRequests != 0 {
		t.Fatalf("request metrics = %#v", metrics)
	}
}

func TestParseRangeLength(t *testing.T) {
	for input, want := range map[string]int64{"bytes=0-9": 10, "bytes=-8192": 8192, "bad": 0} {
		if got := ParseRangeLength(input); got != want {
			t.Fatalf("%s = %d, want %d", input, got, want)
		}
	}
}

func TestInstrumentHTTPClientCountsChunkedPayloadAndConnectionReuse(t *testing.T) {
	ResetHTTPMetrics()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/failed" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		if request.URL.Path == "/failed" {
			_, _ = io.WriteString(w, "failure")
			return
		}
		_, _ = io.WriteString(w, "payload")
	}))
	defer server.Close()

	client := InstrumentHTTPClient(server.Client())
	for i := 0; i < 2; i++ {
		response, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, response.Body); err != nil {
			t.Fatal(err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
	}
	response, err := client.Get(server.URL + "/failed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}

	metrics := SnapshotHTTPMetrics()
	if metrics.Requests != 3 || metrics.FailedRequests != 1 || metrics.ResponseBytes != 21 || metrics.UniquePayloadBytes != 14 || metrics.DuplicateBytes != 7 {
		t.Fatalf("HTTP metrics = %#v", metrics)
	}
	if metrics.Connections != 1 || metrics.ReusedConnections != 2 {
		t.Fatalf("connection metrics = %#v", metrics)
	}
	if metrics.Protocols["HTTP/1.1"] != 3 || metrics.RequestTimeNanos <= 0 {
		t.Fatalf("protocol/time metrics = %#v", metrics)
	}
	if got := metrics.DuplicateByURL[server.URL]; got != 7 {
		t.Fatalf("duplicate bytes for URL = %d", got)
	}
}

func TestInstrumentHTTPClientForStageKeepsConcurrentAttributionSeparate(t *testing.T) {
	ResetHTTPMetrics()
	resource.ResetStages()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(w, request.URL.Path[1:])
	}))
	defer server.Close()

	first := resource.StartStage("first", resource.StageOptions{})
	second := resource.StartStage("second", resource.StageOptions{})
	clients := []*http.Client{
		InstrumentHTTPClientForStage(server.Client(), first),
		InstrumentHTTPClientForStage(server.Client(), second),
	}
	paths := []string{"12345", "123456789"}
	var wg sync.WaitGroup
	for i := range clients {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			response, err := clients[i].Get(server.URL + "/" + paths[i])
			if err != nil {
				t.Errorf("get: %v", err)
				return
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}(i)
	}
	wg.Wait()
	resource.FinishStage(first, nil)
	resource.FinishStage(second, nil)

	snapshot := resource.SnapshotStages()
	if len(snapshot.Work) != 2 || snapshot.Work[0].NetworkTransferredBytes != 5 || snapshot.Work[1].NetworkTransferredBytes != 9 {
		t.Fatalf("stage work = %#v", snapshot.Work)
	}
	metrics := SnapshotHTTPMetrics()
	resource.ReconcileStageNetwork(uint64(metrics.UniquePayloadBytes), uint64(metrics.ResponseBytes))
	if snapshot := resource.SnapshotStages(); !snapshot.Complete {
		t.Fatalf("stages = %#v", snapshot)
	}
}
